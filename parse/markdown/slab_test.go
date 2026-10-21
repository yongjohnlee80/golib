package markdown_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
)

// A node the parse lets go keeps nothing alive. A GFM table's header line starts as a paragraph,
// whose block state holds the whole line, and the table replaces it. Eighty documents of a
// 128 KiB header must not each keep that paragraph's content.
func TestAParseKeepsNothingOfANodeItLetGo(t *testing.T) {
	source := []byte("|" + strings.Repeat("x", 128<<10) + "|\n|---|\n")
	const count = 80
	docs := make([]*markdown.Document, count)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := range docs {
		docs[i] = markdown.Parse(source, markdown.GFM())
		if docs[i].Root.FirstChild.Kind != markdown.KindTable {
			t.Fatal("the fixture did not become a GFM table")
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	per := (int64(after.HeapAlloc) - int64(before.HeapAlloc)) / count
	t.Logf("each document keeps %d bytes", per)
	if per > 64<<10 {
		t.Errorf("each document keeps %d bytes; the replaced paragraph's content outlived the parse (budget %d)", per, 64<<10)
	}
	runtime.KeepAlive(docs)
	runtime.KeepAlive(source)
}
