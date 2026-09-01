package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yongjohnlee80/golib/dao"
)

// ADR-0018: the session-pinned connection capability. dao core is untouched;
// everything here lives in the leaf, behind optional interfaces probed by type
// assertion, and pgx's high-level Conn machinery is structurally unreachable on a
// pinned member for the whole pinned lifetime (the handle never hands the *pgx.Conn out
// and its own methods use only the raw pgproto3 face).
//
// The state machine is the ADR's §2.3: two ORTHOGONAL tracks — outbound (what the
// consumer has built on the wire) and inbound (where the response stream stands) — plus
// a poison flag that outranks both, and a private-exchange flag the transaction and
// query paths hold. Refusal of a guarded call is an immediate state inspection under
// [pinnedConn.mu], never a wait for the segment to end (serialization is not refusal).

// Compile-time proof of the capability (ADR-0018 criterion 1). The dao-core proof that
// the base interfaces are unchanged lives in txcapabilities.go's assertion block.
var _ SessionPinner = (*pgxConn)(nil)

// SessionPinner is an optional postgres-driver capability: the ability to pin ONE
// connection exclusively, for a session's lifetime, and hand back a handle that can
// both run raw extended-protocol segments and host the session's transaction on that
// same connection (ADR-0018 §2.1).
//
// Probe it with [SupportsSessionPinning] at the consumer's entry point; a miss reports
// [dao.ErrUnsupported] — never a silent pool fallback, which would put the session's
// transaction and its raw segments on different connections, the exact defect this seam
// exists to make impossible.
type SessionPinner interface {
	// PinSessionConn acquires one pool member and holds it exclusively for the
	// returned handle's lifetime. Every success must be paired with a deferred
	// [PinnedConn.Discard].
	PinSessionConn(ctx context.Context) (PinnedConn, error)
}

// PinnedConn is one PostgreSQL connection, pinned for a session's lifetime (ADR-0018
// §2.2). It has two faces — raw extended-protocol execution, and the session
// transaction — sharing ONE wire, serialized by the state machine (§2.3) rather than a
// bare mutex.
type PinnedConn interface {
	// Send queues ONE extended-protocol frontend frame. It does not flush and does not
	// block on the server: Parse and Bind may be queued back-to-back before a single
	// Flush, exactly as a wire client emits them.
	Send(ctx context.Context, op ExtendedOp) error

	// Flush writes all queued frames to the wire followed by a protocol Flush frame, so
	// the server emits the group's responses without ending the exchange; bounded by
	// ctx. An inbound receiving state is preserved: a resumed group's bytes may go out
	// while an earlier group's messages are still arriving.
	Flush(ctx context.Context) error

	// Receive returns the next backend message of the current response group, blocking
	// bounded by ctx. DataRow payloads are BORROWED: valid only until the next Receive;
	// a consumer that keeps a row copies it (bytes.Clone, per the RawRows rule). A
	// server ErrorResponse arrives as [ExtendedMessage.Err] (protocol data), not a Go
	// error — after it the inbound track enters the discarding phase and the consumer's
	// next Sync ends it.
	Receive(ctx context.Context) (ExtendedMessage, error)

	// Sync sends ONE Sync frame and consumes through the terminal ReadyForQuery,
	// returning its status byte. This is the ONLY call that returns the wire to the
	// quiescent state (both tracks reset); a discard-through-Sync after an ErrorResponse
	// is this same single call.
	Sync(ctx context.Context) (byte, error)

	// BeginSessionTx opens the session's transaction ON THE PINNED CONNECTION,
	// returning a guarded [dao.ContextTxConn] (ADR-0018 §2.4). Requires the quiescent
	// state; anything else is an immediate [ErrSegmentInFlight].
	BeginSessionTx(ctx context.Context, opts dao.TxOptions) (dao.ContextTxConn, error)

	// Release returns the connection to the pool and makes the handle TERMINAL: it
	// requires the quiescent state and no open transaction (otherwise ErrSegmentInFlight
	// / ErrTxStillOpen immediately), and after a successful Release every face refuses
	// with ErrReleased — the connection may already belong to another caller.
	Release(ctx context.Context) error

	// Discard is the idempotent TERMINAL operation: it always relinquishes the pool
	// lease, closing the physical connection when safe reuse cannot be proven. Every
	// PinSessionConn success carries a deferred Discard; repeated calls are no-ops.
	Discard()
}

