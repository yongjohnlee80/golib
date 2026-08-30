package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

// ADR-0017 §2.2a, criterion 2 (mysql row). No server needed: the whole point of
// the row is which options are refused, and every refusal happens before
// database/sql is reached — proven here by a connection whose *sql.DB is nil,
// which would panic if the BEGIN were attempted.

// What MySQL CAN express, and exactly how it renders.
func TestMysqlTxOptions_Honored(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts dao.TxOptions
		want sql.TxOptions
	}{
		{"default", dao.TxOptions{}, sql.TxOptions{Isolation: sql.LevelDefault, ReadOnly: false}},
		{"read only", dao.TxOptions{Access: dao.TxReadOnly}, sql.TxOptions{Isolation: sql.LevelDefault, ReadOnly: true}},

		// The full isolation domain, READ UNCOMMITTED included — MySQL is the
		// driver that implements it literally, which is why rev 3 put it in the
		// domain at all.
		{"read uncommitted", dao.TxOptions{Isolation: dao.TxReadUncommitted}, sql.TxOptions{Isolation: sql.LevelReadUncommitted}},
		{"read committed", dao.TxOptions{Isolation: dao.TxReadCommitted}, sql.TxOptions{Isolation: sql.LevelReadCommitted}},
		{"repeatable read", dao.TxOptions{Isolation: dao.TxRepeatableRead}, sql.TxOptions{Isolation: sql.LevelRepeatableRead}},
		{"serializable", dao.TxOptions{Isolation: dao.TxSerializable}, sql.TxOptions{Isolation: sql.LevelSerializable}},

		{
			"serializable read only",
			dao.TxOptions{Isolation: dao.TxSerializable, Access: dao.TxReadOnly},
			sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := mysqlTxOptions(tt.opts)
			if err != nil {
				t.Fatalf("mysqlTxOptions(%+v) = %v, want no error", tt.opts, err)
			}
			if *got != tt.want {
				t.Errorf("mysqlTxOptions(%+v) = %+v, want %+v", tt.opts, *got, tt.want)
			}
		})
	}
}

// What MySQL CANNOT express is refused, not quietly downgraded.
//
// Explicit READ WRITE is the interesting one: sql.TxOptions carries a bool, and
// ReadOnly=false renders a plain START TRANSACTION — a request for the server
// default, not an override of it. Accepting it would hand back a transaction
// that silently differs from the one asked for on a server whose default is
// read-only.
func TestMysqlTxOptions_RefusedBeforeBegin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opts   dao.TxOptions
		option string
	}{
		{"explicit read write", dao.TxOptions{Access: dao.TxReadWrite}, "Access=read write"},
		{
			"read write with isolation",
			dao.TxOptions{Access: dao.TxReadWrite, Isolation: dao.TxSerializable},
			"Access=read write",
		},
		{
			"deferrable",
			dao.TxOptions{Isolation: dao.TxSerializable, Access: dao.TxReadOnly, Deferrable: dao.TxDeferrable},
			"Deferrable=deferrable",
		},
		{
			"not deferrable",
			dao.TxOptions{Isolation: dao.TxSerializable, Access: dao.TxReadOnly, Deferrable: dao.TxNotDeferrable},
			"Deferrable=not deferrable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// A nil *sql.DB: if the refusal did not happen before the BEGIN,
			// this call would panic instead of returning.
			c := &mysqlConn{name: "mysql"}
			_, err := c.BeginTx(context.Background(), tt.opts)

			if !errors.Is(err, dao.ErrUnsupported) {
				t.Fatalf("err = %v, want a dao.ErrUnsupported match", err)
			}
			var unsup *dao.ErrTxOptionUnsupported
			if !errors.As(err, &unsup) {
				t.Fatalf("err = %v, want *dao.ErrTxOptionUnsupported", err)
			}
			if unsup.Driver != "mysql" {
				t.Errorf("Driver = %q, want %q", unsup.Driver, "mysql")
			}
			if unsup.Option != tt.option {
				t.Errorf("Option = %q, want %q", unsup.Option, tt.option)
			}
		})
	}
}

// Validation order: a malformed option set is malformed input, even when the
// driver would also have refused something in it.
func TestMysqlTxOptions_InvalidBeforeUnsupported(t *testing.T) {
	t.Parallel()

	// Deferrable outside SERIALIZABLE READ ONLY is invalid by the driver-neutral
	// rule; MySQL would ALSO refuse Deferrable as unsupported. The caller must
	// be told the first thing, because it is the thing they can fix.
	c := &mysqlConn{name: "mysql"}
	_, err := c.BeginTx(context.Background(), dao.TxOptions{Deferrable: dao.TxDeferrable})

	var invalid *dao.ErrTxOptionInvalid
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *dao.ErrTxOptionInvalid", err)
	}
	if errors.Is(err, dao.ErrUnsupported) {
		t.Error("the invalid-input error must not also read as a capability miss")
	}

	// And an out-of-range value on a field MySQL fully supports.
	if _, err := c.BeginTx(context.Background(), dao.TxOptions{Isolation: dao.TxIsolation(9)}); !errors.As(err, &invalid) {
		t.Errorf("err = %v, want *dao.ErrTxOptionInvalid", err)
	}
}

// MySQL claims TxBeginner and nothing else. The absence is as much a part of
// the contract as the presence: a consumer that needs bounded finalization must
// see this connection fail the probe rather than get a handle that quietly
// ignores its context.
func TestMysqlCapabilitySet(t *testing.T) {
	t.Parallel()

	var conn dao.DataConn = &mysqlConn{name: "mysql"}
	if _, ok := conn.(dao.TxBeginner); !ok {
		t.Error("mysqlConn must satisfy dao.TxBeginner")
	}
	if _, ok := conn.(dao.SessionTxBeginner); ok {
		t.Error("mysqlConn must NOT claim dao.SessionTxBeginner — *sql.Tx has no context finalizers")
	}
	var tx dao.TxConn = &mysqlTx{}
	if _, ok := tx.(dao.ContextTxConn); ok {
		t.Error("mysqlTx must NOT claim dao.ContextTxConn")
	}
	var rows dao.Rows = &sql.Rows{}
	if _, ok := dao.RawRowsOf(rows); ok {
		t.Error("*sql.Rows must NOT satisfy dao.RawRows")
	}
}
