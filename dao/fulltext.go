package dao

import (
	"fmt"
	"math"
	"strings"

	"github.com/yongjohnlee80/golib/errs"
)

// FullTextIndex names an engine's full-text index over a content table. Its DDL
// and its upkeep (SQLite's triggers, a Postgres generated column) belong to the
// product's schema scripts; this names it for queries.
type FullTextIndex struct {
	// Name is the index: SQLite's FTS5 table.
	Name string
	// Table is the content table it indexes, and Key the content table's key
	// column the index is keyed by.
	Table, Key string
	// Columns are the index's columns, in its order. Rank's weights and
	// Snippet's column refer to them.
	Columns []string

	// Config is the text-search configuration PostgreSQL parses the document
	// and the query with; empty is "simple", which lowercases and does not
	// stem, the closest to SQLite's tokenizer. It is an identifier, optionally
	// schema-qualified ("english", "myschema.my_cfg"), checked before it can
	// reach SQL (see Validate). SQLite ignores it.
	Config string

	// Classes are the tsvector weight class ('A' to 'D') the content table's
	// migration gives each of Columns, in the same order: a ranking with
	// weights on PostgreSQL puts weight i on Classes[i]'s slot. SQLite ignores
	// them; its weights stay positional over Columns.
	Classes []byte
}

// Validate reports whether the index can reach SQL: Config, when set, is an
// identifier of lowercase letters, digits and underscores (each part at most
// 63 bytes, at most a schema and a name), and Classes, when set, give each
// column one class from 'A' to 'D', none twice. A consumer that only ever
// uses [Match], whose index is checked when a query runs, can call it at
// start-up to fail fast. The error matches [errs.ErrInvalidArgument].
func (ix FullTextIndex) Validate() error {
	if err := validConfig(ix.Config); err != nil {
		return err
	}
	if len(ix.Classes) == 0 {
		return nil
	}
	if len(ix.Classes) != len(ix.Columns) {
		return errs.Wrap(errs.ErrInvalidArgument, "dao: full-text index %q has %d classes for %d columns", ix.Name, len(ix.Classes), len(ix.Columns))
	}
	var seen [4]bool
	for _, c := range ix.Classes {
		if c < 'A' || c > 'D' {
			return errs.Wrap(errs.ErrInvalidArgument, "dao: full-text index %q: class %q is not one of A, B, C, D", ix.Name, c)
		}
		if seen[c-'A'] {
			return errs.Wrap(errs.ErrInvalidArgument, "dao: full-text index %q: class %q is given twice", ix.Name, c)
		}
		seen[c-'A'] = true
	}
	return nil
}

// validConfig is the identifier check for a text-search configuration: a
// value that passes is spelled only with characters no SQL quoting rule
// treats specially.
func validConfig(cfg string) error {
	if cfg == "" {
		return nil
	}
	parts := strings.Split(cfg, ".")
	if len(parts) > 2 {
		return errs.Wrap(errs.ErrInvalidArgument, "dao: text-search configuration %q has more than a schema and a name", cfg)
	}
	for _, p := range parts {
		if p == "" || len(p) > 63 || !(p[0] == '_' || p[0] >= 'a' && p[0] <= 'z') {
			return errs.Wrap(errs.ErrInvalidArgument, "dao: text-search configuration %q is not an identifier", cfg)
		}
		for i := 1; i < len(p); i++ {
			c := p[i]
			if !(c == '_' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
				return errs.Wrap(errs.ErrInvalidArgument, "dao: text-search configuration %q is not an identifier", cfg)
			}
		}
	}
	return nil
}

// mustValidIndex panics for an index a declaration cannot use: the check runs
// while New resolves the declaration, before any statement exists.
func mustValidIndex(ix FullTextIndex, who string) {
	if err := ix.Validate(); err != nil {
		panic(errs.Fatal{Op: "dao." + who, Rule: "the full-text index is not valid", Detail: err.Error()})
	}
}

// FullTextMatcher is an optional [Dialect] capability: the condition that a row
// matches the query bound at placeholder. Every engine with [FullTexter] has
// it; PostgreSQL has it without FullTexter, as its other pieces need the query
// bound again. [Match] needs it.
type FullTextMatcher interface {
	FullTextMatch(ix FullTextIndex, placeholder string) string
}

// FullTextQueryRanker is an optional [Dialect] capability: an ORDER BY
// expression ranking matches best first when ascending, against the query
// bound at placeholder, with one weight per column (by class, see
// [FullTextIndex].Classes). [RankQuery] uses it where an engine has it.
type FullTextQueryRanker interface {
	FullTextQueryRank(ix FullTextIndex, weights []float64, placeholder string) string
}

