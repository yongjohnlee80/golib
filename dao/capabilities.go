package dao

import (
	"context"
	"strings"
)

// Optional Dialect capabilities.
//
// A [Dialect] declares only what every database engine must answer. Anything
// an engine may or may not be able to do is one of the interfaces below, and
// an engine declares it by IMPLEMENTING it. Support is therefore a fact about
// the type, settled when the driver is written and checked with a type
// assertion:
//
//	if c, ok := d.(Copier); ok {
//	    n, err := c.Copy(ctx, ex, table, cols, rows)
//	}
//
// There is deliberately no SupportsCopy() bool to go with it. A predicate and
// an interface can disagree; a type either implements the interface or it does
// not. Adding a capability this way also costs nothing to the engines that
// will never have it — they are not asked.
//
// A dialect that does not implement a capability keeps the documented
// behaviour it had before the capability existed: an error naming
// [ErrUnsupported], or the fallback the call site documents. Never a panic and
// never a silent no-op.

// Upserter renders the conflict clause appended to an INSERT, letting an
// engine express "insert, or update on conflict" in its own syntax.
//
// Engines differ here in more than spelling: PostgreSQL and SQLite use
// ON CONFLICT, MySQL uses ON DUPLICATE KEY UPDATE. An engine with no upsert
// at all does not implement this.
type Upserter interface {
	BuildUpsertSuffix(conflictCols, updateCols []string) string
}

// Copier is a bulk-load fast path that bypasses ordinary INSERT batching —
// PostgreSQL's COPY, and nothing else at present.
type Copier interface {
	Copy(ctx context.Context, ex any, table string, cols []string, rows [][]any) (int64, error)
}

// TwoPhaser is prepared-transaction support: the engine can PREPARE a
// transaction on one connection and COMMIT or ROLLBACK it later, possibly from
// another. gid is the caller's global transaction identifier.
//
// The three methods are one capability, not three. An engine that can prepare
// but cannot commit what it prepared is not usable, so they are declared and
// implemented together.
type TwoPhaser interface {
	Prepare(ctx context.Context, tx TxConn, gid string) error
	CommitPrepared(ctx context.Context, conn DataConn, gid string) error
	RollbackPrepared(ctx context.Context, conn DataConn, gid string) error
}

// Returner means the engine can hand back the generated id from the INSERT
// itself, via a RETURNING clause, instead of requiring a second round trip.
//
// quotedIDCol arrives ALREADY QUOTED by the dialect's own QuoteIdent, so an
// implementation appends it verbatim and never re-quotes.
type Returner interface {
	ReturningClause(quotedIDCol string) string
}

// LastInsertIDReader means a plain INSERT's result carries the generated id,
// the way MySQL's LAST_INSERT_ID() does. It is the fallback for engines that
// cannot RETURNING.
//
// An engine implementing NEITHER this nor [Returner] performs the insert and
// reports a zero id with a nil error — the documented no-generated-id insert,
// for append-only stores whose ids are supplied by the caller.
type LastInsertIDReader interface {
	LastInsertID(res Result) (int64, error)
}

// StandardReturningClause is the RETURNING clause every engine that supports
// RETURNING renders today.
//
// It exists so that the several dialects implementing [Returner] identically
// share one body rather than copying three lines each. An engine whose syntax
// differs writes its own method instead of calling this.
func StandardReturningClause(quotedIDCol string) string {
	if quotedIDCol == "" {
		return ""
	}
	return " RETURNING " + quotedIDCol
}

