package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgproto3"
)

// SimpleQuerier is the simple-protocol face of a pinned connection
// (Amendment 1, RATIFIED 2026-09-02): ONE Query frame, the response streamed as
// protocol data until the terminal ReadyForQuery.
//
// It exists for a consumer that gates SQL TEXT before dispatch — autodb's
// engine — and it is declared as a SEPARATE interface rather than on PinnedConn
// so that reaching it requires an explicit type assertion, which an acceptance
// grep can see. That is VISIBILITY, not prevention: the dynamic *pinnedConn
// implements both faces, so any holder of the handle can assert for this one.
// golib does not and cannot make the method engine-only; the consumer does
// (A1-C1). What golib DOES enforce is below: transaction control that reaches
// this face poisons the handle (A1-C4).
type SimpleQuerier interface {
	// SimpleQuery sends ONE simple-protocol Query frame carrying sql, then calls
	// emit for every backend message of the response — RowDescription, DataRow,
	// CommandComplete, EmptyQueryResponse, ErrorResponse (as protocol data,
	// ExtendedMessage.Err, never a Go error), and the asynchronous
	// NoticeResponse / ParameterStatus / NotificationResponse — in wire order,
	// AS EACH IS DECODED, and consumes the terminal ReadyForQuery itself,
	// returning its status byte. Multi-statement text yields several groups and
	// ONE terminal ReadyForQuery, exactly as psql sees it.
	//
	// DataRow values are BORROWED for the duration of the emit call (the RawRows
	// rule). Nothing is accumulated, paged, or truncated by the driver.
	//
	// Legal only from the quiescent segment state — the same guard as pinnedTx's
	// own operations; any other state returns ErrSegmentInFlight immediately.
	// The transaction track is orthogonal: an OPEN session transaction is the
	// normal case. Inside emit the handle is private and NO handle method
	// deadlocks: Send / Flush / Receive / Sync / SimpleQuery / BeginSessionTx /
	// Release return ErrSegmentInFlight; Discard is honoured at once — it
	// terminalizes the handle and destroys the pool member — and SimpleQuery
	// returns ErrPoisoned as soon as the callback returns, without reading again.
	//
	// A nil emit fails BEFORE dispatch (ErrEmitNil); nothing is written. A
	// non-nil error from emit stops delivery; the driver still drains to the
	// terminal ReadyForQuery so the wire is quiescent, then returns emit's
	// error. A transport failure during send, read or drain poisons the handle
	// and is returned in preference to any emitter error — the wire's state is
	// what the error must describe. An ErrorResponse does NOT poison and needs
	// no Sync: the simple protocol is self-terminating.
	//
	// Transaction control is FORBIDDEN on this face and detected on the wire
	// (A1-C4): a control CommandComplete tag (BEGIN, COMMIT, ROLLBACK, SAVEPOINT,
	// RELEASE, PREPARE TRANSACTION, COMMIT PREPARED, ROLLBACK PREPARED), or a
	// terminal status that crosses between no-transaction and in-transaction
	// relative to the track held before dispatch, poisons the handle and
	// returns ErrTransactionControlOnRawFace. The one legitimate change, a
	// statement failing inside the owned transaction (T→E), is not control.
	SimpleQuery(ctx context.Context, sql string, emit func(ExtendedMessage) error) (status byte, err error)
}

var _ SimpleQuerier = (*pinnedConn)(nil)

var (
	// ErrEmitNil is returned before any state is touched or any frame written.
	ErrEmitNil = errors.New("postgres: SimpleQuery requires a non-nil emit")

	// ErrTransactionControlOnRawFace reports that a transaction-control statement
	// reached the simple-query face. The session transaction has exactly one
	// owner (pinnedTx); control arriving here would split the server's state from
	// txOpen and pinnedTx.closed, so the handle is POISONED — the split is not
	// provably safe to reuse — and Discard reclaims it.
	ErrTransactionControlOnRawFace = errors.New("postgres: transaction control reached the simple-query face; the handle is poisoned")
)

// isTransactionControlTag reports whether a CommandComplete tag names a
// transaction-ownership or boundary control. The set is closed and matches
// PostgreSQL's tags: START TRANSACTION tags as BEGIN, END as COMMIT, ABORT and
// ROLLBACK TO as ROLLBACK. SET TRANSACTION tags as the indistinguishable SET and
// is the consumer gate's alone (A1-C4 (i)).
func isTransactionControlTag(tag string) bool {
	switch tag {
	case "BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE":
		return true
	}
	return strings.HasPrefix(tag, "PREPARE TRANSACTION") ||
		strings.HasPrefix(tag, "COMMIT PREPARED") ||
		strings.HasPrefix(tag, "ROLLBACK PREPARED")
}