// SupportsSessionPinning reports whether conn can pin a session connection (ADR-0018
// §2.1). It is the capability-honest probe: a false answer means the consumer must
// report [dao.ErrUnsupported], never fall back to the pool.
func SupportsSessionPinning(conn dao.DataConn) bool {
	_, ok := conn.(SessionPinner)
	return ok
}

// PinSessionConn probes conn for [SessionPinner] and pins a connection, or reports
// [dao.ErrUnsupported] when conn lacks the capability (ADR-0018 criterion 9). It is the
// typed entry point a consumer calls instead of asserting the interface itself, mirroring
// [dao.BeginConnTx]: a miss is an error the caller handles, never a panic and never a
// silent fallback to the pool.
func PinSessionConn(ctx context.Context, conn dao.DataConn) (PinnedConn, error) {
	sp, ok := conn.(SessionPinner)
	if !ok {
		return nil, fmt.Errorf("postgres: %w: session pinning (SessionPinner not implemented by %T)", dao.ErrUnsupported, conn)
	}
	return sp.PinSessionConn(ctx)
}

// outboundState is what the CONSUMER has built on the wire (ADR-0018 §2.3).
type outboundState int

const (
	idleOut  outboundState = iota // nothing queued, socket drained
	building                      // Send queued ≥1 frame, not flushed
	flushed                       // Flush wrote them; bytes on the wire
)

// inboundState is where the RESPONSE stream stands (ADR-0018 §2.3).
type inboundState int

const (
	noInbound  inboundState = iota // no response group expected or started
	receiving                      // messages of the group still arriving
	discarding                     // ErrorResponse seen; consuming to Sync
)

// ErrSegmentInFlight reports a guarded call made while the wire is not quiescent. It is
// typed and immediate: the guard inspects persistent state under the handle's
// synchronization and returns; it never waits for the segment to end (ADR-0018 §2.3 —
// serialization is not refusal).
var ErrSegmentInFlight = errors.New("postgres: an extended segment is in flight")

// ErrPoisoned reports any operation on a handle whose wire is unprovable after a
// transport-level failure. The only legal next call is [PinnedConn.Discard].
var ErrPoisoned = errors.New("postgres: the pinned connection is poisoned")

// ErrTxStillOpen reports a [PinnedConn.Release] attempted while the pinned transaction
// has not been finalized.
var ErrTxStillOpen = errors.New("postgres: the pinned transaction is still open")

// ErrReleased reports any operation on a handle whose connection has been returned to
// the pool by [PinnedConn.Release]. The handle is terminal; the connection may already
// belong to another caller.
var ErrReleased = errors.New("postgres: the pinned connection has been released to the pool")

// ErrPrematureReadyForQuery reports a ReadyForQuery observed by [PinnedConn.Receive] —
// a contract violation, because the terminal ReadyForQuery belongs to Sync and the
// consumer sent none (ADR-0018 §2.3). The driver does not silently absorb protocol skew.
var ErrPrematureReadyForQuery = errors.New("postgres: premature ReadyForQuery in Receive — a terminal ReadyForQuery belongs to Sync, and none was sent")

// pinnedConn is the ADR-0018 handle: one acquired pool member plus the state machine
// that serializes every face against every other.
//
// Two locks, never held simultaneously by design:
//   - mu guards the state fields and the pool lease. It is held only briefly, never
//     across blocking I/O, so a guarded call's refusal is immediate.
//   - wireMu owns the physical wire during a blocking read/write. It exists so that
//     Discard can barrier behind an in-flight I/O before it destroys the connection —
//     the pgconn contract forbids Close racing an in-flight operation on the same conn.
type pinnedConn struct {
	pool    *pgxpool.Pool
	typeMap *pgtype.Map

	// frontend and pgConn are the raw face of the pinned member; netConn is the
	// underlying socket, used to bound writes with a deadline and to interrupt a
	// blocked read during Discard. All three are written once at construction and never
	// mutated, so they are read without mu.
	frontend *pgproto3.Frontend
	pgConn   *pgconn.PgConn
	netConn  net.Conn

	mu       sync.Mutex
	acq      *pgxpool.Conn // niled by Discard; the goroutine that nils it owns the release
	out      outboundState
	in       inboundState
	poisoned bool
	// released records a successful Release: the pool member is gone and the handle is
	// TERMINAL (MF1). Every face refuses with ErrReleased thereafter — the frontend,
	// pgConn and netConn still point at a connection the pool may have handed to another
	// goroutine, so touching them would corrupt a stranger's wire.
	released bool
	// writing is held by the one wire-write in progress (Flush or Sync). It is claimed
	// under mu BEFORE the write starts, so a concurrent Send/Flush/Sync refuses rather
	// than mutating pgproto3's shared write buffer mid-Write (MF3). Receive does not set
	// it — the Send-during-Receive resume path stays open.
	writing bool
	// wirePrivate is set while a transaction or private-query exchange owns the wire.
	// A guarded call refuses immediately when it is set — the private exchange does not
	// touch the (out, in) tracks (it consumes its own terminal ReadyForQuery), so this
	// flag is what makes "quiescent" honest during one.
	wirePrivate bool
	// txOpen records the pinnedTx lifecycle: BeginSessionTx sets it, the finalizers
	// clear it, Release refuses while it is set.
	txOpen bool

	wireMu sync.Mutex
}

