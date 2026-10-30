package dao

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// In-process cells for WithLocalSettings: the exact order
// of hooks and statements for every verb, the refusals that send nothing, and
// the restore's bound arguments and failures. The live cells, against
// PostgreSQL 18 with pgvector, are dao/postgres/localsettings_integration_test.go.

// lsDialect allows the names "a" and "b"; the value "bad" is out of domain.
type lsDialect struct{ returningDialect }

func (lsDialect) CheckLocalSetting(s LocalSetting) error {
	if s.Name != "a" && s.Name != "b" {
		return errs.Wrap(errs.ErrInvalidArgument, "lsDialect: %q is not allowed", s.Name)
	}
	if s.Value == "bad" {
		return errs.Wrap(errs.ErrInvalidArgument, "lsDialect: %s = bad", s.Name)
	}
	return nil
}

func (lsDialect) LocalSettingsSQL(ss []LocalSetting) (string, []any) {
	var args []any
	for _, s := range ss {
		args = append(args, s.Name, s.Value)
	}
	return "SETTINGS", args
}

func (lsDialect) LocalRestoreSQL(ps []PriorSetting) (string, []any) {
	var args []any
	for _, p := range ps {
		var v any
		if p.Value != nil {
			v = *p.Value
		}
		args = append(args, p.Name, v)
	}
	return "RESTORE", args
}

// lsConn records every BEGIN, statement and hook call into one log, in the
// order they happen, so a cell can assert the order the server would see.
type lsConn struct {
	d       Dialect
	log     []string
	prior   []any            // the settings statement's row: priors, then the set_config columns
	fail    map[string]error // statement kind → its error
	restore []any            // the restore's arguments, as sent
}

func kindOf(q string) string {
	for _, k := range []string{"SETTINGS", "RESTORE", "SELECT COUNT", "SELECT EXISTS", "SELECT", "INSERT", "UPDATE", "DELETE"} {
		if strings.HasPrefix(q, k) {
			return k
		}
	}
	return q
}

func (c *lsConn) rowsFor(k string) *fakeRows {
	switch k {
	case "SETTINGS":
		return &fakeRows{data: [][]any{c.prior}}
	case "SELECT COUNT":
		return &fakeRows{data: [][]any{{uint64(2)}}}
	case "SELECT EXISTS":
		return &fakeRows{data: [][]any{{true}}}
	case "INSERT":
		return &fakeRows{data: [][]any{{"id-1"}}}
	}
	return &fakeRows{data: [][]any{{"1", "one", "u1"}, {"2", "two", "u2"}}}
}

func (c *lsConn) query(where, q string, args []any) (Rows, error) {
	k := kindOf(q)
	c.log = append(c.log, where+" "+k)
	if err := c.fail[k]; err != nil {
		return nil, err
	}
	return c.rowsFor(k), nil
}

func (c *lsConn) exec(where, q string, args []any) (Result, error) {
	k := kindOf(q)
	c.log = append(c.log, where+" "+k)
	if k == "RESTORE" {
		c.restore = args
	}
	if err := c.fail[k]; err != nil {
		return nil, err
	}
	return fakeResult{}, nil
}

func (c *lsConn) QueryContext(_ context.Context, q string, args ...any) (Rows, error) {
	return c.query("pool", q, args)
}
func (c *lsConn) ExecContext(_ context.Context, q string, args ...any) (Result, error) {
	return c.exec("pool", q, args)
}
func (c *lsConn) Dialect() Dialect { return c.d }
func (c *lsConn) Begin(context.Context) (TxConn, error) {
	c.log = append(c.log, "BEGIN")
	return lsTx{c}, nil
}
func (c *lsConn) Name() string { return "ls" }
func (c *lsConn) Close() error { return nil }

type lsTx struct{ c *lsConn }

func (t lsTx) QueryContext(_ context.Context, q string, args ...any) (Rows, error) {
	return t.c.query("tx", q, args)
}
func (t lsTx) ExecContext(_ context.Context, q string, args ...any) (Result, error) {
	return t.c.exec("tx", q, args)
}
func (t lsTx) Commit() error   { return nil }
func (t lsTx) Rollback() error { return nil }

// lsHook logs BeforeExec and AfterExec with their ops into the connection's log;
// failBefore fails that op's BeforeExec.
type lsHook struct {
	NopHook
	c          *lsConn
	failBefore Op
}

