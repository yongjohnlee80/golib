package deploy

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/dao/internal/txexec"
	"github.com/yongjohnlee80/golib/errs"
)

var (
	// ErrDowngrade is a database that records a script this binary lacks: an
	// older binary must not write a newer schema.
	ErrDowngrade = fmt.Errorf("%w: the database has a newer schema", errs.ErrPrecondition)
	// ErrNotLatest is a revert of a script that is not the latest applied.
	ErrNotLatest = fmt.Errorf("%w: only the latest applied script can be reverted", errs.ErrPrecondition)
)

// DefaultLedger is the ledger table a Runner keeps unless told otherwise.
const DefaultLedger = "schema_version"

// Runner applies one product's scripts.
type Runner struct {
	fsys   fs.FS
	ledger string
}

// Option configures a Runner.
type Option func(*Runner)

// Ledger names the ledger table (default [DefaultLedger]).
func Ledger(table string) Option { return func(r *Runner) { r.ledger = table } }

// New is a runner for the scripts in fsys.
func New(fsys fs.FS, opts ...Option) *Runner {
	r := &Runner{fsys: fsys, ledger: DefaultLedger}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Status is what a database has, and what Apply did or would do.
type Status struct {
	// Applied are the update scripts the ledger records, by name, before this call.
	Applied []string
	// Pending are the update scripts not yet applied, in the order they run.
	// After Apply they are the ones it applied.
	Pending []string
	// Warnings name each applied script whose digest differs from this binary's
	// copy. Such a script is not re-run; a released script must never change.
	Warnings []string
}

type applied struct {
	Script, SHA256 string
	AppliedAt      int64
}

type ledgerField string

const (
	lScript ledgerField = "script"
	lSHA    ledgerField = "sha256"
	lAt     ledgerField = "applied_at"
)

type ledgerSort string

const byScript ledgerSort = "script"

func (r *Runner) ledgerSchema(conn dao.DataConn) *dao.Schema[*applied, ledgerField, ledgerSort, string] {
	return dao.New[*applied, ledgerField, ledgerSort, string](conn,
		dao.Table[*applied, ledgerField, ledgerSort, string](r.ledger),
		dao.ID[*applied, ledgerField, ledgerSort, string](lScript),
		dao.Fields[*applied, ledgerField, ledgerSort, string](map[ledgerField]dao.Field[*applied]{
			lScript: {Expr: dao.C(lScript), Scan: func(a *applied) any { return &a.Script }},
			lSHA:    {Expr: dao.C(lSHA), Scan: func(a *applied) any { return &a.SHA256 }},
			lAt:     {Expr: dao.C(lAt), Scan: func(a *applied) any { return &a.AppliedAt }},
		}),
		dao.Default[*applied, ledgerField, ledgerSort, string](lScript, lSHA, lAt),
		dao.SortMap[*applied, ledgerField, ledgerSort, string](map[ledgerSort]string{byScript: "script"}))
}

// ledgerDDL creates the ledger: the same types on every engine the runner
// takes, and a primary key MySQL can index (VARCHAR, not TEXT).
func (r *Runner) ledgerDDL(d dao.Dialect) string {
	return "CREATE TABLE IF NOT EXISTS " + d.QuoteIdent(r.ledger) +
		" (script VARCHAR(255) PRIMARY KEY, sha256 CHAR(64) NOT NULL, applied_at BIGINT NOT NULL)"
}

// executor is the transaction's executor for conn, through dao's internal seam.
func executor(tx *dao.Transaction, conn dao.DataConn) (dao.TxConn, error) {
	v, err := txexec.Join(tx, conn)
	if err != nil {
		return nil, err
	}
	return v.(dao.TxConn), nil
}

// engine checks that conn's engine can apply scripts, and names it.
func engine(conn dao.DataConn) (string, error) {
	d := conn.Dialect()
	if t, ok := d.(interface{ TransactionalDDL() bool }); !ok || !t.TransactionalDDL() {
		return "", fmt.Errorf("%w: schema scripts on %s, which does not run DDL inside a transaction", dao.ErrUnsupported, d.Name())
	}
	return d.Name(), nil
}

// plan compares the ledger with the scripts: the downgrade guard, the digest
// warnings, and what is pending.
func plan(updates []Script, rows []*applied) (Status, []Script, error) {
	var st Status
	have := map[string]string{}
	for _, a := range rows {
		have[a.Script] = a.SHA256
		st.Applied = append(st.Applied, a.Script)
	}
	known := map[string]bool{}
	var pending []Script
	for _, s := range updates {
		known[s.Name] = true
		sum, done := have[s.Name]
		switch {
		case !done:
			pending = append(pending, s)
			st.Pending = append(st.Pending, s.Name)
		case sum != s.SHA256:
			st.Warnings = append(st.Warnings, fmt.Sprintf(
				"schema script %s was applied with digest %.12s but this binary's copy is %.12s: it is not re-run; a released script must never change",
				s.Name, sum, s.SHA256))
		}
	}
	for _, a := range rows {
		if !known[a.Script] {
			return st, nil, fmt.Errorf("%w: it has schema script %s, which this binary does not", ErrDowngrade, a.Script)
		}
	}
	return st, pending, nil
}

func updatesOf(all []Script) []Script {
	var out []Script
	for _, s := range all {
		if s.Kind == Update {
			out = append(out, s)
		}
	}
	return out
}

// Pending reports what Apply would do, and changes nothing: not even the
// ledger is created.
func (r *Runner) Pending(ctx context.Context, conn dao.DataConn) (Status, error) {
	eng, err := engine(conn)
	if err != nil {
		return Status{}, err
	}
	all, err := Load(r.fsys, eng)
	if err != nil {
		return Status{}, err
	}
	tables, err := dao.ListTables(ctx, conn, "")
	if err != nil {
		return Status{}, err
	}
	var rows []*applied
	for _, t := range tables {
		if t.Name == r.ledger {
			if rows, err = r.ledgerSchema(conn).OnCtx(ctx).OrderBy(dao.Asc(byScript)).Select(); err != nil {
				return Status{}, err
			}
			break
		}
	}
	st, _, err := plan(updatesOf(all), rows)
	return st, err
}

// Apply brings the database's schema up to date: every pending update script,
// in number order, and its ledger row, all in one transaction, so a failure
// anywhere leaves the database as it was. The returned Status's Pending are
// the scripts it applied. An engine that locks (Postgres) takes the lock first,
// so two processes applying at once take turns.
func (r *Runner) Apply(ctx context.Context, conn dao.DataConn) (Status, error) {
	eng, err := engine(conn)
	if err != nil {
		return Status{}, err
	}
	all, err := Load(r.fsys, eng)
	if err != nil {
		return Status{}, err
	}
	ledger := r.ledgerSchema(conn)
	var st Status
	err = dao.RunTx(ctx, func(tx *dao.Transaction) error {
		ex, err := executor(tx, conn)
		if err != nil {
			return err
		}
		if l, ok := conn.Dialect().(interface{ DeployLock() string }); ok {
			if _, err := ex.ExecContext(ctx, l.DeployLock()); err != nil {
				return fmt.Errorf("deploy: taking the lock: %w", err)
			}
		}
		if _, err := ex.ExecContext(ctx, r.ledgerDDL(conn.Dialect())); err != nil {
			return fmt.Errorf("deploy: creating the ledger %s: %w", r.ledger, err)
		}
		rows, err := ledger.On(tx).OrderBy(dao.Asc(byScript)).Select()
		if err != nil {
			return err
		}
		var pending []Script
		if st, pending, err = plan(updatesOf(all), rows); err != nil {
			return err
		}
		for _, s := range pending {
			if err := run(ctx, ex, s); err != nil {
				return err
			}
			if _, err := ledger.On(tx).Set(lScript, s.Name).Set(lSHA, s.SHA256).Set(lAt, time.Now().Unix()).Insert(); err != nil {
				return fmt.Errorf("deploy: recording %s: %w", s.Name, err)
			}
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	return st, nil
}

// run executes a script's statements, naming the script and the line a
// failure came from.
func run(ctx context.Context, ex dao.TxConn, s Script) error {
	stmts, err := s.Statements()
	if err != nil {
		return err
	}
	for _, st := range stmts {
		if _, err := ex.ExecContext(ctx, st.Text); err != nil {
			return fmt.Errorf("deploy: schema script %s, line %d: %w", s.Name, st.Line, err)
		}
	}
	return nil
}

// Revert undoes update script number n and removes it from the ledger, in one
// transaction. n must be the latest applied script; the baseline (1) has no
// revert. It returns the revert script's name.
func (r *Runner) Revert(ctx context.Context, conn dao.DataConn, n int) (string, error) {
	eng, err := engine(conn)
	if err != nil {
		return "", err
	}
	all, err := Load(r.fsys, eng)
	if err != nil {
		return "", err
	}
	var update, revert *Script
	for i := range all {
		if all[i].Number == n {
			if all[i].Kind == Update {
				update = &all[i]
			} else {
				revert = &all[i]
			}
		}
	}
	if update == nil {
		return "", errs.Wrap(errs.ErrInvalidArgument, "deploy: there is no update script numbered %06d", n)
	}
	if revert == nil {
		return "", errs.Wrap(errs.ErrInvalidArgument, "deploy: %s has no revert (the baseline never does)", update.Name)
	}
	ledger := r.ledgerSchema(conn)
	err = dao.RunTx(ctx, func(tx *dao.Transaction) error {
		ex, err := executor(tx, conn)
		if err != nil {
			return err
		}
		if l, ok := conn.Dialect().(interface{ DeployLock() string }); ok {
			if _, err := ex.ExecContext(ctx, l.DeployLock()); err != nil {
				return fmt.Errorf("deploy: taking the lock: %w", err)
			}
		}
		// a database that never applied a script has no ledger yet; creating it
		// here is rolled back with the refusal that follows
		if _, err := ex.ExecContext(ctx, r.ledgerDDL(conn.Dialect())); err != nil {
			return fmt.Errorf("deploy: creating the ledger %s: %w", r.ledger, err)
		}
		latest, err := ledger.On(tx).OrderBy(dao.Desc(byScript)).Get(lScript)
		if errors.Is(err, dao.ErrNoRows) {
			return fmt.Errorf("%w: nothing is applied", ErrNotLatest)
		}
		if err != nil {
			return err
		}
		if latest.Script != update.Name {
			return fmt.Errorf("%w: the latest is %s, not %s", ErrNotLatest, latest.Script, update.Name)
		}
		if err := run(ctx, ex, *revert); err != nil {
			return err
		}
		return ledger.On(tx).With(lScript, update.Name).Delete()
	})
	if err != nil {
		return "", err
	}
	return revert.Name, nil
}
