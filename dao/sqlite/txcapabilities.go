package sqlite

import (
	"github.com/yongjohnlee80/golib/dao"
)

// This driver claims NO optional transaction capabilities, deliberately.
//
// modernc/sqlite is a single-writer engine whose transaction semantics are set
// by the BEGIN keyword itself (DEFERRED/IMMEDIATE/EXCLUSIVE), not by the
// standard access/isolation/deferrable options, and database/sql's TxOptions
// cannot reach them. Implementing dao.TxBeginner here would mean accepting an
// option set and quietly ignoring most of it. Instead sqliteConn stays a plain
// dao.DataConn, and dao.BeginConnTx refuses any non-default option up front
// with dao.ErrTxOptionUnsupported naming this driver, so a caller learns the
// option was refused instead of assuming it was honoured.
//
// dao.ContextTxConn is likewise not implemented: *sql.Tx has no context
// finalizers, and modernc/sqlite finalizes on context.Background.
//
// These two lines are a compile-time statement about what this driver does
// and does not implement. They prove the base contracts still hold; the
// capability interfaces are ABSENT on purpose, and that absence is the claim.
// Adding one here would make sqliteConn advertise a capability it cannot
// honour, and nothing at runtime would catch it.
// REFERENCE: dao/sqlite/txcapabilities_test.go
var (
	_ dao.DataConn = (*sqliteConn)(nil)
	_ dao.TxConn   = (*sqliteTx)(nil)
)
