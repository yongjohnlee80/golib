package search

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

func TestChunkIdentity(t *testing.T) {
	c := Chunk{Breadcrumb: "Guide > Storage", Body: "One file."}
	if c.EmbedText() != "Guide > Storage\nOne file." || c.TextHash() != sha256.Sum256([]byte("Guide > Storage\nOne file.")) {
		t.Errorf("the default embed text: %q", c.EmbedText())
	}
	c.Embed = "func Open(path string) (*Store, error)"
	if c.EmbedText() != c.Embed || c.TextHash() != sha256.Sum256([]byte(c.Embed)) {
		t.Errorf("a separate embed text: %q", c.EmbedText())
	}
}

type fixedChunker string

func (f fixedChunker) Version() string              { return string(f) }
func (f fixedChunker) Chunk(d Doc) ([]Chunk, error) { return []Chunk{{Body: string(d.Text)}}, nil }

func TestChunkers(t *testing.T) {
	var r Chunkers
	if _, ok := r.For("a.md"); ok {
		t.Error("the zero registry has a chunker")
	}
	if err := r.Register(".md", fixedChunker("1")); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(".md", fixedChunker("2")); !errors.Is(err, ErrChunkerTaken) || !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("a second chunker: %v", err)
	}
	for _, bad := range []string{"md", ".", "", "./x", "a/.md"} {
		if err := r.Register(bad, fixedChunker("1")); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("Register(%q): %v", bad, err)
		}
	}
	if err := r.Register(".txt", nil); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("a nil chunker: %v", err)
	}
	if c, ok := r.For("notes/guide.md"); !ok || c.Version() != "1" {
		t.Errorf("For(.md) = %v, %v", c, ok)
	}
	if _, ok := r.For("notes/guide.MD"); ok {
		t.Error("extensions are matched exactly")
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			_ = r.Register("."+string(rune('a'+i)), fixedChunker("x"))
			r.For("x.md")
		})
	}
	wg.Wait()
}

func TestWithFusionK(t *testing.T) {
	s := &fake{lexical: []Candidate[key]{cand("a", 0)}}
	res, _ := NewEngine[key, lexView](lexStore{s}, WithFusionK(10)).Search(context.Background(), Query{Text: "x"})
	if one, k := 1.0, 10.0; res.Hits[0].Score != one/(k+1) || res.Hits[0].Relevance != 1 {
		t.Errorf("k = 10: %+v", res.Hits[0])
	}
	res, _ = NewEngine[key, lexView](lexStore{s}, WithFusionK(-1)).Search(context.Background(), Query{Text: "x"})
	if one, k := 1.0, 60.0; res.Hits[0].Score != one/(k+1) {
		t.Errorf("a k that cannot be meant keeps 60: %+v", res.Hits[0])
	}
}
