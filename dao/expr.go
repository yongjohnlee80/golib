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
		panic(errs.Fatal{Op: "dao." + who, Rule: "zero Expr (build it with dao.T, dao.C, dao.Str, dao.Int or dao.SQL)"})
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
// whole-string quoting otherwise — the documented ADR-0013 behavior.
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

// Str is a string literal. It refuses anything it cannot render identically on
// every supported dialect: a string containing a single quote, a backslash or a
// control character panics at declaration.
//
// This is deliberate. MySQL's escaping depends on NO_BACKSLASH_ESCAPES and the
// connection charset, so a portable contract cannot be "escape correctly" — it
// can only be "accept what needs no escaping". That covers every literal a
// declaration has needed in practice (the empty string, simple defaults).
// Anything richer belongs in [SQL], where the author owns the text.
func Str(s string) Expr {
	return Expr{render: func(d Dialect) string {
		// The dialect quotes when it can say how. Only it knows whether a
		// backslash is an escape, what a NUL byte does, and which delimiters
		// are correct — so this asks rather than guesses.
		if q, ok := SupportsStringQuoting(d); ok {
			lit, err := q.QuoteString(s)
			if err != nil {
				panic(errs.Fatal{Op: "dao.Str",
					Rule:   "the dialect cannot represent this string as a literal",
					Detail: err.Error()})
			}
			return lit
		}
		// No capability: refuse exactly what has no portable inline escaping,
		// rather than inherit a guess. This is the pre-capability behaviour and
		// stays the answer for a dialect that has not stated its rule.
		for i := 0; i < len(s); i++ {
			switch b := s[i]; {
			case b == '\'':
				panic(errs.Fatal{Op: "dao.Str",
					Rule:   "string contains a single quote and " + d.Name() + " has not stated its quoting rule",
					Detail: "use dao.SQL, or give the dialect a QuoteString"})
			case b == '\\':
				panic(errs.Fatal{Op: "dao.Str",
					Rule:   "string contains a backslash and " + d.Name() + " has not stated its quoting rule",
					Detail: "use dao.SQL, or give the dialect a QuoteString"})
			case b < 0x20 || b == 0x7f:
				panic(errs.Fatal{Op: "dao.Str",
					Rule:   "string contains a control character and " + d.Name() + " has not stated its quoting rule",
					Detail: "use dao.SQL, or give the dialect a QuoteString"})
			}
		}
		return "'" + s + "'"
	}}
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

// Alt is what [Coalesce] accepts as its fallback: an [Expr], or a string or
// integer literal. Anything else is a COMPILE error, not a runtime panic.
//
// The terms are deliberately NOT ~string / ~int. A tilde term would admit a
// NAMED type — a field enum is ~string — which satisfies the constraint but
// does not match `case string` in the routing below; the literal would never be
// built and the zero Expr's nil renderer would fail at statement time.
// Excluding tildes closes that hole and additionally makes
// Coalesce(T(t, c), ArtistName) — a column enum where a VALUE belongs — stop
// compiling.
type Alt interface {
	Expr | string | int | int64
}

// lit routes an [Alt] to an Expr through the closed literal set.
//
// INVARIANT: the terms of Alt and the cases here are kept in lockstep. A term
// with no case yields a zero Expr, i.e. a nil renderer — expr_test.go asserts a
// non-nil renderer for every term so a future widening fails a test instead of
// a caller.
func lit[A Alt](alt A) Expr {
	switch v := any(alt).(type) {
	case Expr:
		return v.mustSet("Coalesce")
	case string:
		return Str(v)
	case int:
		return Int(int64(v))
	case int64:
		return Int(v)
	}
	panic("dao: unreachable — an Alt term has no lit case")
}

// Coalesce renders COALESCE(e, alt). alt is used directly when it is an Expr;
// otherwise it is a literal routed through the same closed set and the same
// refusal rules, so Coalesce(T(t, c), "") reads as intended and cannot produce
// a literal this package would not render identically everywhere.
//
// The result carries no write identity: a COALESCE has no column to write to.
func Coalesce[A Alt](e Expr, alt A) Expr {
	e.mustSet("Coalesce")
	a := lit(alt)
	return Expr{render: func(d Dialect) string {
		return "COALESCE(" + e.render(d) + ", " + a.render(d) + ")"
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