// StandardUpsertSuffix renders the SQL-standard ON CONFLICT clause shared by
// PostgreSQL and SQLite, so both implement [Upserter] from one body.
//
// It takes the QUOTER rather than quoting internally, and that is deliberate.
// The generic implementation this replaces quoted with its own receiver's
// QuoteIdent, so a dialect that embedded it and overrode QuoteIdent would have
// had its conflict clause quoted by the wrong rules — invisible today only
// because the two engines that inherited it also inherit QuoteIdent. Passing
// the caller keeps the quoting and the dialect together.
func StandardUpsertSuffix(q interface{ QuoteIdent(string) string }, conflictCols, updateCols []string) string {
	if len(conflictCols) == 0 {
		return "ON CONFLICT DO NOTHING"
	}
	var sb strings.Builder
	sb.WriteString("ON CONFLICT (")
	for i, c := range conflictCols {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(q.QuoteIdent(c))
	}
	sb.WriteString(") DO ")
	if len(updateCols) == 0 {
		sb.WriteString("NOTHING")
		return sb.String()
	}
	sb.WriteString("UPDATE SET ")
	for i, c := range updateCols {
		if i > 0 {
			sb.WriteString(", ")
		}
		qc := q.QuoteIdent(c)
		sb.WriteString(qc)
		sb.WriteString(" = EXCLUDED.")
		sb.WriteString(qc)
	}
	return sb.String()
}

// ResultLastInsertID reads the generated id from a plain INSERT's result. It
// is the one body behind every [LastInsertIDReader] implementation.
func ResultLastInsertID(res Result) (int64, error) {
	if res == nil {
		return 0, nil
	}
	return res.LastInsertId()
}

// Capability discovery.
//
// These are FUNCTIONS, not methods, and that distinction is the whole design.
// A method named SupportsUpsert would make every dialect answer a question
// about a feature most of them have nothing to do with — and, being a second
// statement of the same fact, it could disagree with the implementation. A
// function asks the type system instead: it cannot be wrong, because there is
// nothing to keep in sync.
//
// Use them where a call site reads better for it. A direct type assertion is
// equally correct and is what these do:
//
//	if dao.SupportsUpsert(conn.Dialect()) { ... }
//	if u, ok := conn.Dialect().(dao.Upserter); ok { ... }
//
// Prefer the assertion when you also need the interface, and the helper when
// you only need the answer.

// SupportsUpsert reports whether d can render a conflict clause — insert, or
// update on conflict.
func SupportsUpsert(d Dialect) bool {
	_, ok := d.(Upserter)
	return ok
}

// SupportsCopy reports whether d has a bulk-load fast path that bypasses
// ordinary INSERT batching.
func SupportsCopy(d Dialect) bool {
	_, ok := d.(Copier)
	return ok
}

// SupportsTwoPhase reports whether d can prepare a transaction on one
// connection and commit or roll it back later, possibly from another.
func SupportsTwoPhase(d Dialect) bool {
	_, ok := d.(TwoPhaser)
	return ok
}

// SupportsReturning reports whether d can hand back a generated id from the
// INSERT itself, rather than needing a second round trip.
func SupportsReturning(d Dialect) bool {
	_, ok := d.(Returner)
	return ok
}

// SupportsLastInsertID reports whether a plain INSERT's result carries the
// generated id on d.
func SupportsLastInsertID(d Dialect) bool {
	_, ok := d.(LastInsertIDReader)
	return ok
}

// StringQuoter is an optional [Dialect] capability: a dialect that can render a
// Go string as a literal for its own engine implements it, and [Str] then
// quotes through it instead of refusing the characters it cannot escape
// portably.
//
// It is a CAPABILITY and not a [Dialect] method for the same reason TableQuoter
// is, only with more at stake. A promoted [GenericDialect] default would apply
// silently to every dialect that did not write its own — and a wrong default
// here is not a mis-quoted table name, it is an injection. A dialect that
// cannot state its own rule must keep refusing rather than inherit a guess.
//
// QuoteString RETURNS AN ERROR because refusal is a legitimate answer. Some
// inputs have no representation as a literal in some engines — a NUL byte, or a
// backslash where the escaping mode is a session setting rather than a property
// of the dialect — and a quoter that cannot represent its input must say so
// instead of producing SQL that means something else.
//
// The returned string INCLUDES its delimiters, because which delimiters are
// correct is part of what the dialect knows.
type StringQuoter interface {
	QuoteString(s string) (string, error)
}

// SupportsStringQuoting reports whether d can render a Go string as a literal
// for its engine, returning the capability if so.
//
// A caller that gets false must not fall back to quoting the string itself:
// the whole point of the capability is that only the dialect knows the rule.
func SupportsStringQuoting(d Dialect) (StringQuoter, bool) {
	q, ok := d.(StringQuoter)
	return q, ok
}
