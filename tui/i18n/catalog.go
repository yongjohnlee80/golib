package i18n

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/yongjohnlee80/golib/errs"
)

// Catalog is one language's translated messages, by message id. It is immutable once
// parsed, so one Catalog may sit in any number of [Set]s.
type Catalog struct {
	language string
	texts    map[string]string
}

// Language is the catalog's language as its <TS language="…"> attribute names it, with
// "-" written as "_": "ko_KR", "es". It is "" when the file names none.
func (c *Catalog) Language() string { return c.language }

// ParseTS reads one catalog in Qt's TS XML schema.
//
// Only what an id-based lookup needs is kept: each message's id and its translation. A
// message with no id is refused, because nothing could ever look it up, and so is an id
// written twice, because which one wins would be an accident of order. A plural message
// (numerus="yes") is refused with [errs.ErrUnsupported]: its forms need a language's plural
// rule, which this package does not have yet. Every other refusal carries
// [errs.ErrInvalidArgument], and says where in the file it is.
func ParseTS(r io.Reader) (*Catalog, error) { return parse(r, "the catalog") }

// parse is ParseTS, naming src (a file, or "the catalog") in every refusal.
func parse(r io.Reader, src string) (*Catalog, error) {
	d := xml.NewDecoder(r)
	c := &Catalog{texts: map[string]string{}}
	root := false
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errs.WrapCause(errs.ErrInvalidArgument, err, "i18n: %s is not well-formed XML", src)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "TS":
			root = true
			c.language = canonical(attr(start, "language"))
		case "message":
			if !root {
				return nil, errs.Wrap(errs.ErrInvalidArgument, "i18n: %s has a <message> outside <TS>, at line %d", src, line(d))
			}
			if err := c.readMessage(d, start, src); err != nil {
				return nil, err
			}
		}
	}
	if !root {
		return nil, errs.Wrap(errs.ErrInvalidArgument, "i18n: %s has no <TS> element, so it is not a catalog", src)
	}
	return c, nil
}

// readMessage reads one <message>, through its end tag, and keeps its translation if it
// has one.
func (c *Catalog) readMessage(d *xml.Decoder, start xml.StartElement, src string) error {
	at := line(d)
	id := attr(start, "id")
	if id == "" {
		return errs.Wrap(errs.ErrInvalidArgument, "i18n: %s, line %d: a message has no id", src, at)
	}
	if attr(start, "numerus") == "yes" {
		return errs.Wrap(errs.ErrUnsupported, "i18n: %s, line %d: message %q is a plural message", src, at, id)
	}
	if _, dup := c.texts[id]; dup {
		return errs.Wrap(errs.ErrInvalidArgument, "i18n: %s, line %d: message %q is written a second time", src, at, id)
	}
	text, kept := "", false
	for {
		tok, err := d.Token()
		if err != nil {
			return errs.WrapCause(errs.ErrInvalidArgument, err, "i18n: %s, line %d: message %q does not end", src, at, id)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "translation" {
				// <source>, <comment>, <location> and the rest are for a translator.
				if err := d.Skip(); err != nil {
					return errs.WrapCause(errs.ErrInvalidArgument, err, "i18n: %s, line %d: message %q does not end", src, at, id)
				}
				continue
			}
			s, err := chardata(d)
			if err != nil {
				return errs.WrapCause(errs.ErrInvalidArgument, err, "i18n: %s, line %d: the translation of %q does not end", src, at, id)
			}
			switch attr(t, "type") {
			case "unfinished", "vanished", "obsolete":
				// Not translated, or no longer used: lookup must fall past it.
			default:
				text, kept = s, s != ""
			}
		case xml.EndElement:
			if kept {
				c.texts[id] = text
			}
			return nil
		}
	}
}

// chardata is the text of the element just opened, up to its end tag. A nested element is
// refused: a translation is plain text.
func chardata(d *xml.Decoder) (string, error) {
	var b strings.Builder
	for {
		tok, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.StartElement:
			return "", errors.New("a translation holds an element, where only text belongs")
		case xml.EndElement:
			return b.String(), nil
		}
	}
}

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func line(d *xml.Decoder) int {
	l, _ := d.InputPos()
	return l
}

// canonical writes a language tag the one way this package compares it: "-" as "_", so
// "pt-BR" and Qt's "pt_BR" name the same language.
func canonical(tag string) string { return strings.ReplaceAll(strings.TrimSpace(tag), "-", "_") }
