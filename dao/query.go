package dao

import (
	"fmt"
	"strings"
)

// Predicate is a renderable WHERE fragment. Implementations are provided for the
// common cases (see the constructors below); callers can supply their own for
// bespoke SQL.
//
// ToSQL renders the fragment using d for placeholder/dialect rendering. next is a
// shared 1-based placeholder counter: an implementation pre-increments it for
// each bind argument it emits, so fragments compose with continuous numbering.
type Predicate interface {
	ToSQL(d Dialect, next *int) (sql string, args []any)
}

// Eq renders "col = ?".
func Eq(col string, v any) Predicate { return &eq{col, v} }

type eq struct {
	col string
	v   any
}

func (p *eq) ToSQL(d Dialect, next *int) (string, []any) {
	*next++
	return p.col + " = " + d.Placeholder(*next), []any{p.v}
}

// In renders "col IN (...)". An empty value set renders "1 = 0" (matches nothing).
func In(col string, vs []any) Predicate { return &in{col, vs} }

type in struct {
	col string
	vs  []any
}

func (p *in) ToSQL(d Dialect, next *int) (string, []any) {
	if len(p.vs) == 0 {
		return "1 = 0", nil
	}
	return listPredicate(d, next, p.col, " IN (", p.vs)
}

// NotIn renders "col NOT IN (...)". An empty value set renders "1 = 1" (matches all).
func NotIn(col string, vs []any) Predicate { return &notIn{col, vs} }

type notIn struct {
	col string
	vs  []any
}

func (p *notIn) ToSQL(d Dialect, next *int) (string, []any) {
	if len(p.vs) == 0 {
		return "1 = 1", nil
	}
	return listPredicate(d, next, p.col, " NOT IN (", p.vs)
}

// listPredicate renders "<col><open><ph>, <ph>...)" and collects the args.
func listPredicate(d Dialect, next *int, col, open string, vs []any) (string, []any) {
	var sb strings.Builder
	sb.WriteString(col)
	sb.WriteString(open)
	args := make([]any, 0, len(vs))
	for i, v := range vs {
		if i > 0 {
			sb.WriteString(", ")
		}
		*next++
		sb.WriteString(d.Placeholder(*next))
		args = append(args, v)
	}
	sb.WriteByte(')')
	return sb.String(), args
}

// IsNull renders "col IS NULL".
func IsNull(col string) Predicate { return &nullCheck{col, false} }

// IsNotNull renders "col IS NOT NULL".
//
// Note: ADR-0002 names this predicate NotNull; it is IsNotNull here to avoid
// colliding with the NotNull [ConstraintKind] constant (ADR-0004). The pairing
// with IsNull also reads better.
func IsNotNull(col string) Predicate { return &nullCheck{col, true} }

type nullCheck struct {
	col string
	not bool
}

func (p *nullCheck) ToSQL(_ Dialect, _ *int) (string, []any) {
	if p.not {
		return p.col + " IS NOT NULL", nil
	}
	return p.col + " IS NULL", nil
}

// Gt renders "col > ?".
func Gt(col string, v any) Predicate { return &cmp{col, ">", v} }

// Gte renders "col >= ?".
func Gte(col string, v any) Predicate { return &cmp{col, ">=", v} }

// Lt renders "col < ?".
func Lt(col string, v any) Predicate { return &cmp{col, "<", v} }

// Lte renders "col <= ?".
func Lte(col string, v any) Predicate { return &cmp{col, "<=", v} }

type cmp struct {
	col, op string
	v       any
}

func (p *cmp) ToSQL(d Dialect, next *int) (string, []any) {
	*next++
	return p.col + " " + p.op + " " + d.Placeholder(*next), []any{p.v}
}

// Between renders "col BETWEEN ? AND ?".
func Between(col string, lo, hi any) Predicate { return &between{col, lo, hi} }

type between struct {
	col    string
	lo, hi any
}

func (p *between) ToSQL(d Dialect, next *int) (string, []any) {
	*next++
	lo := d.Placeholder(*next)
	*next++
	hi := d.Placeholder(*next)
	return p.col + " BETWEEN " + lo + " AND " + hi, []any{p.lo, p.hi}
}

