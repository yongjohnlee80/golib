package docx

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/extract"
)

var (
	// ErrEntryTooLarge is an archive entry that decompresses past the per-entry limit. A ZIP
	// header's size can lie, so the bytes are counted as they are read.
	ErrEntryTooLarge = errs.Sentinel(errs.ErrInvalidArgument, "docx: an archive entry is larger than the entry limit")
	// ErrArchiveTooLarge is the entries read decompressing past the total limit together.
	ErrArchiveTooLarge = errs.Sentinel(errs.ErrInvalidArgument, "docx: the archive's entries are larger than the total limit")
	// ErrTooDeep is XML nested past the depth limit.
	ErrTooDeep = errs.Sentinel(errs.ErrInvalidArgument, "docx: XML nested past the depth limit")
	// ErrBlockTooLarge is one paragraph or table row past the block limit.
	ErrBlockTooLarge = errs.Sentinel(errs.ErrInvalidArgument, "docx: a paragraph or table row is larger than the block limit")
	// ErrNotDocx is a ZIP archive with no word/document.xml.
	ErrNotDocx = errs.Sentinel(errs.ErrInvalidArgument, "docx: not a Word document")
)

// The limits a zero Extractor uses.
const (
	DefaultMaxEntry = 256 << 20 // decompressed bytes of one entry
	DefaultMaxTotal = 512 << 20 // decompressed bytes of every entry read
	DefaultMaxDepth = 256
	DefaultMaxBlock = 4 << 20 // bytes of one paragraph or table row
)

// Extractor is an extract.Extractor for Word documents (.docx). It reads the archive's directory
// through the io.ReaderAt, the styles and numbering for heading levels and list kinds, and the
// document one XML token at a time, holding one paragraph or table row at a time. Headings become
// #–######, list items "- " or "1. ", tables GFM tables, and a hyperlink keeps its text; media,
// headers, footers and comments are skipped. Info.Title comes from the core properties and
// Info.Pages from the application properties when they are present.
//
// The zero value is ready to use with the default limits; a field set to a positive value
// replaces its default. An Extractor holds no state, so one serves any number of goroutines.
type Extractor struct {
	MaxEntry, MaxTotal int64
	MaxDepth, MaxBlock int
}

// ID names this extractor for a derived cache's key.
func (Extractor) ID() string { return "golib/docx" }

// Version changes whenever the Markdown for the same document may change.
func (Extractor) Version() string { return "1" }

func (e Extractor) limits() (maxEntry, maxTotal int64, maxDepth, maxBlock int) {
	maxEntry, maxTotal, maxDepth, maxBlock = DefaultMaxEntry, DefaultMaxTotal, DefaultMaxDepth, DefaultMaxBlock
	if e.MaxEntry > 0 {
		maxEntry = e.MaxEntry
	}
	if e.MaxTotal > 0 {
		maxTotal = e.MaxTotal
	}
	if e.MaxDepth > 0 {
		maxDepth = e.MaxDepth
	}
	if e.MaxBlock > 0 {
		maxBlock = e.MaxBlock
	}
	return
}

// Extract writes the document's Markdown to w.
func (e Extractor) Extract(ctx context.Context, r io.ReaderAt, size int64, w io.Writer) (extract.Info, error) {
	if size < 0 {
		return extract.Info{}, errs.Wrap(errs.ErrInvalidArgument, "docx: negative size %d", size)
	}
	if err := ctx.Err(); err != nil {
		return extract.Info{}, err
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return extract.Info{}, fmt.Errorf("docx: %w", err)
	}
	maxEntry, maxTotal, maxDepth, maxBlock := e.limits()
	x := &doc{ctx: ctx, files: map[string]*zip.File{}, maxEntry: maxEntry, maxTotal: maxTotal, maxDepth: maxDepth, maxBlock: maxBlock,
		styles: map[string]int{}, numFmt: map[string]map[int]bool{}}
	for _, f := range zr.File {
		x.files[f.Name] = f
	}
	if x.files["word/document.xml"] == nil {
		return extract.Info{}, ErrNotDocx
	}
	var info extract.Info
	if err := x.read("word/styles.xml", x.readStyles); err != nil {
		return info, err
	}
	if err := x.read("word/numbering.xml", x.readNumbering); err != nil {
		return info, err
	}
	if err := x.read("docProps/core.xml", func(d *xml.Decoder) error {
		info.Title, err = textOf(d, x, "title")
		return err
	}); err != nil {
		return info, err
	}
	if err := x.read("docProps/app.xml", func(d *xml.Decoder) error {
		s, err := textOf(d, x, "Pages")
		if n, perr := strconv.Atoi(strings.TrimSpace(s)); perr == nil {
			info.Pages = n
		}
		return err
	}); err != nil {
		return info, err
	}
	bw := bufio.NewWriter(w)
	x.out = bw
	if err := x.read("word/document.xml", x.readDocument); err != nil {
		bw.Flush()
		return info, err
	}
	if err := bw.Flush(); err != nil {
		return info, err
	}
	return info, nil
}