// PinSessionConn acquires one pool member and holds it for the handle's lifetime
// (ADR-0018 §2.1).
func (c *pgxConn) PinSessionConn(ctx context.Context) (PinnedConn, error) {
	acq, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres: pinning a session connection: %w", err)
	}
	conn := acq.Conn()
	pg := conn.PgConn()
	return &pinnedConn{
		pool:     c.pool,
		typeMap:  conn.TypeMap(),
		frontend: pg.Frontend(),
		pgConn:   pg,
		netConn:  pg.Conn(),
		acq:      acq,
	}, nil
}

// terminalErrLocked reports the terminal state's error, or nil when the handle is live.
// Poison outranks release (a poisoned wire is never returned to the pool). Callers hold
// mu.
func (p *pinnedConn) terminalErrLocked() error {
	switch {
	case p.poisoned:
		return ErrPoisoned
	case p.released:
		return ErrReleased
	default:
		return nil
	}
}

// quiescentLocked reports whether the wire is at the both-tracks-reset, no-private-
// exchange, no-write-in-progress state. Callers hold mu.
func (p *pinnedConn) quiescentLocked() bool {
	return p.out == idleOut && p.in == noInbound && !p.wirePrivate && !p.writing
}

// Send queues ONE frontend frame (ADR-0018 §2.3's Send row). It is non-blocking: the
// frame is buffered on the frontend and nothing reaches the server until Flush. The
// resume transition flushed→building is legal — group B queues while group A's
// responses are still inbound; the inbound track is UNCHANGED by Send.
func (p *pinnedConn) Send(_ context.Context, op ExtendedOp) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.terminalErrLocked(); err != nil {
		return err
	}
	if p.writing {
		return fmt.Errorf("%w: a flush owns the write buffer", ErrSegmentInFlight)
	}
	if p.wirePrivate {
		return fmt.Errorf("%w: a transaction or query exchange owns the wire", ErrSegmentInFlight)
	}
	if p.in == discarding {
		return fmt.Errorf("%w: the segment is discarding to Sync", ErrSegmentInFlight)
	}
	if err := op.encode(p.frontend); err != nil {
		return err
	}
	p.out = building
	return nil
}

// Flush writes the queued frames (ADR-0018 §2.3's Flush row). It preserves an inbound
// receiving track: group A keeps streaming while group B's bytes go out. A write
// failure poisons the handle.
func (p *pinnedConn) Flush(ctx context.Context) error {
	p.mu.Lock()
	if err := p.terminalErrLocked(); err != nil {
		p.mu.Unlock()
		return err
	}
	if p.writing {
		p.mu.Unlock()
		return fmt.Errorf("%w: a flush already owns the write buffer", ErrSegmentInFlight)
	}
	if p.wirePrivate {
		p.mu.Unlock()
		return fmt.Errorf("%w: a transaction or query exchange owns the wire", ErrSegmentInFlight)
	}
	if p.in == discarding {
		p.mu.Unlock()
		return fmt.Errorf("%w: the segment is discarding to Sync", ErrSegmentInFlight)
	}
	if p.out != building {
		p.mu.Unlock()
		return fmt.Errorf("%w: nothing queued to flush", ErrSegmentInFlight)
	}
	// Claim the write buffer BEFORE dropping mu, so a concurrent Send/Sync refuses
	// rather than mutating pgproto3's shared wbuf while Write reads and resets it (MF3).
	p.writing = true
	p.mu.Unlock()

	if err := ctx.Err(); err != nil {
		p.clearWriting()
		return err
	}
	p.wireMu.Lock()
	// The protocol Flush ('H') frame makes the server emit the responses to every frame
	// processed so far WITHOUT ending the exchange — it is what lets Receive serve a
	// group message-at-a-time before the consumer's Sync (ADR-0018 §2.3, criterion 2). A
	// TCP flush alone would leave them in the server's output buffer until Sync.
	p.frontend.Send(&pgproto3.Flush{})
	err := p.writeBuffered(ctx)
	p.wireMu.Unlock()

	p.mu.Lock()
	p.writing = false
	if err == nil {
		p.out = flushed
	}
	p.mu.Unlock()
	if err != nil {
		p.poison()
		return err
	}
	return nil
}