// FullTexter is an optional [Dialect] capability: the SQL of full-text search.
// It renders pieces of a query and runs nothing, so the query runs as any other
// DAO query, in whatever transaction the caller holds. Callers use [Match],
// [Rank], [Snippet] and [FullTextJoin] rather than calling it.
type FullTexter interface {
	// FullTextJoin joins the index to its content table.
	FullTextJoin(ix FullTextIndex) string
	// FullTextMatch is the condition that a row matches the query bound at
	// placeholder.
	FullTextMatch(ix FullTextIndex, placeholder string) string
	// FullTextRank is the ORDER BY expression ranking matches best first when
	// ascending, with one weight per column.
	FullTextRank(ix FullTextIndex, weights []float64) string
	// FullTextSnippet is an excerpt of column (an index into Columns) around the
	// matches, the matches between open and close, cut with ellipsis, about
	// tokens long. It returns an error for a marker the engine cannot quote.
	FullTextSnippet(ix FullTextIndex, column int, m SnippetMarks) (string, error)
}

// SupportsFullText reports whether d implements [FullTexter].
func SupportsFullText(d Dialect) bool {
	_, ok := d.(FullTexter)
	return ok
}

// SnippetMarks are how a [Snippet] marks its matches and its cuts.
type SnippetMarks struct {
	Open, Close, Ellipsis string
	// Tokens is about how long the excerpt is; SQLite takes 1 to 64.
	Tokens int
}

// needsFullText is a full-text piece's need of an engine.
func needsFullText(d Dialect) error {
	if !SupportsFullText(d) {
		return fmt.Errorf("%w: full-text search on %s", ErrUnsupported, d.Name())
	}
	return nil
}

// fullTexter is d's capability, or the panic a declaration gets for an engine
// without one: declaring full-text search on it is a mistake in the code. A
// query reaches it only after the expression's needs passed (see Cmp), so it
// panics only while New resolves a declaration.
func fullTexter(d Dialect, who string) FullTexter {
	ft, ok := d.(FullTexter)
	if !ok {
		panic(errs.Fatal{Op: "dao." + who, Rule: "the engine has no full-text search (check dao.SupportsFullText before declaring one)", Detail: d.Name()})
	}
	return ft
}

// FullTextJoin joins ix to its content table, for [OptionalJoinExpr]:
//
//	dao.OptionalJoinExpr[…](JoinFTS, dao.FullTextJoin(ChunkFTS))
//	// SQLite: INNER JOIN "chunk_fts" ON "chunk_fts"."rowid" = "chunk"."id"
func FullTextJoin(ix FullTextIndex) Expr {
	return Expr{render: func(d Dialect) string {
		mustValidIndex(ix, "FullTextJoin")
		return fullTexter(d, "FullTextJoin").FullTextJoin(ix)
	}, needs: needsIndex(ix, needsFullText)}
}

// needsIndex is a declaration's needs with the index's own check first.
func needsIndex(ix FullTextIndex, then func(Dialect) error) func(Dialect) error {
	return func(d Dialect) error {
		if err := ix.Validate(); err != nil {
			return err
		}
		return then(d)
	}
}

// Rank orders matches best first (ascending), weighting the columns: one weight
// per column of ix, or none for equal weights. For [SortExpr]:
//
//	dao.SortExpr[…](ByRank, dao.Rank(ChunkFTS, 10, 5, 5, 1))
//	// SQLite: bm25("chunk_fts", 10, 5, 5, 1)
//
// A weight count other than zero or len(Columns), or a weight that is not
// finite, panics at the call.
func Rank(ix FullTextIndex, weights ...float64) Expr {
	if len(weights) != 0 && len(weights) != len(ix.Columns) {
		panic(errs.Fatal{Op: "dao.Rank", Rule: "give one weight per column of the index, or none", Detail: fmt.Sprintf("%d weights, %d columns", len(weights), len(ix.Columns))})
	}
	for _, w := range weights {
		if math.IsNaN(w) || math.IsInf(w, 0) {
			panic(errs.Fatal{Op: "dao.Rank", Rule: "a weight must be finite", Detail: fmt.Sprint(w)})
		}
	}
	ws := append([]float64(nil), weights...)
	return Expr{render: func(d Dialect) string {
		mustValidIndex(ix, "Rank")
		return fullTexter(d, "Rank").FullTextRank(ix, ws)
	}, needs: needsIndex(ix, needsFullText)}
}