// SimpleQuery implements SimpleQuerier. See the interface for the contract.
func (p *pinnedConn) SimpleQuery(ctx context.Context, sql string, emit func(ExtendedMessage) error) (byte, error) {
	if emit == nil {
		return 0, ErrEmitNil // before the guard, before the wire: nothing happens
	}
	release, err := p.beginPrivate() // quiescent + not terminal; marks the wire private
	if err != nil {
		return 0, err
	}
	defer release()

	// The transaction track BEFORE dispatch. The terminal status is compared
	// against it: crossing between no-transaction and in-transaction means the
	// text contained control that bypassed the owner.
	preTx := p.txOpenLoad()

	select {
	case <-ctx.Done():
		return 0, &notDispatchedError{cause: ctx.Err()} // nothing written; wire clean
	default:
	}
	p.frontend.SendQuery(&pgproto3.Query{String: sql})
	if werr := p.writeBuffered(ctx); werr != nil {
		p.poison()
		return 0, werr
	}

	var emitErr error     // the first emitter error; delivery stops, draining continues
	var controlTag string // the first control tag seen, if any
	for {
		msg, rerr := p.readMessage(ctx)
		if rerr != nil {
			// Transport failure outranks everything: the wire's state is unprovable.
			p.poison()
			return 0, &dispatchedError{cause: rerr}
		}
		switch m := msg.(type) {
		case *pgproto3.ReadyForQuery:
			st := m.TxStatus
			crossed := (preTx && st == 'I') || (!preTx && st != 'I')
			if controlTag != "" || crossed {
				p.poison()
				what := controlTag
				if what == "" {
					what = fmt.Sprintf("status %c→%c with no control tag", map[bool]byte{true: 'T', false: 'I'}[preTx], st)
				}
				return 0, fmt.Errorf("%w (%s)", ErrTransactionControlOnRawFace, what)
			}
			if emitErr != nil {
				return st, emitErr
			}
			return st, nil
		case *pgproto3.CommandComplete:
			tag := string(m.CommandTag)
			if controlTag == "" && isTransactionControlTag(tag) {
				controlTag = tag
			}
			if emitErr == nil {
				if e, term := p.emitGuarded(emit, ExtendedMessage{Kind: "CommandComplete", Tag: tag}); term != nil {
					return 0, term
				} else if e != nil {
					emitErr = e
				}
			}
		case *pgproto3.ErrorResponse:
			// Protocol data, not a Go error, and not poison: the server will send
			// its own ReadyForQuery after this.
			if emitErr == nil {
				if e, term := p.emitGuarded(emit, errorResponseMessage(m)); term != nil {
					return 0, term
				} else if e != nil {
					emitErr = e
				}
			}
		default:
			dm, derr := decodeMessage(msg)
			if derr != nil {
				// A message the vocabulary does not know is protocol skew: the frame
				// boundaries are no longer provable.
				p.poison()
				return 0, &dispatchedError{cause: derr}
			}
			if emitErr == nil {
				if e, term := p.emitGuarded(emit, dm); term != nil {
					return 0, term
				} else if e != nil {
					emitErr = e
				}
			}
		}
	}
}

// emitGuarded delivers one message to the consumer with inEmit set, then — under
// the same mu, before any further read — reports whether the handle became
// terminal while the callback ran. A callback that calls Discard (on this
// goroutine or any other) is thereby honoured: Discard skips its wireMu barrier
// because inEmit certifies no I/O is in flight, and SimpleQuery stops here
// instead of reading a connection that is being destroyed. The consumer's own
// error is returned separately so the drain/precedence rules still apply to it.
//
// A PANICKING callback is consumer code failing with the response tail unread:
// the wire is not at a boundary the driver can resume from, so the handle is
// poisoned and inEmit restored BEFORE the panic propagates (PR #22 MF2). The
// deferred private release in SimpleQuery then frees a wire that every face
// refuses; without the poison it would look quiescent and the next query would
// consume this one's remaining frames as its own answer. The panic is not
// recovered here — value and stack reach the caller unchanged.
func (p *pinnedConn) emitGuarded(emit func(ExtendedMessage) error, m ExtendedMessage) (emitErr, terminal error) {
	p.mu.Lock()
	p.inEmit = true
	p.mu.Unlock()
	returned := false
	defer func() {
		if returned {
			return
		}
		p.mu.Lock()
		p.inEmit = false
		p.poisoned = true
		p.mu.Unlock()
	}()
	emitErr = emit(m)
	returned = true
	// Clear inEmit and read terminal state in ONE critical section: a concurrent
	// Discard decides barrier-or-not against inEmit in ITS poison section, so the
	// two orderings are exactly "Discard waits behind our next read" or "we stop
	// before it" — never "Discard skipped the barrier and we read anyway".
	p.mu.Lock()
	p.inEmit = false
	terminal = p.terminalErrLocked()
	p.mu.Unlock()
	return emitErr, terminal
}
