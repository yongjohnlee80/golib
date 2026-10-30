package dao

import (
	"context"
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/errs"
)

// LocalSetting is one transaction-local setting for [WithLocalSettings]: a name
// and its value as text. A dialect's helpers make them (postgres.PlanCacheMode,
// postgres.HNSWEfSearch …); the dialect checks each against its allowlist, so a
// hand-built LocalSetting goes through the same check as a helper's.
type LocalSetting struct{ Name, Value string }

// PriorSetting is a name's value as it was before a statement's settings were
// made: Value is nil when the name had none (a placeholder the server has not
// defined yet). [LocalSetter.LocalRestoreSQL] puts it back.
type PriorSetting struct {
	Name  string
	Value *string
}

// LocalSetter is the capability of a dialect that can set settings for one
// statement on that statement's own transaction and connection. PostgreSQL
// implements it; a dialect that does not refuses [WithLocalSettings] with
// [ErrUnsupported].
type LocalSetter interface {
	// CheckLocalSetting refuses a name not on the dialect's allowlist, or a
	// value outside the name's domain, with an error wrapping
	// errs.ErrInvalidArgument that names the setting.
	CheckLocalSetting(LocalSetting) error
	// LocalSettingsSQL is the one statement that reads each name's value in
	// force, then sets each for the transaction, with its arguments. Names and
	// values are bound, never spliced into the text. It answers one row of
	// 2·len(settings) columns: each name's previous value, in order, NULL for a
	// name with none, then one column per setting made, which dao reads and
	// ignores.
	LocalSettingsSQL(settings []LocalSetting) (string, []any)
	// LocalRestoreSQL is the one statement that puts each previous value back
	// for the transaction, names and values bound; a nil value resets the
	// name to the value it would have with no setting made in the transaction.
	LocalRestoreSQL(previous []PriorSetting) (string, []any)
	// AbortsTransaction reports whether err, a statement's error as the driver
	// gave it, was reported by the server, which then aborts the transaction
	// (or rolls back the savepoint) and so ends the settings itself: dao sends
	// no restore after it. Any other failure (a scan, decode or type error on
	// the client, an error before anything was sent) leaves the transaction
	// alive, and the restore is sent.
	AbortsTransaction(err error) bool
}

// ErrLocalSettingsNeedTx is a verb's answer for a DAO acquired with
// [WithLocalSettings] that is not on a transaction (a pool DAO, On(nil), or
// after Use(nil)). A pool statement runs in a transaction of its own, which no
// setting sent before it could reach, so the verb refuses before sending
// anything instead of running without its settings. It wraps
// errs.ErrPrecondition.
var ErrLocalSettingsNeedTx = fmt.Errorf("dao: WithLocalSettings needs the DAO on a transaction (%w)", errs.ErrPrecondition)

// WithLocalSettings sets each setting for each of the DAO's statements only:
// on the statement's own transaction and connection, immediately before it, and
// puts each name's previous value back once the statement is done (for
// [DAO.Iterate], when its rows close). A later statement of the transaction,
// this DAO's or any other's, sees the transaction's own values, never these.
//
// It costs two round trips per statement: one that reads the previous values
// and makes the settings, one that puts the values back. A DAO acquired
// without it sends neither.
//
// The DAO must be on a transaction (On(tx), OnCtx with a transaction, or
// Use(tx)); without one every verb answers [ErrLocalSettingsNeedTx]. The
// dialect must implement [LocalSetter], or the DAO answers [ErrUnsupported].
// A setting off the dialect's allowlist, a value outside its domain, an empty
// name or a name given twice fail the DAO with an error wrapping
// errs.ErrInvalidArgument. All three refusals come before anything is sent.
// [DAO.Batch] refuses the option with ErrUnsupported.
//
// The restore runs after every outcome but an error the server reported
// ([LocalSetter.AbortsTransaction]); a client-side failure is restored. Should
// the restore not run (the server failed the statement, the restore failed,
// the context ended), the settings still end with the transaction: commit,
// rollback, or a rolled-back savepoint. No setting of this option outlives the
// transaction, so none reaches the next user of a pooled connection.
func WithLocalSettings(settings ...LocalSetting) QueryOption {
	return func(c *queryConfig) { c.local = append(c.local, settings...) }
}

// checkLocalSettings is the acquisition-time check of WithLocalSettings'
// settings against the dialect: a duplicate or empty name, then the dialect's
// capability, then its allowlist.
func checkLocalSettings(d Dialect, settings []LocalSetting) error {
	if len(settings) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(settings))
	for _, s := range settings {
		if s.Name == "" {
			return errs.Wrap(errs.ErrInvalidArgument, "dao: a local setting has no name")
		}
		if seen[s.Name] {
			return errs.Wrap(errs.ErrInvalidArgument, "dao: local setting %q is given twice", s.Name)
		}
		seen[s.Name] = true
	}
	ls, ok := d.(LocalSetter)
	if !ok {
		return fmt.Errorf("%w: local settings (dialect %s)", ErrUnsupported, d.Name())
	}
	for _, s := range settings {
		if err := ls.CheckLocalSetting(s); err != nil {
			return err
		}
	}
	return nil
}

