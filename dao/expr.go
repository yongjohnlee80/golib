package dao

import (
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/errs"
)

// Expr is a SQL expression that can only be rendered once a dialect is known,
// plus the raw column identity the write path needs.
//
// A declaration is written long before any [DataConn] exists — a package-level
// map[Field]dao.Field[R] is initialized at init time — so an expression that
// must quote identifiers cannot be a string. It is a value carrying a renderer,
// and [New] resolves it exactly once against the connection's dialect.
//
// Construct one only through the helpers in this file: the fields are
// unexported precisely so an Expr cannot be assembled with a write identity
// that disagrees with what it renders.
type Expr struct {
	render func(Dialect) string

	// write is the RAW, unquoted column name INSERT/UPDATE need. Only T and C
	// set it; every composition leaves it empty, because an expression has no
	// write identity and inventing one would be worse than having none.
	write string
}

// isSet reports whether e was built by a helper (a zero Expr has no renderer).
func (e Expr) isSet() bool { return e.render != nil }

// mustSet panics on a zero Expr, at declaration time rather than at New or, far
// worse, at statement time with a nil renderer.
func (e Expr) mustSet(who string) Expr {
	if !e.isSet() {
		panic(errs.Fatal{Op: "dao." + who, Rule: "zero Expr (build it with dao.T, dao.C, dao.Int or dao.SQL)"})
	}
	return e
}

// T qualifies a column with its table: T(TableArtist, ArtistName) renders
// "artist"."name" on Postgres and `artist`.`name` on MySQL. It is generic over
// ~string so the field enum and table constants a schema already declares pass
// without conversion.
//
// The table part is rendered in TABLE position (through the same helper the
// builder uses), so a schema-qualified constant like "app.users" splits into
// two quoted parts on a dialect implementing [TableQuoter] and falls back to
// whole-string quoting otherwise — the documented behavior.
//
// The write identity is the raw column: INSERT/UPDATE see "name" and quote it
// exactly once, byte-identical to a hand-written Column of "artist.name".
func T[Tbl ~string, Col ~string](table Tbl, col Col) Expr {
	tbl, c := string(table), string(col)
	return Expr{
		render: func(d Dialect) string { return quoteTable(d, tbl) + "." + d.QuoteIdent(c) },
		write:  c,
	}
}

// C is an unqualified column: C(MetaKVAction) renders "action" / `action`.
// Single-table entities with no joins need no qualification but still benefit
// from quoting. Like [T], it carries the raw column as its write identity.
func C[Col ~string](col Col) Expr {
	c := string(col)
	return Expr{
		render: func(d Dialect) string { return d.QuoteIdent(c) },
		write:  c,
	}
}

// Int is an integer literal, rendered in decimal. Floats are deliberately
// absent: their text form is precision-sensitive and non-finite values have no
// portable spelling. NULL is absent because COALESCE(x, NULL) is a no-op.
func Int(i int64) Expr {
	lit := strconv.FormatInt(i, 10)
	return Expr{render: func(Dialect) string { return lit }}
}

// SQL is the escape hatch: text is emitted verbatim, unquoted and unresolved,
// for expressions the helpers do not cover (NOW(), a window function, a
// dialect-specific cast). It carries no write identity, so a writable field
// using one must declare ReadOnly or an explicit WriteColumn.
//
// Named SQL because [Raw] is already the predicate-position escape hatch.
func SQL(text string) Expr {
	return Expr{render: func(Dialect) string { return text }}
}

// Coalesce renders COALESCE(e, alt). Both sides are [Expr], so both are
// rendered by the connection's dialect and neither is text this function had to
// quote.
//
// THERE IS NO STRING OVERLOAD, deliberately. An earlier version accepted a bare
// Go string (and an int) through a type-constrained fallback, which read well —
// Coalesce(T(t, c), "n/a") — but it meant this package had to turn a string into
// a SQL literal, and there is no portable way to do that: MySQL's escaping
// depends on NO_BACKSLASH_ESCAPES and the connection charset, both session
// state, while a declaration is written before any connection exists. The old
// helper resolved that by PANICKING on anything needing an escape, which made it
// correct only for the strings a caller could safely have written by hand.
//
// So a literal fallback is spelled with the constructor that fits it: [Int] for
// a number, which has one portable spelling and cannot render anything but
// digits, or [SQL] for anything else, where the text is visibly the author's.
//
//	Coalesce(T(t, c), Int(0))
//	Coalesce(T(t, c), SQL("''"))
//
// Values that are DATA rather than declaration belong in a predicate, where they
// travel as bind parameters and the driver escapes them — see [Eq] and friends.
// Nothing on this path needs to be inline.
//
// A Coalesce carries no write identity: a writable field declared with one must
// set ReadOnly or WriteColumn.
func Coalesce(e, alt Expr) Expr {
	e.mustSet("Coalesce")
	alt.mustSet("Coalesce")
	return Expr{render: func(d Dialect) string {
		return "COALESCE(" + e.render(d) + ", " + alt.render(d) + ")"
	}}
}

// LeftJoin renders a LEFT JOIN clause for [OptionalJoinExpr]:
//
//	LeftJoin(TableLabelGroup,
//	    T(TableLabelGroup, LabelGroupID),
//	    T(TableArtist, ArtistLabelGroupID))
//
// becomes LEFT JOIN "label_group" ON "label_group"."id" = "artist"."label_group_id".
// The table is rendered in table position, so a schema-qualified constant
// splits on a [TableQuoter] dialect.
func LeftJoin[Tbl ~string](table Tbl, left, right Expr) Expr {
	return joinClauseExpr("LEFT JOIN", string(table), left, right)
}

// InnerJoin renders an INNER JOIN clause; see [LeftJoin].
func InnerJoin[Tbl ~string](table Tbl, left, right Expr) Expr {
	return joinClauseExpr("INNER JOIN", string(table), left, right)
}

// joinClauseExpr builds "<kind> <table> ON <left> = <right>".
func joinClauseExpr(kind, table string, left, right Expr) Expr {
	left.mustSet(strings.ReplaceAll(kind, " ", ""))
	right.mustSet(strings.ReplaceAll(kind, " ", ""))
	return Expr{render: func(d Dialect) string {
		var b strings.Builder
		b.WriteString(kind)
		b.WriteByte(' ')
		b.WriteString(quoteTable(d, table))
		b.WriteString(" ON ")
		b.WriteString(left.render(d))
		b.WriteString(" = ")
		b.WriteString(right.render(d))
		return b.String()
	}}
}

// plainIdent reports whether s is usable as a bare write column: no whitespace,
// parentheses, commas or quotes.
func plainIdent(s string) bool {
	if s == "" {
		return false
	}
	return !strings.ContainsAny(s, " \t\n\r(),'\"`")
}
