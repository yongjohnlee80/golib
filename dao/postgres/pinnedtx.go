package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

// The guarded pinned transaction. The handle does NOT return the
// pool-path pgxTx: that wrapper's methods call pgx directly and know nothing of the
// handle's state, so handing it out would make the enforcement a lie. pinnedTx
// shares the handle's lock and state machine — every operation first inspects the
// handle state and proceeds only from quiescent; anything else is an immediate typed
// ErrSegmentInFlight — and drives BEGIN/COMMIT/ROLLBACK through the RAW face as
// simple-protocol text, so no pgx cache machinery ever runs on the pinned wire. Outcome
// classification is delegated to the helper classifyCommit: the same code the
// pool path uses, shared, not duplicated.

// Compile-time proof that the guarded transaction honors the published contracts.
var (
	_ dao.TxConn        = (*pinnedTx)(nil)
	_ dao.ContextTxConn = (*pinnedTx)(nil)
	_ dao.Rows          = (*pinnedRows)(nil)
	_ dao.RawRows       = (*pinnedRows)(nil)
	_ dao.RowsColumns   = (*pinnedRows)(nil)
)

// pinnedTx is a dao.ContextTxConn hosted on a pinned connection.
type pinnedTx struct {
	p *pinnedConn
	// ctx is the BEGIN context. The legacy no-context finalizers dispatch on it, exactly
	// as the pool-path pgxTx does (/).
	ctx context.Context

	// closed records that a finalizer has DISPATCHED (or, for the legacy finalizers,
	// was attempted — they mark the handle closed BEFORE dispatch, the legacy shape).
	// It is guarded by p.mu so a concurrent finalizer attempt is
	// race-clean even though one transaction is single-goroutine by contract.
	closed bool
}

// --- finalizers -------------------------------------------------------------------

// Commit is the unchanged dao.TxConn finalizer: it dispatches on the BEGIN context and
// marks the handle closed BEFORE dispatch, so a cancelled BEGIN context fails with the
// context error and leaves the handle TERMINAL (the legacy shape).
// A mid-segment call is refused first and leaves the transaction open.
func (t *pinnedTx) Commit() error { return t.finalizeLegacy(commitSQL) }

// Rollback is the unchanged dao.TxConn finalizer; see Commit for the legacy shape.
func (t *pinnedTx) Rollback() error { return t.finalizeLegacy(rollbackSQL) }

// CommitContext commits with ctx bounding the COMMIT, satisfying dao.ContextTxConn. The
// outcome is classified per through the shared helper: fault state 1 (ctx
// already dead) returns the raw context error and leaves the handle OPEN; the other
// three states arrive as ErrTxRolledBack / ErrTxOutcomeUnknown with the cause preserved.
func (t *pinnedTx) CommitContext(ctx context.Context) error {
	if err := t.finalizeContext(ctx, commitSQL); err != nil {
		if errors.Is(err, dao.ErrTransactionClosed) || errors.Is(err, ErrSegmentInFlight) || errors.Is(err, ErrPoisoned) {
			return err
		}
		if err == ctx.Err() {
			return err // fault state 1: nothing dispatched; handle stays open
		}
		return classifyCommit(err)
	}
	return nil
}

// RollbackContext rolls back with ctx bounding the ROLLBACK, satisfying
// dao.ContextTxConn. A failed rollback is reported, never swallowed.
func (t *pinnedTx) RollbackContext(ctx context.Context) error {
	if err := t.finalizeContext(ctx, rollbackSQL); err != nil {
		if errors.Is(err, dao.ErrTransactionClosed) || errors.Is(err, ErrSegmentInFlight) || errors.Is(err, ErrPoisoned) {
			return err
		}
		if err == ctx.Err() {
			return err
		}
		return fmt.Errorf("postgres: rollback failed: %w", err)
	}
	return nil
}

const (
	commitSQL   = "COMMIT"
	rollbackSQL = "ROLLBACK"
)

