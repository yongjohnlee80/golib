package keymap

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui/key"
)

// Scheme is one parsed key-mapping file. It is immutable once parsed.
type Scheme struct {
	file    string
	entries []entry
}

// entry is one <shortcut>.
type entry struct {
	id     string
	keys   []key.Seq // empty with unbind set: the command has no keys
	unbind bool      // the entry was <key value=""/>
	line   int
}

// File is the name the scheme was parsed under.
func (s *Scheme) File() string { return s.file }

// IDs is the command ids the scheme names, in file order.
func (s *Scheme) IDs() []string {
	out := make([]string, len(s.entries))
	for i, e := range s.entries {
		out[i] = e.id
	}
	return out
}

// ParseScheme reads one scheme with encoding/xml's token stream. Every refusal names file and line and
// carries errs.ErrInvalidArgument, or errs.ErrUnsupported for a multi-chord key:
//   - XML that is not well formed, or a file with no <mapping> root (an empty file too);
//   - an element other than <mapping>, <shortcut> and <key>, or an attribute other than <shortcut id>
//     and <key value>, so a misspelling never silently unbinds;
//   - text where only elements belong;
//   - a <shortcut> with no id, or the same id twice;
//   - a <key> with no value attribute (value="" is the explicit unbind);
//   - a <shortcut> holding both value="" and a key, which would both unbind and bind;
//   - a value key.Parse refuses.
//
// It does not know the commands, so an unknown id is Build's to report.
func ParseScheme(r io.Reader, file string) (*Scheme, error) {
	d := xml.NewDecoder(r)
	s := &Scheme{file: file}
	root := false
	seen := map[string]int{}
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errs.WrapCause(errs.ErrInvalidArgument, err, "keymap: %s is not well-formed XML", file)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if root || t.Name.Local != "mapping" {
				return nil, errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: <%s> where <mapping> is the root", file, line(d), t.Name.Local)
			}
			if err := noAttrs(d, t, file); err != nil {
				return nil, err
			}
			root = true
			if err := s.readMapping(d, file, seen); err != nil {
				return nil, err
			}
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return nil, errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: text outside <mapping>", file, line(d))
			}
		}
	}
	if !root {
		return nil, errs.Wrap(errs.ErrInvalidArgument, "keymap: %s has no <mapping> element, so it is not a key-mapping scheme", file)
	}
	return s, nil
}

// readMapping reads <mapping>'s children through its end tag.
func (s *Scheme) readMapping(d *xml.Decoder, file string, seen map[string]int) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return errs.WrapCause(errs.ErrInvalidArgument, err, "keymap: %s: <mapping> does not end", file)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "shortcut" {
				return errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: <%s> in <mapping>, where only <shortcut> belongs", file, line(d), t.Name.Local)
			}
			e, err := readShortcut(d, t, file)
			if err != nil {
				return err
			}
			if first, dup := seen[e.id]; dup {
				return errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: shortcut %q is written a second time (first at line %d)", file, e.line, e.id, first)
			}
			seen[e.id] = e.line
			s.entries = append(s.entries, e)
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: text in <mapping>, where only <shortcut> belongs", file, line(d))
			}
		case xml.EndElement:
			return nil
		}
	}
}

// readShortcut reads one <shortcut> through its end tag.
func readShortcut(d *xml.Decoder, start xml.StartElement, file string) (entry, error) {
	e := entry{line: line(d)}
	for _, a := range start.Attr {
		if a.Name.Local != "id" || a.Name.Space != "" {
			return entry{}, errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: <shortcut> has an attribute %q, where only id belongs", file, e.line, a.Name.Local)
		}
		e.id = strings.TrimSpace(a.Value)
	}
	if e.id == "" {
		return entry{}, errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: a <shortcut> has no id", file, e.line)
	}
	for {
		tok, err := d.Token()
		if err != nil {
			return entry{}, errs.WrapCause(errs.ErrInvalidArgument, err, "keymap: %s, line %d: shortcut %q does not end", file, e.line, e.id)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			at := line(d)
			if t.Name.Local != "key" {
				return entry{}, errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: <%s> in shortcut %q, where only <key> belongs", file, at, t.Name.Local, e.id)
			}
			value, has := "", false
			for _, a := range t.Attr {
				if a.Name.Local != "value" || a.Name.Space != "" {
					return entry{}, errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: <key> has an attribute %q, where only value belongs", file, at, a.Name.Local)
				}
				value, has = a.Value, true
			}
			if !has {
				return entry{}, errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: a <key> in shortcut %q has no value (value=\"\" unbinds)", file, at, e.id)
			}
			if err := endsEmpty(d, file, at); err != nil {
				return entry{}, err
			}
			if strings.TrimSpace(value) == "" {
				e.unbind = true
			} else {
				seq, err := key.Parse(value)
				if err != nil {
					return entry{}, errs.WrapCause(errs.ErrInvalidArgument, err, "keymap: %s, line %d: shortcut %q", file, at, e.id)
				}
				e.keys = append(e.keys, seq)
			}
			if e.unbind && len(e.keys) > 0 {
				return entry{}, errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: shortcut %q both unbinds (value=\"\") and binds a key", file, at, e.id)
			}
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return entry{}, errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: text in shortcut %q, where only <key> belongs", file, line(d), e.id)
			}
		case xml.EndElement:
			if !e.unbind && len(e.keys) == 0 {
				return entry{}, errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: shortcut %q has no <key> (write <key value=\"\"/> to unbind)", file, e.line, e.id)
			}
			return e, nil
		}
	}
}

// endsEmpty reads a <key>'s content through its end tag: it holds nothing.
func endsEmpty(d *xml.Decoder, file string, at int) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return errs.WrapCause(errs.ErrInvalidArgument, err, "keymap: %s, line %d: a <key> does not end", file, at)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: <%s> inside a <key>, which holds nothing", file, line(d), t.Name.Local)
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: text inside a <key>, which holds nothing", file, at)
			}
		case xml.EndElement:
			return nil
		}
	}
}

// noAttrs refuses any attribute on an element that takes none.
func noAttrs(d *xml.Decoder, t xml.StartElement, file string) error {
	for _, a := range t.Attr {
		if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
			continue
		}
		return errs.Wrap(errs.ErrInvalidArgument, "keymap: %s, line %d: <%s> has an attribute %q, where none belongs", file, line(d), t.Name.Local, a.Name.Local)
	}
	return nil
}

// line is the decoder's current line.
func line(d *xml.Decoder) int {
	l, _ := d.InputPos()
	return l
}
