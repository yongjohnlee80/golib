package sqlite

import (
	"github.com/yongjohnlee80/golib/dao"
)

// ADR-0017 capabilities for the SQLite driver: none, deliberately.
//
// modernc/sqlite is a single-writer engine whose transaction semantics are set
// by the BEGIN keyword itself (DEFERRED/IMMEDIATE/EXCLUSIVE), not by the
// standard access/isolation/deferrable options, and database/sql's TxOptions
// cannot reach them. Implementing dao.TxBeginner here would mean accepting an
// option set and quietly ignoring most of it. Instead sqliteConn stays a plain
// dao.DataConn, and dao.BeginConnTx refuses any non-default option up front
// with dao.ErrTxOptionUnsupported naming this driver (ADR-0017 §2.2a).
//
// dao.ContextTxConn is likewise not implemented: *sql.Tx has no context
// finalizers, and modernc/sqlite finalizes on context.Background.
//
// Compile-time proof that the base contracts are unchanged (ADR-0017
// criterion 1) — and, by their absence here, that no capability is claimed.
var (
	_ dao.DataConn = (*sqliteConn)(nil)
	_ dao.TxConn   = (*sqliteTx)(nil)
)