// Receive returns the next backend message (ADR-0018 §2.3's Receive row). The blocking
// read runs under wireMu but not mu, so a concurrent guarded call stays immediate while
// it blocks. ErrorResponse is protocol data (drives inbound→discarding); a terminal
// ReadyForQuery is a contract violation; a transport error poisons.
func (p *pinnedConn) Receive(ctx context.Context) (ExtendedMessage, error) {
	p.mu.Lock()
	if err := p.terminalErrLocked(); err != nil {
		p.mu.Unlock()
		return ExtendedMessage{}, err
	}
	if p.wirePrivate {
		p.mu.Unlock()
		return ExtendedMessage{}, fmt.Errorf("%w: a transaction or query exchange owns the wire", ErrSegmentInFlight)
	}
	if p.in == discarding {
		p.mu.Unlock()
		return ExtendedMessage{}, fmt.Errorf("%w: the segment is discarding to Sync", ErrSegmentInFlight)
	}
	if p.out != flushed && p.out != building {
		p.mu.Unlock()
		return ExtendedMessage{}, fmt.Errorf("%w: no flushed group to receive", ErrSegmentInFlight)
	}
	p.mu.Unlock()

	p.wireMu.Lock()
	msg, err := p.readMessage(ctx)
	p.wireMu.Unlock()
	if err != nil {
		p.poison()
		return ExtendedMessage{}, err
	}

	switch m := msg.(type) {
	case *pgproto3.ErrorResponse:
		p.mu.Lock()
		p.in = discarding
		p.mu.Unlock()
		return errorResponseMessage(m), nil
	case *pgproto3.ReadyForQuery:
		return ExtendedMessage{}, ErrPrematureReadyForQuery
	default:
		p.mu.Lock()
		if p.in == noInbound {
			p.in = receiving
		}
		p.mu.Unlock()
		return decodeMessage(msg)
	}
}

// Sync sends ONE Sync frame and consumes through the terminal ReadyForQuery (ADR-0018
// §2.3's Sync row): the single call that resets both tracks. It is legal from any
// non-poisoned, non-private state.
func (p *pinnedConn) Sync(ctx context.Context) (byte, error) {
	p.mu.Lock()
	if err := p.terminalErrLocked(); err != nil {
		p.mu.Unlock()
		return 0, err
	}
	if p.writing {
		p.mu.Unlock()
		return 0, fmt.Errorf("%w: a flush owns the write buffer", ErrSegmentInFlight)
	}
	if p.wirePrivate {
		p.mu.Unlock()
		return 0, fmt.Errorf("%w: a transaction or query exchange owns the wire", ErrSegmentInFlight)
	}
	p.writing = true
	p.mu.Unlock()

	if err := ctx.Err(); err != nil {
		p.clearWriting()
		return 0, err
	}
	p.wireMu.Lock()
	status, err := p.syncLocked(ctx)
	p.wireMu.Unlock()

	p.mu.Lock()
	p.writing = false
	if err == nil {
		p.out = idleOut
		p.in = noInbound
	}
	p.mu.Unlock()
	if err != nil {
		p.poison()
		return 0, err
	}
	return status, nil
}

// clearWriting releases the write-buffer claim after a pre-write refusal.
func (p *pinnedConn) clearWriting() {
	p.mu.Lock()
	p.writing = false
	p.mu.Unlock()
}

// syncLocked emits one Sync frame and drains to the terminal ReadyForQuery. The caller
// holds wireMu.
func (p *pinnedConn) syncLocked(ctx context.Context) (byte, error) {
	p.frontend.SendSync(&pgproto3.Sync{})
	if err := p.writeBuffered(ctx); err != nil {
		return 0, err
	}
	return p.drainToReady(ctx)
}

