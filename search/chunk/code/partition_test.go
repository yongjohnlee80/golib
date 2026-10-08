package code

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/search"
)

// partition asserts the rule every chunker keeps: each chunk's Body is exactly its span of the
// source; spans are ascending and disjoint; every byte outside them is whitespace, so every other
// byte is in exactly one chunk.
func partition(t *testing.T, name string, src []byte, chunks []search.Chunk) {
	t.Helper()
	pos := 0
	for i, c := range chunks {
		if c.Ord != i {
			t.Errorf("%s: chunk %d has Ord %d", name, i, c.Ord)
		}
		if c.ByteStart < pos || c.ByteEnd < c.ByteStart || c.ByteEnd > len(src) {
			t.Fatalf("%s: chunk %d span [%d,%d) after %d: not ascending and disjoint", name, i, c.ByteStart, c.ByteEnd, pos)
		}
		if c.Body != string(src[c.ByteStart:c.ByteEnd]) {
			t.Fatalf("%s: chunk %d (%s) Body is not src[%d:%d]:\n body %q\n span %q", name, i, c.Breadcrumb, c.ByteStart, c.ByteEnd, c.Body, src[c.ByteStart:c.ByteEnd])
		}
		if gap := strings.TrimSpace(string(src[pos:c.ByteStart])); gap != "" {
			t.Fatalf("%s: non-whitespace outside every chunk before chunk %d: %q", name, i, gap)
		}
		pos = c.ByteEnd
	}
	if gap := strings.TrimSpace(string(src[pos:])); gap != "" {
		t.Fatalf("%s: non-whitespace after the last chunk: %q", name, gap)
	}
}

type fixture struct {
	name string
	c    search.Chunker
	path string
	src  string
}

func allFixtures() []fixture {
	return []fixture{
		{"go", Go{}, "pkg/x/x.go", goSample},
		{"go-grouped", Go{}, "pkg/x/types.go", goGrouped},
		{"ts", TypeScript{}, "web/app.ts", tsSample},
		{"js-strings", TypeScript{}, "web/s.js", jsStrings},
		{"py", Python{}, "py/m.py", pySample},
		{"py-strings", Python{}, "py/s.py", pyStrings},
		{"rs", Rust{}, "src/lib.rs", rsSample},
		{"rs-strings", Rust{}, "src/s.rs", rsStrings},
	}
}

func TestEveryChunkerPartitionsItsSource(t *testing.T) {
	for _, f := range allFixtures() {
		for _, limit := range []int{0, 40, 12} {
			chunks, err := f.c.Chunk(search.Doc{Path: f.path, Text: []byte(f.src), Tokens: limit})
			if err != nil {
				t.Fatal(err)
			}
			partition(t, f.name, []byte(f.src), chunks)
		}
	}
}

func TestEmptyAndBlankSourcesAreOneEmptyChunk(t *testing.T) {
	for _, f := range allFixtures() {
		for _, s := range []string{"", "  \n\t\n"} {
			chunks, err := f.c.Chunk(search.Doc{Path: f.path, Text: []byte(s)})
			if err != nil {
				t.Fatal(err)
			}
			if f.name == "go" || f.name == "go-grouped" {
				continue // an empty Go file does not parse: the text fallback's own shape
			}
			if len(chunks) != 1 || chunks[0].Body != "" || chunks[0].ByteEnd != 0 {
				t.Errorf("%s %q: %+v", f.name, s, chunks)
			}
		}
	}
}

func TestVersionsAreOnePerLanguage(t *testing.T) {
	seen := map[string]string{}
	for ext, c := range Extensions() {
		v := c.Version()
		if v == "" {
			t.Errorf("%s: empty version", ext)
		}
		lang := map[string]string{".go": "go", ".ts": "ts", ".tsx": "ts", ".js": "ts", ".jsx": "ts", ".mjs": "ts", ".py": "py", ".rs": "rs"}[ext]
		if prev, ok := seen[lang]; ok && prev != v {
			t.Errorf("%s: one language with two versions %q, %q", ext, prev, v)
		}
		seen[lang] = v
	}
	if len(seen) != 4 || seen["go"] == seen["ts"] || seen["py"] == seen["rs"] || seen["go"] == seen["py"] {
		t.Errorf("versions are not distinct per language: %v", seen)
	}
}

func TestRegisterFillsTheCallersRegistry(t *testing.T) {
	var r search.Chunkers
	if err := Register(&r); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a.go", "b.tsx", "c.mjs", "d.py", "e.rs"} {
		if _, ok := r.For(p); !ok {
			t.Errorf("no chunker for %s", p)
		}
	}
	if err := Register(&r); err == nil {
		t.Error("registering twice was accepted")
	}
}

// chunkOf is the chunk whose breadcrumb ends with suffix.
func chunkOf(t *testing.T, chunks []search.Chunk, suffix string) search.Chunk {
	t.Helper()
	for _, c := range chunks {
		if strings.HasSuffix(c.Breadcrumb, suffix) {
			return c
		}
	}
	var crumbs []string
	for _, c := range chunks {
		crumbs = append(crumbs, c.Breadcrumb)
	}
	t.Fatalf("no chunk ends %q; have %q", suffix, crumbs)
	return search.Chunk{}
}