// RankQuery orders matches best first (ascending) against the query bound when
// the statement renders, for [SortParam]; a query orders by it with the same
// query text [Match] binds:
//
//	dao.SortParam[…](ByRank, dao.RankQuery(ChunkFTS, 1.0, 0.1))
//	….WithPredicate(dao.Match(ChunkFTS, q)).OrderBy(dao.AscBy(ByRank, q))
//	// PostgreSQL: -ts_rank_cd('{0.1, 0.2, 0.4, 1.0}', "chunk"."tsv", to_tsquery('simple'::regconfig, $2))
//	// SQLite:     bm25("chunk_fts", 1, 0.1), binding nothing
//
// On an engine with [FullTextQueryRanker] it binds the query; on one with only
// [FullTexter] (SQLite, whose rank reads the match it already has) it renders
// the plain rank and binds nothing, so one sort key works on both. Weights are
// one per column, or none for equal weights; with weights, PostgreSQL needs the
// index's Classes. A weight count other than zero or len(Columns), or a weight
// that is not finite, panics at the call; an invalid index panics at [New].
func RankQuery(ix FullTextIndex, weights ...float64) ParamExpr {
	if len(weights) != 0 && len(weights) != len(ix.Columns) {
		panic(errs.Fatal{Op: "dao.RankQuery", Rule: "give one weight per column of the index, or none", Detail: fmt.Sprintf("%d weights, %d columns", len(weights), len(ix.Columns))})
	}
	for _, w := range weights {
		if math.IsNaN(w) || math.IsInf(w, 0) {
			panic(errs.Fatal{Op: "dao.RankQuery", Rule: "a weight must be finite", Detail: fmt.Sprint(w)})
		}
	}
	ws := append([]float64(nil), weights...)
	return ParamExpr{
		arity: 1,
		render: func(d Dialect, ph func(any) string, args []any) string {
			if qr, ok := d.(FullTextQueryRanker); ok {
				return qr.FullTextQueryRank(ix, ws, ph(args[0]))
			}
			return d.(FullTexter).FullTextRank(ix, ws)
		},
		needs: func(d Dialect) error {
			if err := ix.Validate(); err != nil {
				return err
			}
			if _, ok := d.(FullTextQueryRanker); ok {
				if len(ws) > 0 && len(ix.Classes) == 0 {
					return errs.Wrap(errs.ErrInvalidArgument, "dao: ranking %q with weights on %s needs the index's Classes", ix.Name, d.Name())
				}
				return nil
			}
			if SupportsFullText(d) {
				return nil
			}
			return fmt.Errorf("%w: full-text ranking on %s", ErrUnsupported, d.Name())
		},
	}
}

// Snippet is an excerpt of column around the matches, for a ReadOnly field:
//
//	ChunkSnippet: {Expr: dao.Snippet(ChunkFTS, "body", dao.SnippetMarks{Open: "[", Close: "]", Ellipsis: "…", Tokens: 16}), ReadOnly: true, …}
//	// SQLite: snippet("chunk_fts", 3, '[', ']', '…', 16)
//
// The markers are constants of the declaration, quoted by the engine. A column
// not in the index panics at the call; a marker the engine cannot quote panics
// at [New].
func Snippet(ix FullTextIndex, column string, m SnippetMarks) Expr {
	col := -1
	for i, c := range ix.Columns {
		if c == column {
			col = i
		}
	}
	if col < 0 {
		panic(errs.Fatal{Op: "dao.Snippet", Rule: "the column must be one of the index's columns", Detail: column})
	}
	return Expr{render: func(d Dialect) string {
		mustValidIndex(ix, "Snippet")
		sql, err := fullTexter(d, "Snippet").FullTextSnippet(ix, col, m)
		if err != nil {
			panic(errs.Fatal{Op: "dao.Snippet", Rule: "the engine could not render the snippet", Detail: err.Error()})
		}
		return sql
	}, needs: needsIndex(ix, func(d Dialect) error {
		if err := needsFullText(d); err != nil {
			return err
		}
		_, err := d.(FullTexter).FullTextSnippet(ix, col, m)
		return err
	})}
}

// Match is the condition that a row matches q, a query in the engine's
// full-text syntax, bound:
//
//	chunks.On(tx).Join(JoinFTS).WithPredicate(dao.Match(ChunkFTS, q)).With(ChunkWorkspace, ws).
//		OrderBy(dao.Asc(ByRank)).Limit(20).Select(…)
//
// Every other predicate of the query is in the same WHERE, so it filters before
// the rank and the LIMIT. It needs a [FullTextMatcher]: on an engine without
// one the query fails with [ErrUnsupported].
//
// Match runs on a request path, so it never panics: an index that fails
// [FullTextIndex].Validate is kept as the query's error, an
// [errs.ErrInvalidArgument] returned when the query is admitted, before any
// statement is built or sent.
func Match(ix FullTextIndex, q string) Predicate { return &match{ix: ix, q: q, err: ix.Validate()} }

type match struct {
	ix  FullTextIndex
	q   string
	err error // the index's validity, decided when Match was called
}

func (p *match) checkDialect(d Dialect) error {
	if p.err != nil {
		return p.err
	}
	if _, ok := d.(FullTextMatcher); !ok {
		return fmt.Errorf("%w: full-text search on %s", ErrUnsupported, d.Name())
	}
	return nil
}

func (p *match) ToSQL(d Dialect, next *int) (string, []any) {
	*next++
	ft, ok := d.(FullTextMatcher)
	if !ok || p.err != nil { // unreachable through a DAO, which checks first
		return "1 = 0", nil
	}
	return ft.FullTextMatch(p.ix, d.Placeholder(*next)), []any{p.q}
}