var errHookRefused = errors.New("lsHook: refused")

func (h lsHook) BeforeExec(_ context.Context, q *QueryInfo) error {
	h.c.log = append(h.c.log, "before "+string(q.Op))
	if q.Op == h.failBefore {
		return errHookRefused
	}
	return nil
}

func (h lsHook) AfterExec(_ context.Context, q *QueryInfo, out Outcome) error {
	h.c.log = append(h.c.log, "after "+string(q.Op))
	return out.Err
}

func strp(s string) *string { return &s }

func newLS() *lsConn {
	return &lsConn{d: lsDialect{}, prior: []any{strp("a0"), nil, strp("a1"), strp("b1")}}
}

// lsVerbs runs each verb once on d, answering its error (Iterate: consumed to
// the end, or closed after one row when early is set).
func lsVerbs(early bool) map[Op]func(DAO[*artist, artistField, string]) error {
	return map[Op]func(DAO[*artist, artistField, string]) error{
		OpGet:    func(d DAO[*artist, artistField, string]) error { _, err := d.With(aID, "1").Get(); return err },
		OpSelect: func(d DAO[*artist, artistField, string]) error { _, err := d.Select(); return err },
		OpCount:  func(d DAO[*artist, artistField, string]) error { _, err := d.Count(); return err },
		OpExists: func(d DAO[*artist, artistField, string]) error { _, err := d.Exists(); return err },
		OpInsert: func(d DAO[*artist, artistField, string]) error { _, err := d.Set(aName, "x").Insert(); return err },
		OpUpdate: func(d DAO[*artist, artistField, string]) error { return d.With(aID, "1").Set(aName, "x").Update() },
		OpUpsert: func(d DAO[*artist, artistField, string]) error { return d.Set(aURI, "u").Set(aName, "x").Upsert() },
		OpDelete: func(d DAO[*artist, artistField, string]) error { return d.With(aID, "1").Delete() },
		OpIterate: func(d DAO[*artist, artistField, string]) error {
			it, err := d.Iterate()
			if err != nil {
				return err
			}
			for it.Next() {
				if early {
					return it.Close()
				}
			}
			if err := it.Err(); err != nil {
				return err
			}
			return it.Close()
		},
	}
}

// verbStatement is the kind of statement op sends.
func verbStatement(op Op) string {
	switch op {
	case OpCount:
		return "SELECT COUNT"
	case OpExists:
		return "SELECT EXISTS"
	case OpInsert, OpUpsert:
		return "INSERT"
	case OpUpdate:
		return "UPDATE"
	case OpDelete:
		return "DELETE"
	}
	return "SELECT"
}

// Cell 9, every verb: with the option, BEGIN, the settings statement inside its
// own hook pair, the verb's statement inside its pair, then the restore inside
// its pair: the order the statements reach the server. Iterate's restore comes
// at its rows' close, at the end or early. Without the option, today's order:
// the verb's BeforeExec, BEGIN, its statement, its AfterExec, nothing more.
func TestLocalSettings_HooksSeeEveryStatementInOrder(t *testing.T) {
	for _, early := range []bool{false, true} {
		for op, run := range lsVerbs(early) {
			if early && op != OpIterate {
				continue
			}
			t.Run(fmt.Sprintf("%s/early=%v", op, early), func(t *testing.T) {
				c := newLS()
				s := buildSchema(c)
				tx := Begin(context.Background())
				if err := run(s.On(tx, WithHooks(lsHook{c: c}), WithLocalSettings(LocalSetting{"a", "1"}, LocalSetting{"b", "2"}))); err != nil {
					t.Fatalf("verb: %v", err)
				}
				stmt := "tx " + verbStatement(op)
				want := []string{"BEGIN",
					"before local-settings", "tx SETTINGS", "after local-settings",
					"before " + string(op), stmt, "after " + string(op),
					"before local-restore", "tx RESTORE", "after local-restore"}
				if !slices.Equal(c.log, want) {
					t.Fatalf("with the option:\n got  %q\n want %q", c.log, want)
				}

				c2 := newLS()
				tx2 := Begin(context.Background())
				if err := run(buildSchema(c2).On(tx2, WithHooks(lsHook{c: c2}))); err != nil {
					t.Fatalf("verb without the option: %v", err)
				}
				want2 := []string{"before " + string(op), "BEGIN", stmt, "after " + string(op)}
				if !slices.Equal(c2.log, want2) {
					t.Fatalf("without the option:\n got  %q\n want %q", c2.log, want2)
				}
			})
		}
	}
}