// finalizeLegacy is the body of Commit/Rollback. Order: closed → ErrTransactionClosed;
// mid-segment → ErrSegmentInFlight (transaction stays open); then closed=true BEFORE
// dispatch; then the pre-dispatch context check on the BEGIN context (terminal on
// failure, the legacy shape); then the raw exchange.
func (t *pinnedTx) finalizeLegacy(sql string) error {
	if t.isClosed() {
		return fmt.Errorf("postgres: %w: %s", dao.ErrTransactionClosed, lower(sql))
	}
	release, err := t.p.beginPrivate()
	if err != nil {
		return err
	}
	defer release()
	if t.isClosed() { // re-check under the wire: a racing finalizer may have won
		return fmt.Errorf("postgres: %w: %s", dao.ErrTransactionClosed, lower(sql))
	}
	t.setClosed()
	if err := t.ctx.Err(); err != nil {
		return err
	}
	return t.dispatchFinalizerLocked(t.ctx, sql)
}

// finalizeContext is the body of CommitContext/RollbackContext. Order: closed →
// ErrTransactionClosed; mid-segment → ErrSegmentInFlight; ctx already dead → the raw
// context error with the handle STILL OPEN (fault state 1); then closed=true and
// the raw exchange.
func (t *pinnedTx) finalizeContext(ctx context.Context, sql string) error {
	if t.isClosed() {
		return fmt.Errorf("postgres: %w: %s", dao.ErrTransactionClosed, lower(sql))
	}
	release, err := t.p.beginPrivate()
	if err != nil {
		return err
	}
	defer release()
	if t.isClosed() {
		return fmt.Errorf("postgres: %w: %s", dao.ErrTransactionClosed, lower(sql))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	t.setClosed()
	return t.dispatchFinalizerLocked(ctx, sql)
}

// dispatchFinalizerLocked runs COMMIT or ROLLBACK over the raw simple-protocol face and
// returns the failure in the SAME SHAPES the pool path's pgx finalizer would produce, so
// classifyCommit applies unchanged:
//   - a ROLLBACK command tag answering a COMMIT → pgx.ErrTxCommitRollback (the
//     transaction was already aborted; definitely not committed);
//   - a server ErrorResponse → the *pgconn.PgError;
//   - a transport failure → execSimpleLocked's dispatch-aware error: not-dispatched
//     (SafeToRetry, nothing written, wire clean) or dispatched-and-lost (never
//     SafeToRetry, handle poisoned).
//
// txOpen is cleared whenever the frame reached the wire (the server transaction is over
// or unprovable); the not-dispatched case leaves it set because the server transaction
// is provably still open. In every failure case Discard is the lease's cleanup, as the
// ADR requires. The caller owns the wire.
func (t *pinnedTx) dispatchFinalizerLocked(ctx context.Context, sql string) error {
	tag, srvErr, _, err := t.p.execSimpleLocked(ctx, sql)
	var nd *notDispatchedError
	if errors.As(err, &nd) {
		// Nothing was written: the server-side transaction is STILL OPEN and the wire
		// is clean. The handle is terminal (closed), so txOpen stays set — Release
		// refuses with ErrTxStillOpen and Discard closes the physical connection.
		return err
	}
	t.p.mu.Lock()
	t.p.txOpen = false
	t.p.mu.Unlock()
	if err != nil {
		return err // transport failure: execSimpleLocked already poisoned the handle
	}
	if srvErr != nil {
		return srvErr
	}
	if sql == commitSQL && tag == "ROLLBACK" {
		return pgx.ErrTxCommitRollback
	}
	return nil
}

func (t *pinnedTx) isClosed() bool {
	t.p.mu.Lock()
	defer t.p.mu.Unlock()
	return t.closed
}

func (t *pinnedTx) setClosed() {
	t.p.mu.Lock()
	t.closed = true
	t.p.mu.Unlock()
}

// lower spells a finalizer verb for an error message.
func lower(sql string) string {
	if sql == commitSQL {
		return "commit"
	}
	return "rollback"
}

// --- the private query/exec path ---------------------------------------------------

// ExecContext runs q on the pinned wire from quiescent (method-shaped
// dispatch): with no arguments it is ONE simple-protocol Query frame draining every
// result group — pgx's own no-args behavior, so multi-statement text keeps working — and
// with arguments it is the private unnamed extended sequence. A server error is
// translated to the dao sentinels like the pool path; the wire returns to quiescent
// before this returns (or the handle is poisoned).
func (t *pinnedTx) ExecContext(ctx context.Context, q string, args ...any) (dao.Result, error) {
	if t.isClosed() {
		return nil, fmt.Errorf("postgres: %w: exec", dao.ErrTransactionClosed)
	}
	release, err := t.p.beginPrivate()
	if err != nil {
		return nil, err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(args) == 0 {
		tag, srvErr, _, err := t.p.execSimpleLocked(ctx, q)
		if err != nil {
			return nil, err
		}
		if srvErr != nil {
			return nil, translateError(srvErr)
		}
		return pgxResult{tag: pgconn.NewCommandTag(tag)}, nil
	}

	seq := &privateSequence{p: t.p}
	if err := seq.prepare(ctx, q, args); err != nil {
		return nil, err
	}
	// Drain the whole group, then the cleanup exchange.
	for {
		msg, err := seq.next(ctx)
		if err != nil {
			return nil, err
		}
		if msg == nil {
			break
		}
	}
	if err := seq.finish(ctx); err != nil {
		return nil, err
	}
	return pgxResult{tag: pgconn.NewCommandTag(seq.tag)}, nil
}

// QueryContext runs q through the private unnamed extended sequence (for zero or more
// args, preserving the pool path's extended-by-default behavior) and streams the result
// group's rows into a driver-owned dao.Rows whose RawValues carries the borrowed wire
// bytes. The wire stays private until Rows.Close, which completes the cleanup contract
// (explicit unnamed Close frames + the private Sync) and returns it to quiescent. An
// undrained Rows keeps every other face refusing with ErrSegmentInFlight.
func (t *pinnedTx) QueryContext(ctx context.Context, q string, args ...any) (dao.Rows, error) {
	if t.isClosed() {
		return nil, fmt.Errorf("postgres: %w: query", dao.ErrTransactionClosed)
	}
	release, err := t.p.beginPrivate()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	seq := &privateSequence{p: t.p}
	if err := seq.prepare(ctx, q, args); err != nil {
		release()
		return nil, err
	}
	return &pinnedRows{seq: seq, ctx: ctx, release: release}, nil
}

// privateSequence is one run of the driver-owned unnamed extended sequence:
// Parse(unnamed) → Describe(S) → ParameterDescription supplies the OIDs → each
// argument encoded in text format with the exported pgtype.Map.Encode → Bind(unnamed
// portal) → Execute → Sync, and then the exit-aware cleanup (r5 MF1): the objects
// ParseComplete/BindComplete PROVED were created are closed in a second Close+Sync
// exchange, on the normal path and on the ErrorResponse path alike, after the group's
// own terminal ReadyForQuery has been consumed — because PostgreSQL discards frontend
// messages after an extended-query ErrorResponse until Sync, a Close queued before the
// recovery Sync would be thrown away.
type privateSequence struct {
	p *pinnedConn

	stmtCreated   bool // ParseComplete seen: the unnamed statement exists server-side
	portalCreated bool // BindComplete seen: the unnamed portal exists server-side
	fields        []pgproto3.FieldDescription
	tag           string
	srvErr        *pgconn.PgError
	groupDone     bool // CommandComplete / EmptyQueryResponse / ErrorResponse seen
	readyDone     bool // the group's terminal ReadyForQuery consumed
	values        [][]byte
}

// prepare runs the sequence up to and including the flush of Bind+Execute+Sync, leaving
// the response group ready to stream. Phase 1 (Parse+Describe+Flush) is a separate
// round trip because the parameter OIDs are needed to encode the arguments. A server
// error at any stage is recovered to quiescent (recovery Sync + tracked cleanup) and
// returned translated; a transport error poisons.
func (s *privateSequence) prepare(ctx context.Context, q string, args []any) error {
	select {
	case <-ctx.Done():
		return &notDispatchedError{cause: ctx.Err()} // nothing written; wire clean
	default:
	}
	fe := s.p.frontend
	// Phase 1: parse + describe, with a protocol Flush so the server emits the
	// ParseComplete/ParameterDescription/RowDescription without ending the exchange.
	fe.SendParse(&pgproto3.Parse{Name: "", Query: q})
	fe.SendDescribe(&pgproto3.Describe{ObjectType: 'S', Name: ""})
	fe.Send(&pgproto3.Flush{})
	if err := s.p.writeBuffered(ctx); err != nil {
		s.p.poison()
		return err
	}
	var paramOIDs []uint32
phase1:
	for {
		msg, err := s.p.readMessage(ctx)
		if err != nil {
			s.p.poison()
			return err
		}
		switch m := msg.(type) {
		case *pgproto3.ParseComplete:
			s.stmtCreated = true
		case *pgproto3.ParameterDescription:
			paramOIDs = m.ParameterOIDs
		case *pgproto3.RowDescription:
			s.fields = cloneFields(m.Fields)
			break phase1
		case *pgproto3.NoData:
			break phase1
		case *pgproto3.ErrorResponse:
			// Nothing after this is processed until Sync; recover, then clean up
			// whatever the acknowledgements proved exists (nothing, on a Parse error).
			s.srvErr = pgconn.ErrorResponseToPgError(m)
			s.groupDone = true
			if err := s.recoverLocked(ctx); err != nil {
				return err
			}
			return translateError(s.srvErr)
		case *pgproto3.NoticeResponse, *pgproto3.ParameterStatus, *pgproto3.NotificationResponse:
			// asynchronous; not part of the group
		default:
			s.p.poison()
			return errs.Wrap(errs.ErrProtocol, "postgres: unexpected %T during private parse/describe", msg)
		}
	}
	if len(paramOIDs) != len(args) {
		// The server disagrees with the caller about arity; recover the wire (the
		// unnamed statement exists) and report it before any Bind is attempted.
		if err := s.recoverLocked(ctx); err != nil {
			return err
		}
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: expected %d arguments, got %d", len(paramOIDs), len(args))
	}
	values, err := encodeTextArgs(s.p.typeMap, paramOIDs, args)
	if err != nil {
		if rerr := s.recoverLocked(ctx); rerr != nil {
			return rerr
		}
		return err
	}
	formats := make([]int16, len(values))
	// Phase 2: bind + execute + Sync. The Sync is the group's own terminal and doubles
	// as the recovery Sync on an Execute-stage error.
	fe.SendBind(&pgproto3.Bind{
		DestinationPortal: "", PreparedStatement: "",
		ParameterFormatCodes: formats, Parameters: values, ResultFormatCodes: nil,
	})
	fe.SendExecute(&pgproto3.Execute{Portal: "", MaxRows: 0})
	fe.SendSync(&pgproto3.Sync{})
	if err := s.p.writeBuffered(ctx); err != nil {
		s.p.poison()
		return err
	}
	return nil
}

// next returns the next DataRow's borrowed values, or nil when the group is complete
// (CommandComplete / EmptyQueryResponse / ErrorResponse seen). BindComplete is consumed
// here. A server ErrorResponse ends the group and is surfaced through srvErr — the
// caller's finish() performs the recovery and cleanup. A transport error poisons.
func (s *privateSequence) next(ctx context.Context) ([][]byte, error) {
	if s.groupDone {
		return nil, nil
	}
	for {
		msg, err := s.p.readMessage(ctx)
		if err != nil {
			s.p.poison()
			return nil, err
		}
		switch m := msg.(type) {
		case *pgproto3.BindComplete:
			s.portalCreated = true
		case *pgproto3.DataRow:
			s.values = m.Values
			return m.Values, nil
		case *pgproto3.CommandComplete:
			s.tag = string(m.CommandTag)
			s.groupDone = true
			return nil, nil
		case *pgproto3.EmptyQueryResponse:
			s.groupDone = true
			return nil, nil
		case *pgproto3.ErrorResponse:
			s.srvErr = pgconn.ErrorResponseToPgError(m)
			s.groupDone = true
			return nil, nil
		case *pgproto3.ReadyForQuery:
			// Only reachable if the server ended the group without a completion
			// message, which the protocol does not do; treat as done-and-quiescent.
			s.groupDone = true
			s.readyDone = true
			return nil, nil
		case *pgproto3.NoticeResponse, *pgproto3.ParameterStatus, *pgproto3.NotificationResponse:
			// asynchronous; not part of the group
		default:
			s.p.poison()
			return nil, errs.Wrap(errs.ErrProtocol, "postgres: unexpected %T during private execute", msg)
		}
	}
}

// finish completes the sequence after the group is done: it consumes the group's
// terminal ReadyForQuery (from the phase-2 Sync), then runs the tracked-creation cleanup
// exchange, and returns the translated server error if the group ended in one. The wire
// is quiescent on return unless a transport error poisoned it.
func (s *privateSequence) finish(ctx context.Context) error {
	// Drain any rows the caller did not read, up to the completion message.
	for !s.groupDone {
		if _, err := s.next(ctx); err != nil {
			return err
		}
	}
	if !s.readyDone {
		if _, err := s.p.drainToReady(ctx); err != nil {
			s.p.poison()
			return err
		}
		s.readyDone = true
	}
	if err := s.cleanupLocked(ctx); err != nil {
		return err
	}
	if s.srvErr != nil {
		return translateError(s.srvErr)
	}
	return nil
}

// recoverLocked handles a server error seen BEFORE the phase-2 Sync was queued (a Parse/
// Describe-stage error or an arity/encoding failure after phase 1): it issues the
// recovery Sync, consumes its terminal ReadyForQuery, then runs the tracked cleanup.
func (s *privateSequence) recoverLocked(ctx context.Context) error {
	s.p.frontend.SendSync(&pgproto3.Sync{})
	if err := s.p.writeBuffered(ctx); err != nil {
		s.p.poison()
		return err
	}
	if _, err := s.p.drainToReady(ctx); err != nil {
		s.p.poison()
		return err
	}
	s.readyDone = true
	return s.cleanupLocked(ctx)
}

// cleanupLocked is the exit-aware cleanup exchange: it closes
// the unnamed statement and/or portal ONLY if their creation was acknowledged, then
// Syncs and consumes the terminal ReadyForQuery. Nothing is sent when nothing was
// created (the blind-Close guard, 's negative arm). Close is legal in an
// aborted transaction, so the error tail reuses it unchanged. The unnamed statement and
// portal are never destroyed by a Sync inside an explicit transaction, which is why this
// exchange exists at all.
func (s *privateSequence) cleanupLocked(ctx context.Context) error {
	if !s.stmtCreated && !s.portalCreated {
		return nil
	}
	fe := s.p.frontend
	if s.portalCreated {
		fe.SendClose(&pgproto3.Close{ObjectType: 'P', Name: ""})
	}
	if s.stmtCreated {
		fe.SendClose(&pgproto3.Close{ObjectType: 'S', Name: ""})
	}
	fe.SendSync(&pgproto3.Sync{})
	if err := s.p.writeBuffered(ctx); err != nil {
		s.p.poison()
		return err
	}
	if _, err := s.p.drainToReady(ctx); err != nil {
		s.p.poison()
		return err
	}
	s.stmtCreated, s.portalCreated = false, false
	return nil
}

// encodeTextArgs encodes args for Bind in text format against the server-declared OIDs,
// via the exported pgtype.Map.Encode — the same encoding pgx performs for an extended
// ExecParams, driven through the raw frames. A nil arg (or a nil driver.Valuer result)
// is the wire's own NULL: a nil slice, which Bind encodes as length -1.
//
// Encode signals NULL by returning a nil slice — but appending ZERO bytes to a nil
// buffer also returns nil, so an empty string encoded into a nil scratch buffer would be
// indistinguishable from NULL and sent as one. Every value is therefore encoded into one
// shared NON-NIL scratch buffer (pgx's own ExtendedQueryBuilder does the same): a
// zero-length encoding comes back as the same non-nil slice, and the empty value is
// returned as a zero-length, non-nil sub-slice — NULL and empty stay distinct, the same
// invariant the RawRows rule protects on the read side.
func encodeTextArgs(m *pgtype.Map, oids []uint32, args []any) ([][]byte, error) {
	out := make([][]byte, len(args))
	scratch := make([]byte, 0, 256)
	for i, a := range args {
		pos := len(scratch)
		nb, err := m.Encode(oids[i], pgtype.TextFormatCode, a, scratch)
		if err != nil {
			return nil, fmt.Errorf("postgres: encoding argument %d: %w", i+1, err)
		}
		if nb == nil {
			out[i] = nil // NULL
			continue
		}
		scratch = nb
		// Three-index slice: a later append past len cannot be observed through this
		// value; a later reallocation leaves the old array intact underneath it.
		out[i] = scratch[pos:len(scratch):len(scratch)]
	}
	return out, nil
}

// cloneFields copies a RowDescription's descriptors so they outlive pgproto3's receive
// buffer (the Name slices point into it).
func cloneFields(in []pgproto3.FieldDescription) []pgproto3.FieldDescription {
	out := make([]pgproto3.FieldDescription, len(in))
	for i, f := range in {
		out[i] = f
		out[i].Name = append([]byte(nil), f.Name...)
	}
	return out
}

// --- the driver-owned rows ----------------------------------------------------------

// pinnedRows streams the private sequence's result group. It satisfies dao.Rows,
// dao.RowsColumns and dao.RawRows; RawValues are pgproto3's receive buffers, valid only
// until the next Next or Close (the /0017 borrowed-buffer rule).
type pinnedRows struct {
	seq     *privateSequence
	ctx     context.Context
	release func()

	err    error
	closed bool
}

// Next advances to the next row. It returns false at the end of the group or on error;
// a server error is reported by Err after the group ends.
func (r *pinnedRows) Next() bool {
	if r.closed || r.err != nil || r.seq.groupDone {
		return false
	}
	vals, err := r.seq.next(r.ctx)
	if err != nil {
		r.err = err
		return false
	}
	return vals != nil
}

// Scan decodes the current row's text-format values into dest via the connection's type
// map, matching the pool path's Scan semantics for the common Go destinations.
func (r *pinnedRows) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	vals := r.seq.values
	if len(dest) != len(vals) {
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: Scan: %d destinations for %d columns", len(dest), len(vals))
	}
	for i, d := range dest {
		fd := r.seq.fields[i]
		if err := r.seq.p.typeMap.Scan(fd.DataTypeOID, fd.Format, vals[i], d); err != nil {
			return fmt.Errorf("postgres: Scan column %d (%s): %w", i, fd.Name, err)
		}
	}
	return nil
}

// Err reports a transport failure, or the translated server error once the group has
// ended in one.
func (r *pinnedRows) Err() error {
	if r.err != nil {
		return r.err
	}
	if r.seq.groupDone && r.seq.srvErr != nil {
		return translateError(r.seq.srvErr)
	}
	return nil
}

// Close drains any unread rows, completes the cleanup contract (the group's terminal
// ReadyForQuery, then the tracked Close+Sync exchange), releases the private wire, and
// returns Err. It is idempotent.
func (r *pinnedRows) Close() error {
	if r.closed {
		return r.Err()
	}
	r.closed = true
	defer r.release()
	if r.err != nil {
		// Transport failure: the handle is poisoned; no wire cleanup is promised.
		return r.err
	}
	if err := r.seq.finish(r.ctx); err != nil {
		if r.seq.srvErr == nil || !errors.Is(err, translateError(r.seq.srvErr)) {
			r.err = err
		}
		return err
	}
	return nil
}

// Columns reports the result set's column names, satisfying dao.RowsColumns.
func (r *pinnedRows) Columns() ([]string, error) {
	out := make([]string, len(r.seq.fields))
	for i, f := range r.seq.fields {
		out[i] = string(f.Name)
	}
	return out, nil
}

// RawValues returns the current row's values exactly as received — BORROWED, valid only
// until the next Next or Close — satisfying dao.RawRows.
func (r *pinnedRows) RawValues() [][]byte { return r.seq.values }

// Fields returns the result set's column descriptors as the server sent them,
// satisfying dao.RawRows.
func (r *pinnedRows) Fields() []dao.FieldDescription {
	out := make([]dao.FieldDescription, len(r.seq.fields))
	for i, fd := range r.seq.fields {
		out[i] = dao.FieldDescription{
			Name:         string(fd.Name),
			TableOID:     fd.TableOID,
			ColumnAttr:   fd.TableAttributeNumber,
			TypeOID:      fd.DataTypeOID,
			TypeSize:     fd.DataTypeSize,
			TypeModifier: fd.TypeModifier,
			Format:       fd.Format,
		}
	}
	return out
}
