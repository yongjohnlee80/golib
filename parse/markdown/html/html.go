// Package html renders a parse/markdown document as HTML, in the form the CommonMark specification's
// examples use. It is a consumer of the tree: the parser builds the document, and this package decides
// how it is written.
package html

import (
	"bytes"
	"io"
	"strconv"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/parse/markdown"
)

// Option configures Render.
type Option func(*config)

type config struct{ unsafe bool }

// Unsafe passes raw HTML (HTML blocks and inline HTML) through as written. By default it is escaped,
// so a document from an untrusted author renders as text rather than as markup.
func Unsafe() Option { return func(c *config) { c.unsafe = true } }

// Render writes doc as HTML.
func Render(w io.Writer, doc *markdown.Document, opts ...Option) error {
	r := &renderer{src: doc.Source}
	for _, o := range opts {
		if o != nil {
			o(&r.cfg)
		}
	}
	r.block(doc.Root)
	_, err := w.Write(r.out.Bytes())
	return err
}

type renderer struct {
	cfg config
	src []byte
	out bytes.Buffer
}

// cr starts a new line unless the output already ends one.
func (r *renderer) cr() {
	if b := r.out.Bytes(); len(b) > 0 && b[len(b)-1] != '\n' {
		r.out.WriteByte('\n')
	}
}

func (r *renderer) children(n *markdown.Node) {
	for c := n.FirstChild; c != nil; c = c.Next {
		r.block(c)
	}
}

func inTightList(p *markdown.Node) bool {
	if p.Parent == nil || p.Parent.Parent == nil {
		return false
	}
	item, list := p.Parent, p.Parent.Parent
	return item.Kind == markdown.KindItem && list.Kind == markdown.KindList && list.List.Tight
}

func (r *renderer) block(n *markdown.Node) {
	switch n.Kind {
	case markdown.KindDocument:
		r.children(n)
	case markdown.KindParagraph:
		if inTightList(n) {
			r.inlines(n)
			return
		}
		r.cr()
		r.out.WriteString("<p>")
		r.inlines(n)
		r.out.WriteString("</p>")
		r.cr()
	case markdown.KindHeading:
		r.cr()
		tag := "h" + strconv.Itoa(n.Level)
		r.out.WriteString("<" + tag + ">")
		r.inlines(n)
		r.out.WriteString("</" + tag + ">")
		r.cr()
	case markdown.KindThematicBreak:
		r.cr()
		r.out.WriteString("<hr />")
		r.cr()
	case markdown.KindBlockQuote:
		r.cr()
		r.out.WriteString("<blockquote>")
		r.cr()
		r.children(n)
		r.cr()
		r.out.WriteString("</blockquote>")
		r.cr()
	case markdown.KindList:
		r.cr()
		tag := "ul"
		if n.List.Type == markdown.ListOrdered {
			tag = "ol"
		}
		if tag == "ol" && n.List.Start != 1 {
			r.out.WriteString(`<ol start="` + strconv.Itoa(n.List.Start) + `">`)
		} else {
			r.out.WriteString("<" + tag + ">")
		}
		r.cr()
		r.children(n)
		r.cr()
		r.out.WriteString("</" + tag + ">")
		r.cr()
	case markdown.KindItem:
		r.cr()
		r.out.WriteString("<li>")
		r.children(n)
		r.out.WriteString("</li>")
		r.cr()
	case markdown.KindCodeBlock:
		r.cr()
		r.out.WriteString("<pre><code")
		if lang := firstWord(n.Info); len(lang) > 0 {
			r.out.WriteString(` class="language-`)
			r.escape(lang)
			r.out.WriteString(`"`)
		}
		r.out.WriteString(">")
		r.escape(n.Literal)
		r.out.WriteString("</code></pre>")
		r.cr()
	case markdown.KindHTMLBlock:
		r.cr()
		r.raw(n.Literal)
		r.cr()
	case markdown.KindLinkRefDef:
		// provenance only: a definition renders as nothing
	default:
		r.inline(n)
	}
}

func firstWord(info []byte) []byte {
	if i := bytes.IndexAny(info, " \t"); i >= 0 {
		return info[:i]
	}
	return info
}

func (r *renderer) inlines(n *markdown.Node) {
	for c := n.FirstChild; c != nil; c = c.Next {
		r.inline(c)
	}
}