// Cell 6, in-process: the restore puts back exactly the values the settings
// statement read, bound as arguments, a name with none as NULL.
func TestLocalSettings_TheRestoreBindsThePriorValues(t *testing.T) {
	c := newLS()
	tx := Begin(context.Background())
	if _, err := buildSchema(c).On(tx, WithLocalSettings(LocalSetting{"a", "1"}, LocalSetting{"b", "2"})).Count(); err != nil {
		t.Fatalf("Count: %v", err)
	}
	if want := []any{"a", "a0", "b", nil}; !slices.Equal(c.restore, want) {
		t.Fatalf("restore args = %v, want %v", c.restore, want)
	}
}

// Cell 5, in-process: each refusal answers its sentinel and nothing is sent,
// not even BEGIN.
func TestLocalSettings_RefusalsSendNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		dia  Dialect
		dao  func(s *Schema[*artist, artistField, artistSort, string], opt QueryOption) DAO[*artist, artistField, string]
		set  []LocalSetting
		want error
	}{
		{"unknown name", lsDialect{}, onTx, []LocalSetting{{"role", "x"}}, errs.ErrInvalidArgument},
		{"value out of domain", lsDialect{}, onTx, []LocalSetting{{"a", "bad"}}, errs.ErrInvalidArgument},
		{"empty name", lsDialect{}, onTx, []LocalSetting{{"", "1"}}, errs.ErrInvalidArgument},
		{"a name twice", lsDialect{}, onTx, []LocalSetting{{"a", "1"}, {"a", "2"}}, errs.ErrInvalidArgument},
		{"a dialect without LocalSetter", returningDialect{}, onTx, []LocalSetting{{"a", "1"}}, ErrUnsupported},
		{"a pool DAO", lsDialect{}, func(s *Schema[*artist, artistField, artistSort, string], o QueryOption) DAO[*artist, artistField, string] {
			return s.DAO(o)
		}, []LocalSetting{{"a", "1"}}, ErrLocalSettingsNeedTx},
		{"On(nil)", lsDialect{}, func(s *Schema[*artist, artistField, artistSort, string], o QueryOption) DAO[*artist, artistField, string] {
			return s.On(nil, o)
		}, []LocalSetting{{"a", "1"}}, ErrLocalSettingsNeedTx},
		{"Use(nil)", lsDialect{}, func(s *Schema[*artist, artistField, artistSort, string], o QueryOption) DAO[*artist, artistField, string] {
			return onTx(s, o).Use(nil)
		}, []LocalSetting{{"a", "1"}}, ErrLocalSettingsNeedTx},
		{"OnCtx without a transaction", lsDialect{}, func(s *Schema[*artist, artistField, artistSort, string], o QueryOption) DAO[*artist, artistField, string] {
			return s.OnCtx(context.Background(), o)
		}, []LocalSetting{{"a", "1"}}, ErrLocalSettingsNeedTx},
	} {
		for op, run := range lsVerbs(false) {
			t.Run(tc.name+"/"+string(op), func(t *testing.T) {
				c := newLS()
				c.d = tc.dia
				s := buildSchema(c)
				err := run(tc.dao(s, WithLocalSettings(tc.set...)))
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
				if len(c.log) != 0 {
					t.Fatalf("sent %q, want nothing", c.log)
				}
			})
		}
	}
	if !errors.Is(ErrLocalSettingsNeedTx, errs.ErrPrecondition) {
		t.Fatal("ErrLocalSettingsNeedTx does not wrap errs.ErrPrecondition")
	}
}

func onTx(s *Schema[*artist, artistField, artistSort, string], o QueryOption) DAO[*artist, artistField, string] {
	return s.On(Begin(context.Background()), o)
}

// D3: Batch refuses the option with ErrUnsupported, sending nothing.
func TestLocalSettings_BatchRefuses(t *testing.T) {
	c := newLS()
	b := buildSchema(c).On(Begin(context.Background()), WithLocalSettings(LocalSetting{"a", "1"})).Batch()
	b.Add(map[artistField]any{aName: "x", aURI: "u"})
	if err := b.Flush(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Flush = %v, want ErrUnsupported", err)
	}
	if len(c.log) != 0 {
		t.Fatalf("sent %q, want nothing", c.log)
	}
}

