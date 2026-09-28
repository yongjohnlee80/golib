package sqlite

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/dao"
)

type ftChunk struct {
	ID        int64
	Workspace int64
	Title     string
	Excerpt   string
}

type ftField string

const (
	ftID      ftField = "id"
	ftWS      ftField = "workspace_id"
	ftTitle   ftField = "title"
	ftBody    ftField = "body"
	ftExcerpt ftField = "excerpt"
)

type ftSort string

const (
	ftByRank ftSort = "rank"
	ftByID   ftSort = "id"
)

const ftJoin dao.JoinKey = "fts"

// The declarative pieces against SQLite's FTS5: an external-content index kept
// by triggers, as a product's scripts keep it, queried through a DAO.
func TestFullTextOnSQLite(t *testing.T) {
	ctx := context.Background()
	conn := openMem(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	chunk, fts := "ftchunk_"+suffix, "ftchunk_fts_"+suffix
	for _, q := range []string{
		"CREATE TABLE " + chunk + " (id INTEGER PRIMARY KEY, workspace_id INTEGER NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL)",
		"CREATE VIRTUAL TABLE " + fts + " USING fts5(title, body, workspace_id UNINDEXED, content='" + chunk + "', content_rowid='id')",
		"CREATE TRIGGER " + chunk + "_ai AFTER INSERT ON " + chunk + " BEGIN INSERT INTO " + fts + "(rowid, title, body, workspace_id) VALUES (new.id, new.title, new.body, new.workspace_id); END",
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	ix := dao.FullTextIndex{Name: fts, Table: chunk, Key: "id", Columns: []string{"title", "body", "workspace_id"}}
	s := dao.New[*ftChunk, ftField, ftSort, int64](conn,
		dao.Table[*ftChunk, ftField, ftSort, int64](chunk),
		dao.ID[*ftChunk, ftField, ftSort, int64](ftID),
		dao.Fields[*ftChunk, ftField, ftSort, int64](map[ftField]dao.Field[*ftChunk]{
			ftID:    {Expr: dao.T(chunk, ftID), Scan: func(c *ftChunk) any { return &c.ID }},
			ftWS:    {Expr: dao.T(chunk, ftWS), Scan: func(c *ftChunk) any { return &c.Workspace }},
			ftTitle: {Expr: dao.T(chunk, ftTitle), Scan: func(c *ftChunk) any { return &c.Title }},
			ftBody:  {Expr: dao.T(chunk, ftBody), Scan: func(c *ftChunk) any { return new(string) }},
			ftExcerpt: {Expr: dao.Snippet(ix, "body", dao.SnippetMarks{Open: "[", Close: "]", Ellipsis: "…", Tokens: 8}),
				ReadOnly: true, Join: ftJoin, Scan: func(c *ftChunk) any { return &c.Excerpt }},
		}),
		dao.OptionalJoinExpr[*ftChunk, ftField, ftSort, int64](ftJoin, dao.FullTextJoin(ix)),
		dao.SortExpr[*ftChunk, ftField, ftSort, int64](ftByRank, dao.Rank(ix, 10, 1, 0)),
		dao.SortMap[*ftChunk, ftField, ftSort, int64](map[ftSort]string{ftByID: chunk + ".id"}))
	for _, c := range []struct {
		ws          int64
		title, body string
	}{
		{1, "plover", "a note about the marsh"},             // 1: this workspace, match in the title
		{1, "coast", "plover plover plover on the shore"},   // 2: this workspace, three matches in the body only
		{2, "plover plover", "plover plover plover plover"}, // 3: another workspace, the strongest match
		{1, "unrelated", "nothing here"},                    // 4: no match
	} {
		if _, err := s.OnCtx(ctx).Set(ftWS, c.ws).Set(ftTitle, c.title).Set(ftBody, c.body).Insert(); err != nil {
			t.Fatal(err)
		}
	}
	search := func(d dao.DAO[*ftChunk, ftField, int64], ws int64, limit uint64) []*ftChunk {
		t.Helper()
		rows, err := d.Join(ftJoin).WithPredicate(dao.Match(ix, "plover")).With(ftWS, ws).
			OrderBy(dao.Asc(ftByRank)).Limit(limit).Select(ftID, ftTitle, ftExcerpt)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		return rows
	}

	// the workspace filter is in the WHERE, before the rank and the LIMIT: the
	// other workspace's stronger match takes no place in this one's top 1
	if top := search(s.OnCtx(ctx), 1, 1); len(top) != 1 || top[0].ID != 1 {
		t.Fatalf("workspace 1's top match = %+v, want chunk 1 (chunk 3 is workspace 2's)", top)
	}
	// ranked by the weights: one title match (weight 10) above three body
	// matches (weight 1), which would rank first with equal weights
	all := search(s.OnCtx(ctx), 1, 10)
	var ids []int64
	for _, c := range all {
		ids = append(ids, c.ID)
	}
	if fmt.Sprint(ids) != "[1 2]" {
		t.Errorf("workspace 1's matches by rank = %v, want [1 2]", ids)
	}
	if len(all) == 2 && !strings.Contains(all[1].Excerpt, "[plover]") {
		t.Errorf("chunk 2's excerpt %q does not mark the match", all[1].Excerpt)
	}
	// the query is an ordinary DAO query: it runs in a transaction as well
	err := dao.RunTx(ctx, func(tx *dao.Transaction) error {
		if got := search(s.On(tx), 2, 5); len(got) != 1 || got[0].ID != 3 {
			return fmt.Errorf("workspace 2 in a transaction = %+v, want chunk 3", got)
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
	// the query text is bound: FTS5 syntax in it is the query's, and a quote
	// cannot end the statement
	if _, err := s.OnCtx(ctx).Join(ftJoin).WithPredicate(dao.Match(ix, `"marsh"`)).Select(ftID); err != nil {
		t.Errorf("a phrase query: %v", err)
	}
	if _, err := s.OnCtx(ctx).Join(ftJoin).WithPredicate(dao.Match(ix, `'); DROP TABLE x; --`)).Select(ftID); err == nil ||
		!strings.Contains(err.Error(), "fts5") {
		t.Errorf("a malformed query: %v, want FTS5's own syntax error, the text never being SQL", err)
	}
}