// BeginSessionTx opens the session transaction on the pinned wire via a raw
// simple-protocol BEGIN and returns the guarded pinnedTx (ADR-0018 §2.4). It requires
// the quiescent state and no already-open transaction.
func (p *pinnedConn) BeginSessionTx(ctx context.Context, opts dao.TxOptions) (dao.ContextTxConn, error) {
	sql, err := beginSQL(opts)
	if err != nil {
		return nil, err
	}
	release, err := p.beginPrivate()
	if err != nil {
		return nil, err
	}
	defer release()

	if p.txOpenLoad() {
		return nil, ErrTxStillOpen
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tag, srvErr, _, txErr := p.execSimpleLocked(ctx, sql)
	if txErr != nil {
		return nil, fmt.Errorf("postgres: pinned BEGIN failed: %w", txErr)
	}
	if srvErr != nil {
		return nil, translateError(srvErr)
	}
	if tag != "BEGIN" {
		return nil, fmt.Errorf("postgres: pinned BEGIN did not take effect (server returned %q)", tag)
	}
	p.mu.Lock()
	p.txOpen = true
	p.mu.Unlock()
	return &pinnedTx{p: p, ctx: ctx}, nil
}

// Release returns the connection to the pool (ADR-0018 §2.2): it refuses while the wire
// is mid-segment or the transaction is open. Discard is the unconditional counterpart.
func (p *pinnedConn) Release(_ context.Context) error {
	p.mu.Lock()
	if err := p.terminalErrLocked(); err != nil {
		p.mu.Unlock()
		return err
	}
	// A mid-flight segment is reported before an open transaction: it is the more
	// immediate fact about the wire (criterion 6), and the transaction cannot be
	// finalized until it ends anyway.
	if !p.quiescentLocked() {
		p.mu.Unlock()
		return ErrSegmentInFlight
	}
	if p.txOpen {
		p.mu.Unlock()
		return ErrTxStillOpen
	}
	// Terminalize UNDER mu, before the lease goes back: the moment acq.Release runs the
	// pool may hand this member to another goroutine, and every face must already refuse
	// (MF1). released is set here, not after, so no concurrent Send can slip in between.
	p.released = true
	acq := p.acq
	p.acq = nil
	p.mu.Unlock()
	if acq != nil {
		acq.Release()
	}
	return nil
}

// Discard is the idempotent terminal operation (ADR-0018 §2.2): it always relinquishes
// the pool lease; the pool destroys a member whose TxStatus is not idle, whose wire is
// busy, or that is closed, which is exactly the ADR's "discarded, never dirty" rule.
//
// Discard is safe to call while another goroutine holds a blocking read (the deferred-
// Discard-racing-Receive case, criterion 10): it poisons first (so no NEW wire op
// proceeds), interrupts any in-flight read/write by shortening the socket deadline, then
// barriers on wireMu so the in-flight op has fully released the connection before the
// pool destroys it. This avoids the pgconn contract violation of closing a conn under an
// active operation.
func (p *pinnedConn) Discard() {
	p.mu.Lock()
	if p.acq == nil {
		p.mu.Unlock()
		return
	}
	// Reuse is provable only when nothing is in flight, nothing is poisoned, and no
	// server-side transaction is open. Anything else — a mid-flight segment, a poisoned
	// wire, an unfinalized transaction — is a member the pool must NOT recycle. The
	// pool's own dirty test (busy / non-idle TxStatus / closed) does not see a
	// read-timeout-poisoned wire with unread responses, so the decision is made here.
	reusable := !p.poisoned && !p.released && !p.txOpen && p.quiescentLocked()
	p.poisoned = true
	acq := p.acq
	p.acq = nil
	p.mu.Unlock()

	if !reusable {
		// Interrupt any in-flight blocking read/write. Setting a past deadline is the
		// documented way to unblock a pending net.Conn Read; it is safe to call
		// concurrently with that Read.
		_ = p.netConn.SetDeadline(time.Now())
	}
	// Barrier: no wire op is mid-flight past this point (poisoned refuses new ones).
	p.wireMu.Lock()
	p.wireMu.Unlock() //nolint:staticcheck // intentional barrier, see above
	if !reusable {
		// Close the physical connection (best-effort Terminate, bounded) so the pool's
		// Release sees a closed member and destroys it rather than recycling a dirty one.
		_ = p.netConn.SetDeadline(time.Time{})
		ctx, cancel := context.WithTimeout(context.Background(), discardTeardownTimeout)
		_ = p.pgConn.Close(ctx)
		cancel()
	}
	acq.Release()
}

// discardTeardownTimeout bounds Discard's best-effort Terminate on an unprovable wire.
const discardTeardownTimeout = 2 * time.Second

// poison marks the handle terminal after a transport-level failure. A connection whose
// frame boundaries are unprovable is a discarded member: every subsequent operation
// except Discard returns ErrPoisoned.
func (p *pinnedConn) poison() {
	p.mu.Lock()
	p.poisoned = true
	p.mu.Unlock()
}

// txOpenLoad reads txOpen under mu.
func (p *pinnedConn) txOpenLoad() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.txOpen
}

