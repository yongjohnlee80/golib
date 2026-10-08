package code

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yongjohnlee80/golib/search"
)

// The chunks this package cuts are byte-for-byte the ones autorag's codechunk cut before the
// package moved here. The goldens were captured from autorag at e25f508, so a document an older
// build indexed is current under this one: same chunks, same versions, same hashes.
func TestChunksMatchTheMovedPackage(t *testing.T) {
	for _, c := range []struct {
		file, path string
		ch         search.Chunker
	}{
		{"sample.go.txt", "graph/graph.go", Go{}},
		{"sample.ts.txt", "src/a.ts", TypeScript{}},
		{"sample.py.txt", "pkg/a.py", Python{}},
		{"sample.rs.txt", "src/lib.rs", Rust{}},
	} {
		t.Run(c.file, func(t *testing.T) {
			dir := filepath.Join("testdata", "parity")
			src, err := os.ReadFile(filepath.Join(dir, c.file))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(dir, c.file+".golden.json"))
			if err != nil {
				t.Fatal(err)
			}
			chunks, err := c.ch.Chunk(search.Doc{Path: c.path, Text: src})
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.MarshalIndent(struct {
				Version string
				Chunks  []search.Chunk
			}{c.ch.Version(), chunks}, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if got = append(got, '\n'); !bytes.Equal(got, want) {
				t.Errorf("%s: chunks differ from the moved package's (golden %d bytes, got %d)", c.file, len(want), len(got))
			}
		})
	}
}
