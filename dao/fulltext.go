package dao

import (
	"fmt"
	"math"

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
	return Expr{render: func(d Dialect) string { return fullTexter(d, "FullTextJoin").FullTextJoin(ix) }, needs: needsFullText}
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
	return Expr{render: func(d Dialect) string { return fullTexter(d, "Rank").FullTextRank(ix, ws) }, needs: needsFullText}
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
		sql, err := fullTexter(d, "Snippet").FullTextSnippet(ix, col, m)
		if err != nil {
			panic(errs.Fatal{Op: "dao.Snippet", Rule: "the engine could not render the snippet", Detail: err.Error()})
		}
		return sql
	}, needs: func(d Dialect) error {
		if err := needsFullText(d); err != nil {
			return err
		}
		_, err := d.(FullTexter).FullTextSnippet(ix, col, m)
		return err
	}}
}

// Match is the condition that a row matches q, a query in the engine's
// full-text syntax, bound:
//
//	chunks.On(tx).Join(JoinFTS).WithPredicate(dao.Match(ChunkFTS, q)).With(ChunkWorkspace, ws).
//		OrderBy(dao.Asc(ByRank)).Limit(20).Select(…)
//
// Every other predicate of the query is in the same WHERE, so it filters before
// the rank and the LIMIT. On an engine without full-text search the query
// fails with [ErrUnsupported].
func Match(ix FullTextIndex, q string) Predicate { return &match{ix: ix, q: q} }

type match struct {
	ix FullTextIndex
	q  string
}

func (p *match) checkDialect(d Dialect) error { return needsFullText(d) }

func (p *match) ToSQL(d Dialect, next *int) (string, []any) {
	*next++
	ft, ok := d.(FullTexter)
	if !ok { // unreachable through a DAO, which checks first
		return "1 = 0", nil
	}
	return ft.FullTextMatch(p.ix, d.Placeholder(*next)), []any{p.q}
}