// After a statement that fails, nothing more is sent: the transaction is
// aborted, which ends the settings.
func TestLocalSettings_AFailedStatementSendsNoRestore(t *testing.T) {
	for op, run := range lsVerbs(false) {
		t.Run(string(op), func(t *testing.T) {
			c := newLS()
			stmtErr := errors.New("server: statement failed")
			c.fail = map[string]error{verbStatement(op): stmtErr}
			err := run(buildSchema(c).On(Begin(context.Background()), WithLocalSettings(LocalSetting{"a", "1"})))
			if !errors.Is(err, stmtErr) {
				t.Fatalf("err = %v, want the statement's error", err)
			}
			if slices.Contains(c.log, "tx RESTORE") {
				t.Fatalf("sent a restore after a failed statement: %q", c.log)
			}
		})
	}
}

// Cell 13, in-process: a restore that fails fails the verb with that error,
// errors.Is-able, and the caller gets none of the result.
func TestLocalSettings_AFailedRestoreFailsTheVerb(t *testing.T) {
	restoreErr := errors.New("server: restore failed")
	for op, run := range lsVerbs(false) {
		t.Run(string(op)+"/server", func(t *testing.T) {
			c := newLS()
			c.fail = map[string]error{"RESTORE": restoreErr}
			err := run(buildSchema(c).On(Begin(context.Background()), WithLocalSettings(LocalSetting{"a", "1"})))
			if !errors.Is(err, restoreErr) {
				t.Fatalf("err = %v, want the restore's error in its chain", err)
			}
		})
		t.Run(string(op)+"/hook", func(t *testing.T) {
			c := newLS()
			err := run(buildSchema(c).On(Begin(context.Background()),
				WithHooks(lsHook{c: c, failBefore: OpLocalRestore}), WithLocalSettings(LocalSetting{"a", "1"})))
			if !errors.Is(err, errHookRefused) {
				t.Fatalf("err = %v, want the hook's refusal", err)
			}
		})
	}
	c := newLS()
	c.fail = map[string]error{"RESTORE": restoreErr}
	got, err := buildSchema(c).On(Begin(context.Background()), WithLocalSettings(LocalSetting{"a", "1"})).Select()
	if err == nil || got != nil {
		t.Fatalf("Select = %v, %v; want no rows and the restore's error", got, err)
	}
	n, err := buildSchema(c).On(Begin(context.Background()), WithLocalSettings(LocalSetting{"a", "1"})).Count()
	if err == nil || n != 0 {
		t.Fatalf("Count = %d, %v; want 0 and the restore's error", n, err)
	}
}

// A settings statement that fails fails the verb, and neither the statement
// nor a restore is sent.
func TestLocalSettings_AFailedSettingsStatementSendsNothingMore(t *testing.T) {
	c := newLS()
	setErr := errors.New("server: settings failed")
	c.fail = map[string]error{"SETTINGS": setErr}
	_, err := buildSchema(c).On(Begin(context.Background()), WithLocalSettings(LocalSetting{"a", "1"})).Select()
	if !errors.Is(err, setErr) {
		t.Fatalf("err = %v, want the settings statement's error", err)
	}
	if want := []string{"BEGIN", "tx SETTINGS"}; !slices.Equal(c.log, want) {
		t.Fatalf("sent %q, want %q", c.log, want)
	}
}

// A verb whose BeforeExec refuses after the settings were made puts them back:
// the statement is never sent, and the transaction is not aborted.
func TestLocalSettings_ARefusedStatementIsRestored(t *testing.T) {
	c := newLS()
	_, err := buildSchema(c).On(Begin(context.Background()),
		WithHooks(lsHook{c: c, failBefore: OpSelect}), WithLocalSettings(LocalSetting{"a", "1"})).Select()
	if !errors.Is(err, errHookRefused) {
		t.Fatalf("err = %v, want the hook's refusal", err)
	}
	want := []string{"BEGIN", "before local-settings", "tx SETTINGS", "after local-settings",
		"before select", "before local-restore", "tx RESTORE", "after local-restore"}
	if !slices.Equal(c.log, want) {
		t.Fatalf("got  %q\nwant %q", c.log, want)
	}
}
