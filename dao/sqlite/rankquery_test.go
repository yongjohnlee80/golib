package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/dao"
)

// A RankQuery key on SQLite renders bm25 and binds nothing, so it orders
// exactly as the plain Rank does, and a valid Config and Classes on the index
// change nothing: SQLite ignores both. SQLite refuses a statement whose
// arguments outnumber its placeholders, so the query running at all is the
// count check.
func TestRankQueryOnSQLite(t *testing.T) {
	ctx := context.Background()
	conn := openMem(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	chunk, fts := "rqchunk_"+suffix, "rqchunk_fts_"+suffix
	for _, q := range []string{
		"CREATE TABLE " + chunk + " (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL)",
		"CREATE VIRTUAL TABLE " + fts + " USING fts5(title, body, workspace_id UNINDEXED, content='" + chunk + "', content_rowid='id')",
		"CREATE TRIGGER " + chunk + "_ai AFTER INSERT ON " + chunk + " BEGIN INSERT INTO " + fts + "(rowid, title, body, workspace_id) VALUES (new.id, new.title, new.body, new.workspace_id); END",
		"INSERT INTO " + chunk + " (workspace_id, title, body) VALUES (1, 'plover', 'a note about the marsh'), (1, 'coast', 'plover plover plover on the shore'), (1, 'unrelated', 'nothing here')",
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	plain := dao.FullTextIndex{Name: fts, Table: chunk, Key: "id", Columns: []string{"title", "body", "workspace_id"}}
	dressed := plain
	dressed.Config, dressed.Classes = "english", []byte("ADC")
	order := func(ix dao.FullTextIndex, byQuery bool) []int64 {
		t.Helper()
		opts := []dao.Option[*ftChunk, ftField, ftSort, int64]{
			dao.Table[*ftChunk, ftField, ftSort, int64](chunk),
			dao.ID[*ftChunk, ftField, ftSort, int64](ftID),
			dao.Fields[*ftChunk, ftField, ftSort, int64](map[ftField]dao.Field[*ftChunk]{
				ftID: {Expr: dao.T(chunk, ftID), Scan: func(c *ftChunk) any { return &c.ID }},
			}),
			dao.OptionalJoinExpr[*ftChunk, ftField, ftSort, int64](ftJoin, dao.FullTextJoin(ix)),
		}
		srt := dao.Asc(ftByRank)
		if byQuery {
			opts = append(opts, dao.SortParam[*ftChunk, ftField, ftSort, int64](ftByRank, dao.RankQuery(ix, 10, 1, 0)))
			srt = dao.AscBy(ftByRank, "plover")
		} else {
			opts = append(opts, dao.SortExpr[*ftChunk, ftField, ftSort, int64](ftByRank, dao.Rank(ix, 10, 1, 0)))
		}
		rows, err := dao.New[*ftChunk, ftField, ftSort, int64](conn, opts...).OnCtx(ctx).
			Join(ftJoin).WithPredicate(dao.Match(ix, "plover")).OrderBy(srt).Limit(10).Select(ftID)
		if err != nil {
			t.Fatalf("query (byQuery %v): %v", byQuery, err)
		}
		var ids []int64
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		return ids
	}
	want := fmt.Sprint(order(plain, false))
	if want != "[1 2]" {
		t.Fatalf("the plain Rank orders %s, want [1 2] (title weight 10 over body weight 1)", want)
	}
	for name, got := range map[string][]int64{
		"RankQuery":                  order(plain, true),
		"RankQuery, Config, Classes": order(dressed, true),
		"Rank, Config, Classes":      order(dressed, false),
	} {
		if fmt.Sprint(got) != want {
			t.Errorf("%s orders %v, want %s as the plain Rank does", name, got, want)
		}
	}
}