// beginPrivate takes the wire for a transaction or private-query exchange. It verifies
// the handle is quiescent and not poisoned, marks the wire private (so a concurrent
// guarded call refuses immediately), and acquires wireMu. The returned release restores
// the flag and frees the wire; it must be deferred by the caller.
func (p *pinnedConn) beginPrivate() (func(), error) {
	p.mu.Lock()
	if err := p.terminalErrLocked(); err != nil {
		p.mu.Unlock()
		return nil, err
	}
	if !p.quiescentLocked() {
		p.mu.Unlock()
		return nil, ErrSegmentInFlight
	}
	p.wirePrivate = true
	p.mu.Unlock()

	p.wireMu.Lock()
	// A Discard may have poisoned between the flag set and the wire acquisition.
	p.mu.Lock()
	termErr := p.terminalErrLocked()
	p.mu.Unlock()
	if termErr != nil {
		p.wireMu.Unlock()
		p.mu.Lock()
		p.wirePrivate = false
		p.mu.Unlock()
		return nil, termErr
	}
	return func() {
		p.mu.Lock()
		p.wirePrivate = false
		p.mu.Unlock()
		p.wireMu.Unlock()
	}, nil
}

// writeBuffered flushes the frontend's buffered frames to the socket, bounded by ctx.
// The caller owns the wire (holds wireMu). pgproto3's Flush has no context, so the write
// is bounded two ways (MF2): a deadline when ctx carries one, AND a watcher that
// shortens the socket deadline the instant ctx is cancelled — a cancellable context with
// NO deadline, or cancellation before a later deadline, must still interrupt a blocked
// Write per PinnedConn.Flush's contract and ADR §2.3. The watcher's teardown is
// synchronized (wg.Wait) before the deadline is cleared, so it can never strand a past
// deadline on the next operation, and the raw context cause is preserved over pgconn's
// timeout wrapper. The caller poisons on any error — a cancellation mid-write is a
// transport-level outcome.
func (p *pinnedConn) writeBuffered(ctx context.Context) error {
	done := ctx.Done()
	if done == nil {
		return p.frontend.Flush() // non-cancellable context: no watcher needed
	}
	if err := ctx.Err(); err != nil {
		return err // already cancelled: nothing is dispatched
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := p.netConn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-done:
			_ = p.netConn.SetWriteDeadline(time.Now())
		case <-stop:
		}
	}()
	err := p.frontend.Flush()
	close(stop)
	wg.Wait()
	_ = p.netConn.SetWriteDeadline(time.Time{})
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
	}
	return err
}

// readMessage reads one backend message on the raw face, bounded by ctx (pgconn's own
// context watcher applies the deadline). The caller owns the wire (holds wireMu). A
// non-FATAL server ErrorResponse is returned AS a message, not a Go error — pgconn's
// default handler only closes on FATAL, and on this raw face an ErrorResponse is
// protocol data either way.
func (p *pinnedConn) readMessage(ctx context.Context) (pgproto3.BackendMessage, error) {
	return p.pgConn.ReceiveMessage(ctx)
}

// drainToReady consumes messages until the terminal ReadyForQuery, which ends every
// group and every discard (ADR-0018 §2.3). The caller owns the wire.
func (p *pinnedConn) drainToReady(ctx context.Context) (byte, error) {
	for {
		msg, err := p.readMessage(ctx)
		if err != nil {
			return 0, err
		}
		if ready, ok := msg.(*pgproto3.ReadyForQuery); ok {
			return ready.TxStatus, nil
		}
	}
}