// Like renders "col LIKE ?". The pattern is bound, never interpolated, so it
// cannot inject SQL — but it IS a raw LIKE pattern: % and _ keep their wildcard
// meaning. When embedding user input in a pattern, escape it with EscapeLike
// first (StringOp search does this automatically).
func Like(col string, pattern string) Predicate { return &like{col, pattern} }

// EscapeLike escapes the LIKE/ILIKE metacharacters (%, _ and the escape
// character itself) in s so it matches literally inside a pattern. Predicates
// built by this package pair the result with an explicit "ESCAPE '\'" clause,
// which works across the shipped dialects regardless of their default.
func EscapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

type like struct {
	col, pattern string
}

func (p *like) ToSQL(d Dialect, next *int) (string, []any) {
	*next++
	return p.col + " LIKE " + d.Placeholder(*next), []any{p.pattern}
}

// Raw is an escape hatch for bespoke SQL. Write each bind parameter as a "?"; the
// placeholders are renumbered to the dialect's style as the fragment is composed.
// (A literal "?" inside a string literal would also be rewritten — avoid that.)
func Raw(sql string, args ...any) Predicate { return &raw{sql, args} }

type raw struct {
	sql  string
	args []any
}

func (p *raw) ToSQL(d Dialect, next *int) (string, []any) {
	var sb strings.Builder
	for i := 0; i < len(p.sql); i++ {
		if p.sql[i] == '?' {
			*next++
			sb.WriteString(d.Placeholder(*next))
			continue
		}
		sb.WriteByte(p.sql[i])
	}
	return sb.String(), append([]any(nil), p.args...)
}

// And groups predicates with AND. With no members it renders "1 = 1" (true).
func And(ps ...Predicate) Predicate { return &group{or: false, ps: ps} }

// Or groups predicates with OR. With no members it renders "1 = 0" (false).
func Or(ps ...Predicate) Predicate { return &group{or: true, ps: ps} }

type group struct {
	or bool
	ps []Predicate
}

func (g *group) ToSQL(d Dialect, next *int) (string, []any) {
	if len(g.ps) == 0 {
		if g.or {
			return "1 = 0", nil
		}
		return "1 = 1", nil
	}
	op := " AND "
	if g.or {
		op = " OR "
	}
	parts := make([]string, 0, len(g.ps))
	var args []any
	for _, p := range g.ps {
		s, a := p.ToSQL(d, next)
		parts = append(parts, s)
		args = append(args, a...)
	}
	if len(parts) == 1 {
		return parts[0], args
	}
	return "(" + strings.Join(parts, op) + ")", args
}

// Sort is one ORDER BY term, identified by a sort key (a K-enum value) plus
// direction. The schema (ADR-0006) maps the key to one or more SQL expressions.
type Sort struct {
	Key  string
	Desc bool
}

// Asc returns an ascending Sort for the given sort-enum key.
func Asc(key any) Sort { return Sort{Key: fmt.Sprint(key)} }

// Desc returns a descending Sort for the given sort-enum key.
func Desc(key any) Sort { return Sort{Key: fmt.Sprint(key), Desc: true} }

// ParseSorts decodes HTTP-style sort specs into Sorts: a leading "-" means
// descending, an optional leading "+" (or no prefix) means ascending. Empty
// specs are skipped. Example: ParseSorts("-created", "name").
func ParseSorts(specs ...string) []Sort {
	out := make([]Sort, 0, len(specs))
	for _, s := range specs {
		if s == "" {
			continue
		}
		switch s[0] {
		case '-':
			out = append(out, Desc(s[1:]))
		case '+':
			out = append(out, Asc(s[1:]))
		default:
			out = append(out, Asc(s))
		}
	}
	return out
}

// SearchOp maps a search-query token to a predicate factory. The entity declares
// its operators via dao.Search(...) (ADR-0006); DAO.Search(query) parses the
// query and applies each matching operator.
type SearchOp interface {
	Token() string
	Predicate(value string) Predicate
}