func (r *renderer) inline(n *markdown.Node) {
	switch n.Kind {
	case markdown.KindText:
		r.escape(n.Text(r.src))
	case markdown.KindSoftBreak:
		r.out.WriteByte('\n')
	case markdown.KindHardBreak:
		r.out.WriteString("<br />\n")
	case markdown.KindCodeSpan:
		r.out.WriteString("<code>")
		r.escape(n.Literal)
		r.out.WriteString("</code>")
	case markdown.KindEmph:
		r.out.WriteString("<em>")
		r.inlines(n)
		r.out.WriteString("</em>")
	case markdown.KindStrong:
		r.out.WriteString("<strong>")
		r.inlines(n)
		r.out.WriteString("</strong>")
	case markdown.KindLink, markdown.KindAutolink:
		r.out.WriteString(`<a href="`)
		r.href(n.Dest)
		r.out.WriteString(`"`)
		if len(n.Title) > 0 {
			r.out.WriteString(` title="`)
			r.escape(n.Title)
			r.out.WriteString(`"`)
		}
		r.out.WriteString(">")
		r.inlines(n)
		r.out.WriteString("</a>")
	case markdown.KindImage:
		r.out.WriteString(`<img src="`)
		r.href(n.Dest)
		r.out.WriteString(`" alt="`)
		var alt bytes.Buffer
		plainText(&alt, n, r.src)
		r.escape(alt.Bytes())
		r.out.WriteString(`"`)
		if len(n.Title) > 0 {
			r.out.WriteString(` title="`)
			r.escape(n.Title)
			r.out.WriteString(`"`)
		}
		r.out.WriteString(" />")
	case markdown.KindRawHTML:
		r.raw(n.Literal)
	default:
		r.inlines(n)
	}
}

// plainText is an image's alt text: the text of its content, without markup.
func plainText(w *bytes.Buffer, n *markdown.Node, src []byte) {
	for c := n.FirstChild; c != nil; c = c.Next {
		switch c.Kind {
		case markdown.KindText, markdown.KindCodeSpan:
			w.Write(c.Text(src))
		case markdown.KindSoftBreak, markdown.KindHardBreak:
			w.WriteByte('\n')
		default:
			plainText(w, c, src)
		}
	}
}

func (r *renderer) raw(b []byte) {
	if r.cfg.unsafe {
		r.out.Write(b)
		return
	}
	r.escape(b)
}

// escape writes b with '&', '<', '>' and '"' as entities, and each invalid UTF-8 sequence as U+FFFD,
// so the output is valid UTF-8 whatever the input was.
func (r *renderer) escape(b []byte) {
	for i := 0; i < len(b); {
		c := b[i]
		switch c {
		case '&':
			r.out.WriteString("&amp;")
		case '<':
			r.out.WriteString("&lt;")
		case '>':
			r.out.WriteString("&gt;")
		case '"':
			r.out.WriteString("&quot;")
		default:
			if c < utf8.RuneSelf {
				r.out.WriteByte(c)
				break
			}
			ru, size := utf8.DecodeRune(b[i:])
			if ru == utf8.RuneError && size == 1 {
				r.out.WriteString("�")
			} else {
				r.out.Write(b[i : i+size])
			}
			i += size
			continue
		}
		i++
	}
}

// escapeHref percent-encodes a destination for an href or src, keeping the characters that are safe
// in a URL and any percent-escape already present.
func escapeHref(dest []byte) []byte {
	var out []byte
	for i := 0; i < len(dest); i++ {
		c := dest[i]
		switch {
		case c == '%' && i+2 < len(dest) && isHex(dest[i+1]) && isHex(dest[i+2]):
			out = append(out, dest[i:i+3]...)
			i += 2
		case urlSafe(c):
			out = append(out, c)
		default:
			const hex = "0123456789ABCDEF"
			out = append(out, '%', hex[c>>4], hex[c&15])
		}
	}
	return out
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func urlSafe(c byte) bool {
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
		return true
	}
	switch c {
	case '-', '_', '.', '!', '~', '*', '\'', '(', ')', ';', '/', '?', ':', '@', '&', '=', '+', '$', ',', '#':
		return true
	}
	return false
}

// href writes a destination as an attribute value: percent-encoded, then escaped, with an apostrophe
// written as a reference too, as the reference implementation (cmark) writes it.
func (r *renderer) href(dest []byte) {
	b := escapeHref(dest)
	for {
		i := bytes.IndexByte(b, '\'')
		if i < 0 {
			r.escape(b)
			return
		}
		r.escape(b[:i])
		r.out.WriteString("&#x27;")
		b = b[i+1:]
	}
}