// execSimpleLocked drives one simple-protocol Query frame and drains every result group
// to the terminal ReadyForQuery. It is the raw path for BEGIN/COMMIT/ROLLBACK and for a
// no-argument ExecContext. The caller owns the wire (from beginPrivate).
//
// Returns: the LAST CommandComplete tag seen, the FIRST server ErrorResponse (as a
// *pgconn.PgError, nil if none), the final ReadyForQuery status byte, and a transport
// error (nil unless the wire itself failed). A server error is protocol data — reported
// through srvErr, not err — so the caller classifies it rather than poisoning.
//
// Error shapes are DISPATCH-AWARE so classifyCommit can tell fault state 2 from 4:
//   - ctx already done before the write → *notDispatchedError (SafeToRetry: nothing
//     reached the server; the wire is clean and NOT poisoned);
//   - the write itself fails → pgproto3's own write error (SafeToRetry only if zero
//     bytes went out) and the handle is poisoned;
//   - the write succeeded and the read fails → *dispatchedError (never SafeToRetry: the
//     answer is unprovable) and the handle is poisoned.
//
// pgconn marks every ReceiveMessage failure SafeToRetry because its own exec path checks
// the context BEFORE writing; on this raw face the frame is already on the wire by the
// time the read runs, so that flag must not leak through unqualified.
func (p *pinnedConn) execSimpleLocked(ctx context.Context, sql string) (tag string, srvErr *pgconn.PgError, status byte, err error) {
	select {
	case <-ctx.Done():
		return "", nil, 0, &notDispatchedError{cause: ctx.Err()}
	default:
	}
	p.frontend.SendQuery(&pgproto3.Query{String: sql})
	if werr := p.writeBuffered(ctx); werr != nil {
		p.poison()
		return "", nil, 0, werr
	}
	for {
		msg, rerr := p.readMessage(ctx)
		if rerr != nil {
			p.poison()
			return "", nil, 0, &dispatchedError{cause: rerr}
		}
		switch m := msg.(type) {
		case *pgproto3.CommandComplete:
			tag = string(m.CommandTag)
		case *pgproto3.ErrorResponse:
			if srvErr == nil {
				srvErr = pgconn.ErrorResponseToPgError(m)
			}
		case *pgproto3.ReadyForQuery:
			return tag, srvErr, m.TxStatus, nil
		}
	}
}

// beginSQL renders opts as a simple-protocol BEGIN statement. Options are validated
// first, so a malformed set never reaches the wire (the same fail-before-BEGIN contract
// the pool path gives). PostgreSQL accepts the transaction modes space-separated in any
// order.
func beginSQL(opts dao.TxOptions) (string, error) {
	if err := opts.Validate(); err != nil {
		return "", err
	}
	sql := "BEGIN"
	if iso := opts.Isolation.String(); iso != "" {
		sql += " ISOLATION LEVEL " + iso
	}
	if access := opts.Access.String(); access != "" {
		sql += " " + access
	}
	if def := opts.Deferrable.String(); def != "" {
		sql += " " + def
	}
	return sql, nil
}

// notDispatchedError reports a frame that was never written because the context was
// already done at dispatch. It PROVES nothing reached the server, so it is safe to
// retry (pgconn.SafeToRetry → true) and the wire stays clean — the same shape pgconn's
// own pre-write refusal has, which is what classifyCommit's fault state 2 keys on.
type notDispatchedError struct{ cause error }

func (e *notDispatchedError) Error() string { return "postgres: not dispatched: " + e.cause.Error() }
func (e *notDispatchedError) Unwrap() error { return e.cause }

// SafeToRetry reports true: nothing was written.
func (e *notDispatchedError) SafeToRetry() bool { return true }

// dispatchedError reports a frame that WAS written and whose answer did not come back
// (read failure, timeout, cancellation mid-read). The outcome is unprovable, so it is
// never safe to retry — classifyCommit's fault state 4. It answers SafeToRetry itself so
// that pgconn's blanket SafeToRetry on the underlying receive error cannot be reached by
// errors.As ahead of it; the cause stays reachable through Unwrap for every other match.
type dispatchedError struct{ cause error }

func (e *dispatchedError) Error() string {
	return "postgres: response lost after dispatch: " + e.cause.Error()
}
func (e *dispatchedError) Unwrap() error { return e.cause }

// SafeToRetry reports false: the frame reached the wire.
func (e *dispatchedError) SafeToRetry() bool { return false }