// exec resolves the executor for one built statement and fires its
// BeforeExec. Without local settings it keeps the order every DAO has always
// had (BeforeExec, then the executor) and answers a nil restore. With them the
// order is the order statements reach the server: the transaction's executor,
// the settings statement, then the statement's BeforeExec; the caller runs the
// answered restore once its statement is done.
func (d *queryDAO[R, C, K, ID]) exec(pl *pipeline, q *string, args *[]any) (execQuerier, *localRestore, error) {
	if len(d.local) == 0 {
		if err := pl.beforeExec(q, args); err != nil {
			return nil, nil, err
		}
		h, err := d.handle()
		return h, nil, err
	}
	if d.tx == nil {
		return nil, nil, ErrLocalSettingsNeedTx
	}
	h, err := d.handle()
	if err != nil {
		return nil, nil, err
	}
	lr, err := d.setLocal(h)
	if err != nil {
		return nil, nil, err
	}
	if err := pl.beforeExec(q, args); err != nil {
		// The settings are in force and the statement will not be sent: put
		// them back now.
		return nil, nil, lr.after(nil, err)
	}
	return h, lr, nil
}

// setLocal sends the settings statement on h, with its own BeforeExec and
// AfterExec (OpLocalSettings), and answers the restore for the values it read.
func (d *queryDAO[R, C, K, ID]) setLocal(h execQuerier) (*localRestore, error) {
	ls := d.schema.dialect.(LocalSetter) // checkLocalSettings, at acquisition
	q, args := ls.LocalSettingsSQL(d.local)
	pl, err := d.begin(OpLocalSettings, false)
	if err != nil {
		return nil, err
	}
	if err := pl.beforeExec(&q, &args); err != nil {
		return nil, err
	}
	rows, err := h.QueryContext(d.ctx(), q, args...)
	if err != nil {
		return nil, pl.finish(0, -1, d.schema.translate(err))
	}
	prev := make([]*string, 2*len(d.local))
	dest := make([]any, len(prev))
	for i := range prev {
		dest[i] = &prev[i]
	}
	if serr := scanOne(rows, dest...); serr != nil {
		return nil, pl.finish(0, -1, d.schema.translate(serr))
	}
	if ferr := pl.finish(1, -1, nil); ferr != nil {
		return nil, ferr
	}
	lr := &localRestore{d: localDAO{begin: d.begin, ctx: d.ctx, translate: d.schema.translate},
		setter: ls, h: h, prior: make([]PriorSetting, len(d.local))}
	for i, s := range d.local {
		lr.prior[i] = PriorSetting{Name: s.Name, Value: prev[i]}
	}
	return lr, nil
}

// scanOne scans the first row of rows into dest, closing rows; like
// scanScalar, for a row of several columns.
func scanOne(rows Rows, dest ...any) error {
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return ErrNoRows
	}
	if err := rows.Scan(dest...); err != nil {
		return err
	}
	return rows.Err()
}

// localDAO is what a restore needs of its DAO, without the DAO's type
// parameters, so an iterator can carry it.
type localDAO struct {
	begin     func(Op, bool) (*pipeline, error)
	ctx       func() context.Context
	translate func(error) error
}

// localRestore is one optioned statement's restore: the values its settings
// statement read, put back on the connection the statement ran on.
type localRestore struct {
	d      localDAO
	setter LocalSetter
	h      execQuerier
	prior  []PriorSetting
	ended  bool
}

// after ends an optioned statement, its rows (if any) already closed. stmtErr
// is the statement's own error as the driver gave it (nil when it succeeded,
// ErrNoRows included), and err the verb's answer so far. Unless the server
// reported stmtErr ([LocalSetter.AbortsTransaction]), the restore is sent, with
// its own BeforeExec and AfterExec (OpLocalRestore), and its error is joined to
// err. After a server-reported error nothing is sent: the server has aborted
// the transaction, or rolled back the savepoint, which ends the settings. On a
// nil restore (a DAO without the option) it answers err unchanged. It runs
// once; a second call answers err.
func (l *localRestore) after(stmtErr, err error) error {
	if l == nil || l.ended {
		return err
	}
	l.ended = true
	if stmtErr != nil && l.setter.AbortsTransaction(stmtErr) {
		return err
	}
	if rerr := l.restore(); rerr != nil {
		return errors.Join(err, rerr)
	}
	return err
}

// done is after for a statement with rows: it closes them first, since the
// restore runs on their connection. On a nil restore it neither closes the
// rows (the verb's own Close does) nor sends anything.
func (l *localRestore) done(rows Rows, stmtErr, err error) error {
	if l == nil {
		return err
	}
	rows.Close()
	return l.after(stmtErr, err)
}

func (l *localRestore) restore() error {
	q, args := l.setter.LocalRestoreSQL(l.prior)
	pl, err := l.d.begin(OpLocalRestore, false)
	if err != nil {
		return err
	}
	if err := pl.beforeExec(&q, &args); err != nil {
		return err
	}
	_, err = l.h.ExecContext(l.d.ctx(), q, args...)
	if err != nil {
		err = fmt.Errorf("dao: restoring local settings: %w", l.d.translate(err))
	}
	return pl.finish(0, -1, err)
}
