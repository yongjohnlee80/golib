package widget

import (
	"fmt"
	"strconv"
	"strings"
)

// AN ORDER BY BAR'S TEXT — the sort a user types over a table's loaded rows, in SQL's words:
//
//	key [, key]…    key = field [ASC|DESC]
//
// A field is a name, a "double-quoted name" ("" for a quote inside it), or a 1-based position. A
// bare name runs to a space, a comma or a quote. ASC and DESC are case-insensitive. The text is
// read against a table's fields, and an error says where it went wrong, for the bar to point at.

// TableOrderError is text that ParseTableOrder could not read: Offset is the byte of the token at
// fault (len(text) when the text ended too soon), Msg what is wrong with it.
type TableOrderError struct {
	Offset int
	Msg    string
}

func (e *TableOrderError) Error() string { return e.Msg }

// ParseTableOrder reads text as sort keys over fields. A name matches the field with that title
// exactly; failing that, the one field whose title matches it case-insensitively. A name two
// fields share is an error that names their positions, so the user can write one. Text of only
// spaces is no sort: nil keys and no error.
func ParseTableOrder(text string, fields []TableField) ([]TableSortKey, error) {
	p := orderParser{text: text, fields: fields}
	p.space()
	if p.i == len(text) {
		return nil, nil
	}
	var keys []TableSortKey
	for {
		key, err := p.key()
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
		if p.i == len(text) {
			return keys, nil
		}
		if text[p.i] != ',' {
			return nil, p.fail(p.i, "expected a comma before %q", p.word())
		}
		p.i++
	}
}

type orderParser struct {
	text   string
	fields []TableField
	i      int
}

func (p *orderParser) fail(at int, format string, args ...any) error {
	return &TableOrderError{Offset: at, Msg: fmt.Sprintf(format, args...)}
}

func (p *orderParser) space() {
	for p.i < len(p.text) && orderSpace(p.text[p.i]) {
		p.i++
	}
}

// word is the bare token at p.i, without moving.
func (p *orderParser) word() string {
	j := p.i
	for j < len(p.text) && !orderSpace(p.text[j]) && p.text[j] != ',' {
		j++
	}
	return p.text[p.i:j]
}

// key reads one field and its direction, and the space after them.
func (p *orderParser) key() (TableSortKey, error) {
	p.space()
	start := p.i
	if p.i == len(p.text) || p.text[p.i] == ',' {
		return TableSortKey{}, p.fail(start, "a field is missing")
	}
	name, quoted, err := p.field()
	if err != nil {
		return TableSortKey{}, err
	}
	col, err := p.resolve(name, quoted, start)
	if err != nil {
		return TableSortKey{}, err
	}
	key := TableSortKey{Col: col}
	p.space()
	if p.i < len(p.text) && p.text[p.i] != ',' {
		at, w := p.i, p.word()
		switch {
		case strings.EqualFold(w, "ASC"):
		case strings.EqualFold(w, "DESC"):
			key.Desc = true
		default:
			return TableSortKey{}, p.fail(at, "%q is not ASC or DESC", w)
		}
		p.i += len(w)
		p.space()
	}
	return key, nil
}

// field reads a quoted or a bare name.
func (p *orderParser) field() (name string, quoted bool, err error) {
	start := p.i
	if p.text[p.i] != '"' {
		for p.i < len(p.text) && !orderSpace(p.text[p.i]) && p.text[p.i] != ',' && p.text[p.i] != '"' {
			p.i++
		}
		return p.text[start:p.i], false, nil
	}
	var b strings.Builder
	for p.i++; p.i < len(p.text); p.i++ {
		if p.text[p.i] != '"' {
			b.WriteByte(p.text[p.i])
			continue
		}
		if p.i+1 < len(p.text) && p.text[p.i+1] == '"' {
			b.WriteByte('"')
			p.i++
			continue
		}
		p.i++
		return b.String(), true, nil
	}
	return "", true, p.fail(start, "the quoted name is not closed")
}

// resolve is the field a name or a position at offset at names.
func (p *orderParser) resolve(name string, quoted bool, at int) (int, error) {
	if !quoted && orderDigits(name) {
		n, err := strconv.Atoi(name)
		if err != nil || n < 1 || n > len(p.fields) {
			return 0, p.fail(at, "no field %s: the table has %d", name, len(p.fields))
		}
		return n - 1, nil
	}
	exact, folded := titled(p.fields, name, false), titled(p.fields, name, true)
	switch {
	case len(exact) == 1:
		return exact[0], nil
	case len(exact) > 1:
		return 0, p.fail(at, "%q names fields %s: write its position", name, positions(exact))
	case len(folded) == 1:
		return folded[0], nil
	case len(folded) > 1:
		return 0, p.fail(at, "%q names fields %s: write its position", name, positions(folded))
	}
	return 0, p.fail(at, "no field %q", name)
}

// titled is the fields whose title is name, compared exactly or case-insensitively.
func titled(fields []TableField, name string, fold bool) []int {
	var out []int
	for i, f := range fields {
		if f.Title == name || fold && strings.EqualFold(f.Title, name) {
			out = append(out, i)
		}
	}
	return out
}

// positions is fields as 1-based positions: "1 and 3", "1, 2 and 4".
func positions(cols []int) string {
	s := make([]string, len(cols))
	for i, c := range cols {
		s[i] = strconv.Itoa(c + 1)
	}
	if len(s) == 1 {
		return s[0]
	}
	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}

func orderSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

func orderDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// FormatTableOrder writes keys as ParseTableOrder reads them back: `name ASC, "a name" DESC`. A
// field's name is quoted when it would not read back bare, and a name two fields share is written
// as its position. Keys on fields that fields lacks are left out.
func FormatTableOrder(keys []TableSortKey, fields []TableField) string {
	var out []string
	for _, k := range keys {
		if k.Col < 0 || k.Col >= len(fields) {
			continue
		}
		name := fields[k.Col].Title
		switch {
		case len(titled(fields, name, false)) > 1:
			name = strconv.Itoa(k.Col + 1)
		case orderQuoted(name):
			name = `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		}
		if k.Desc {
			out = append(out, name+" DESC")
		} else {
			out = append(out, name+" ASC")
		}
	}
	return strings.Join(out, ", ")
}

// orderQuoted reports whether a name must be quoted to read back as itself: empty, holding a
// space, a comma or a quote, all digits (a position), or a direction's keyword.
func orderQuoted(name string) bool {
	if name == "" || orderDigits(name) || strings.EqualFold(name, "ASC") || strings.EqualFold(name, "DESC") {
		return true
	}
	return strings.ContainsAny(name, " \t\n\r,\"")
}