// doc is one extraction's state.
type doc struct {
	ctx                context.Context
	files              map[string]*zip.File
	maxEntry, maxTotal int64
	maxDepth, maxBlock int
	total              int64 // decompressed bytes read across entries

	styles map[string]int          // paragraph style id -> heading level 1-6
	numFmt map[string]map[int]bool // numId -> level -> ordered
	abs    map[string]map[int]bool // abstractNumId -> level -> ordered

	out     *bufio.Writer
	started bool // a block has been written
	inList  bool // the last block was a list item
}

// read decodes one archive entry, if present, through the counting limits.
func (x *doc) read(name string, fn func(*xml.Decoder) error) error {
	f := x.files[name]
	if f == nil {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("docx: %s: %w", name, err)
	}
	defer rc.Close()
	d := xml.NewDecoder(&counting{r: rc, x: x})
	d.Strict = false
	if err := fn(d); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("docx: %s: %w", name, err)
	}
	return nil
}

// counting counts the bytes an entry decompresses to, against the entry and the total limits.
type counting struct {
	r io.Reader
	n int64
	x *doc
}

func (c *counting) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	c.x.total += int64(n)
	if c.n > c.x.maxEntry {
		return n, ErrEntryTooLarge
	}
	if c.x.total > c.x.maxTotal {
		return n, ErrArchiveTooLarge
	}
	return n, err
}

// tokens walks a decoder's tokens with the depth limit, calling fn for each, and checks for
// cancellation as it goes.
func (x *doc) tokens(d *xml.Decoder, fn func(tok xml.Token, depth int) error) error {
	depth := 0
	for n := 0; ; n++ {
		if n%4096 == 0 {
			if err := x.ctx.Err(); err != nil {
				return err
			}
		}
		tok, err := d.RawToken()
		if err != nil {
			return err
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
			if depth > x.maxDepth {
				return ErrTooDeep
			}
		}
		if err := fn(tok, depth); err != nil {
			return err
		}
		if _, ok := tok.(xml.EndElement); ok {
			depth--
		}
	}
}

func attr(e xml.StartElement, local string) string {
	for _, a := range e.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// textOf is the text of the first element named local.
func textOf(d *xml.Decoder, x *doc, local string) (string, error) {
	var b strings.Builder
	in := false
	err := x.tokens(d, func(tok xml.Token, _ int) error {
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == local {
				in = true
			}
		case xml.EndElement:
			if in && t.Name.Local == local {
				return io.EOF
			}
		case xml.CharData:
			if in && b.Len() < 4096 {
				b.Write(t)
			}
		}
		return nil
	})
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return strings.TrimSpace(b.String()), err
}

// readStyles maps paragraph styles to heading levels: an outline level, or a "heading N" or
// "Title" name.
func (x *doc) readStyles(d *xml.Decoder) error {
	id := ""
	return x.tokens(d, func(tok xml.Token, _ int) error {
		t, ok := tok.(xml.StartElement)
		if !ok {
			return nil
		}
		switch t.Name.Local {
		case "style":
			id = ""
			if attr(t, "type") == "paragraph" {
				id = attr(t, "styleId")
			}
		case "name":
			if id == "" {
				return nil
			}
			name := strings.ToLower(attr(t, "val"))
			if name == "title" {
				x.styles[id] = 1
			} else if n, err := strconv.Atoi(strings.TrimPrefix(name, "heading ")); err == nil && strings.HasPrefix(name, "heading ") && n >= 1 {
				x.styles[id] = min(n, 6)
			}
		case "outlineLvl":
			if id != "" {
				if n, err := strconv.Atoi(attr(t, "val")); err == nil && n >= 0 && n < 9 {
					x.styles[id] = min(n+1, 6)
				}
			}
		}
		return nil
	})
}

// readNumbering records which list levels are ordered (any format but a bullet).
func (x *doc) readNumbering(d *xml.Decoder) error {
	x.abs = map[string]map[int]bool{}
	numAbs := map[string]string{}
	absID, num := "", ""
	lvl := 0
	err := x.tokens(d, func(tok xml.Token, _ int) error {
		t, ok := tok.(xml.StartElement)
		if !ok {
			return nil
		}
		switch t.Name.Local {
		case "abstractNum":
			absID = attr(t, "abstractNumId")
			x.abs[absID] = map[int]bool{}
		case "lvl":
			lvl, _ = strconv.Atoi(attr(t, "ilvl"))
		case "numFmt":
			if m := x.abs[absID]; m != nil {
				m[lvl] = attr(t, "val") != "bullet" && attr(t, "val") != "none"
			}
		case "num":
			num = attr(t, "numId")
		case "abstractNumId":
			if num != "" {
				numAbs[num] = attr(t, "val")
			}
		}
		return nil
	})
	for n, a := range numAbs {
		x.numFmt[n] = x.abs[a]
	}
	return err
}
