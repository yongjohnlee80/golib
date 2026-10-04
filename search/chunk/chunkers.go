package chunk

import (
	"path"
	"strings"

	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/search"
)

// MarkdownChunker is Markdown as a search.Chunker: it parses the document (GFM with Obsidian's
// extensions) and reads its title with ReadMeta when the Doc names none. A caller that already
// parsed the document, to read its links say, calls Markdown with that parse instead.
type MarkdownChunker struct{}

func (MarkdownChunker) Version() string { return Version }

func (MarkdownChunker) Chunk(d search.Doc) ([]search.Chunk, error) {
	doc := markdown.Parse(d.Text, markdown.GFM(), markdown.Obsidian())
	title := d.Title
	if title == "" {
		title = ReadMeta(doc, d.Path).Title
	}
	return Markdown(doc, title, d.Tokens), nil
}

// TextChunker is Text as a search.Chunker. Its title, when the Doc names none, is the file name
// without its extension. The text is taken as UTF-8: a caller checks it first.
type TextChunker struct{}

func (TextChunker) Version() string { return Version }

func (TextChunker) Chunk(d search.Doc) ([]search.Chunk, error) {
	title := d.Title
	if title == "" {
		title = strings.TrimSuffix(path.Base(d.Path), path.Ext(d.Path))
	}
	return Text(d.Text, title, d.Tokens), nil
}

// YAMLChunker is YAML as a search.Chunker. A YAML document names its own title (a top-level
// "title", else its file name), so Doc.Title is not used.
type YAMLChunker struct{}

func (YAMLChunker) Version() string { return Version }

func (YAMLChunker) Chunk(d search.Doc) ([]search.Chunk, error) {
	_, chunks := YAML(d.Text, d.Path, d.Tokens)
	return chunks, nil
}
