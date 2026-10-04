package search

import (
	"crypto/sha256"
	"fmt"
	"path"
	"strings"
	"sync"

	"github.com/yongjohnlee80/golib/errs"
)

// Chunk is one section of a document, as a chunker cut it.
type Chunk struct {
	Ord int // its position in the document, from 0
	// Breadcrumb names the document and the headings above the section: the context a chunk keeps
	// without overlapping its neighbours.
	Breadcrumb string
	// Body is the section's text: what lexical search indexes and a hit shows.
	Body string
	// Embed is the text to embed when it differs from the breadcrumb and body, such as a code
	// symbol's signature and documentation. "" embeds Breadcrumb + "\n" + Body.
	Embed string
	// ByteStart and ByteEnd are the section's span in the document's source.
	ByteStart, ByteEnd int
}

// EmbedText is the text a vector of the chunk is made of.
func (c Chunk) EmbedText() string {
	if c.Embed != "" {
		return c.Embed
	}
	return c.Breadcrumb + "\n" + c.Body
}

// TextHash is the sha256 of EmbedText: the chunk's identity for embedding. Two chunks with the same
// text hash share a vector, and a chunker change that keeps the text keeps it.
func (c Chunk) TextHash() [32]byte {
	return sha256.Sum256([]byte(c.EmbedText()))
}

// Doc is a document to chunk.
type Doc struct {
	Path  string
	Title string // the title the breadcrumbs start with; a chunker may read its own when ""
	Text  []byte
	// Tokens is the section budget, in estimated tokens; 0 is the chunker's default.
	Tokens int
}

// Chunker cuts documents of one kind into chunks.
type Chunker interface {
	// Version names how the chunker cuts. It changes when the same document would be cut
	// differently, so an index that records it knows to cut its documents again.
	Version() string
	Chunk(d Doc) ([]Chunk, error)
}

// ErrChunkerTaken is registering a second chunker for an extension.
var ErrChunkerTaken = errs.Sentinel(errs.ErrInvalidArgument, "search: the extension has a chunker")

// Chunkers is the chunkers an application uses, by file extension. The zero value has none and is
// ready to use; it is safe for concurrent use. An application builds its own: there is no global
// registry.
type Chunkers struct {
	mu sync.RWMutex
	by map[string]Chunker
}

// Register makes c the chunker of ext, which is a file extension with its dot (".md"), matched
// exactly as path.Ext reports it.
func (r *Chunkers) Register(ext string, c Chunker) error {
	if !strings.HasPrefix(ext, ".") || len(ext) < 2 || strings.ContainsAny(ext, "/") {
		return fmt.Errorf("%w: search: %q is not a file extension", errs.ErrInvalidArgument, ext)
	}
	if c == nil {
		return fmt.Errorf("%w: search: a nil chunker for %q", errs.ErrInvalidArgument, ext)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.by[ext]; ok {
		return fmt.Errorf("%w: %q", ErrChunkerTaken, ext)
	}
	if r.by == nil {
		r.by = map[string]Chunker{}
	}
	r.by[ext] = c
	return nil
}

// For is the chunker of p's extension.
func (r *Chunkers) For(p string) (Chunker, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.by[path.Ext(p)]
	return c, ok
}