// StringOp matches token:value with a case-insensitive substring match, rendered
// portably as LOWER(col) LIKE LOWER('%value%') so it works on Postgres, SQLite,
// and MySQL alike (ILIKE is Postgres-only). field is the field-enum key; until a
// schema binds it to a column (ADR-0006), the field key is used as the column.
func StringOp(token string, field any) SearchOp {
	return &stringOp{token: token, field: fmt.Sprint(field)}
}

// ExactOp matches token:value with equality (col = value). field is the
// field-enum key, bound to a column by the schema (ADR-0006).
func ExactOp(token string, field any) SearchOp {
	return &exactOp{token: token, field: fmt.Sprint(field)}
}

// BoolOp matches token:value as a boolean comparison (col = true/false). "true"
// (any case) and "1" are true; everything else is false.
func BoolOp(token string, col string) SearchOp { return &boolOp{token, col} }

// ArrayOp matches token:value as membership (value = ANY(col)).
func ArrayOp(token string, col string) SearchOp { return &arrayOp{token, col} }

// RawOp matches token:value with a caller-supplied predicate factory (e.g. an
// EXISTS subquery).
func RawOp(token string, fn func(value string) Predicate) SearchOp {
	return &rawOp{token, fn}
}

// fieldSearchOp is implemented by search ops declared against a field enum. The
// schema (ADR-0006) resolves the field to a column and calls withColumn at build
// time; until then column() falls back to the field key.
type fieldSearchOp interface {
	SearchOp
	fieldKey() string
	withColumn(col string) SearchOp
}

type stringOp struct {
	token, field, col string
}

func (o *stringOp) Token() string    { return o.token }
func (o *stringOp) fieldKey() string { return o.field }
func (o *stringOp) column() string {
	if o.col != "" {
		return o.col
	}
	return o.field
}
func (o *stringOp) withColumn(col string) SearchOp {
	c := *o
	c.col = col
	return &c
}
func (o *stringOp) Predicate(value string) Predicate {
	// Portable case-insensitive substring match: ILIKE is Postgres-only, so
	// LOWER+LIKE (Postgres, SQLite, MySQL). The user-supplied value is a
	// literal, not a pattern: escape LIKE metacharacters so searching "50%"
	// matches "50%" rather than "50…".
	return Raw("LOWER("+o.column()+`) LIKE LOWER(?) ESCAPE '\'`, "%"+EscapeLike(value)+"%")
}

type exactOp struct {
	token, field, col string
}

func (o *exactOp) Token() string    { return o.token }
func (o *exactOp) fieldKey() string { return o.field }
func (o *exactOp) column() string {
	if o.col != "" {
		return o.col
	}
	return o.field
}
func (o *exactOp) withColumn(col string) SearchOp {
	c := *o
	c.col = col
	return &c
}
func (o *exactOp) Predicate(value string) Predicate { return Eq(o.column(), value) }

type boolOp struct {
	token, col string
}

func (o *boolOp) Token() string { return o.token }
func (o *boolOp) Predicate(value string) Predicate {
	return Eq(o.col, strings.EqualFold(value, "true") || value == "1")
}

type arrayOp struct {
	token, col string
}

func (o *arrayOp) Token() string                    { return o.token }
func (o *arrayOp) Predicate(value string) Predicate { return Raw("? = ANY("+o.col+")", value) }

type rawOp struct {
	token string
	fn    func(string) Predicate
}

func (o *rawOp) Token() string                    { return o.token }
func (o *rawOp) Predicate(value string) Predicate { return o.fn(value) }

// searchTerm is one parsed "token:value" pair.
type searchTerm struct {
	token, value string
}

// parseSearchQuery splits a "token:value token2:value2" query into terms.
// Whitespace-separated parts without a ':' are ignored. The value is everything
// after the first ':'.
func parseSearchQuery(query string) []searchTerm {
	fields := strings.Fields(query)
	terms := make([]searchTerm, 0, len(fields))
	for _, f := range fields {
		i := strings.IndexByte(f, ':')
		if i <= 0 {
			continue
		}
		terms = append(terms, searchTerm{token: f[:i], value: f[i+1:]})
	}
	return terms
}
