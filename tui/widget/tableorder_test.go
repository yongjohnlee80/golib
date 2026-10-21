package widget

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// orderFields are a result's fields: a quoted-only name, two sharing a name, two differing only in
// case, and one that is unique only case-insensitively.
func orderFields() []TableField {
	return []TableField{
		{Title: "id"},        // 1
		{Title: "Name"},      // 2
		{Title: "total sum"}, // 3
		{Title: "id"},        // 4
		{Title: "City"},      // 5
		{Title: "city"},      // 6
		{Title: `say "hi"`},  // 7
		{Title: "Age"},       // 8
	}
}

// ParseTableOrder reads names, quoted names and positions, ASC and DESC in any case, and several
// keys in order; a name matches exactly first, then case-insensitively when one field matches.
func TestParseTableOrder_ReadsNamesQuotedNamesAndPositions(t *testing.T) {
	fields := orderFields()
	for text, want := range map[string][]TableSortKey{
		"":                          nil,
		"   ":                       nil,
		"Name":                      {{Col: 1}},
		"name":                      {{Col: 1}}, // one field matches case-insensitively
		"NAME desc":                 {{Col: 1, Desc: true}},
		"Name ASC":                  {{Col: 1}},
		`"total sum" DeSc`:          {{Col: 2, Desc: true}},
		`"say ""hi"""`:              {{Col: 6}},
		"2":                         {{Col: 1}},
		"8 DESC":                    {{Col: 7, Desc: true}},
		"City":                      {{Col: 4}}, // exact, though city matches it case-insensitively too
		"city desc":                 {{Col: 5, Desc: true}},
		"age DESC,2 asc ,  City":    {{Col: 7, Desc: true}, {Col: 1}, {Col: 4}},
		"\tName\n,\r1 DESC":         {{Col: 1}, {Col: 0, Desc: true}},
		`"Name"`:                    {{Col: 1}},
		"Name DESC, Name ASC":       {{Col: 1, Desc: true}, {Col: 1}},
		"Name,8":                    {{Col: 1}, {Col: 7}},
		"age desc , city asc , 001": {{Col: 7, Desc: true}, {Col: 5}, {Col: 0}},
	} {
		got, err := ParseTableOrder(text, fields)
		if err != nil {
			t.Errorf("%q: %v", text, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %+v, want %+v", text, got, want)
		}
	}
}

// Each error says what is wrong and the byte where: an unknown name, a name two fields share (with
// their positions), a position out of range, a missing field, a word that is not a direction, an
// unclosed quote, and two keys without a comma.
func TestParseTableOrder_ErrorsCarryTheOffsetOfTheirToken(t *testing.T) {
	fields := orderFields()
	for _, c := range []struct {
		text   string
		offset int
		says   string
	}{
		{"nope", 0, `no field "nope"`},
		{"Name, nope DESC", 6, `no field "nope"`},
		{"Name,   id", 8, `"id" names fields 1 and 4`},
		{`age, "ID"`, 5, `"ID" names fields 1 and 4`},
		{"9", 0, "no field 9: the table has 8"},
		{"Name, 0", 6, "no field 0"},
		{"Name,", 5, "a field is missing"},
		{", Name", 0, "a field is missing"},
		{"Name,,age", 5, "a field is missing"},
		{"Name UP", 5, `"UP" is not ASC or DESC`},
		{`Name, "total sum`, 6, "the quoted name is not closed"},
		{"Name ASC age", 9, `expected a comma before "age"`},
		{`"1"`, 0, `no field "1"`},
		{`Name"x"`, 4, `"\"x\"" is not ASC or DESC`},
	} {
		_, err := ParseTableOrder(c.text, fields)
		var oe *TableBarError
		if !errors.As(err, &oe) {
			t.Errorf("%q: err %v, want a *TableBarError", c.text, err)
			continue
		}
		if oe.Offset != c.offset || !strings.Contains(oe.Error(), c.says) {
			t.Errorf("%q: offset %d %q, want offset %d saying %q", c.text, oe.Offset, oe.Error(), c.offset, c.says)
		}
	}
	if _, err := ParseTableOrder("a", nil); err == nil {
		t.Error("a name over no fields parsed")
	}
}

// FormatTableOrder writes what ParseTableOrder reads back to the same keys: names bare when they
// can be, quoted when not, a shared name by its position.
func TestFormatTableOrder_RoundTrips(t *testing.T) {
	fields := orderFields()
	keys := []TableSortKey{{Col: 1}, {Col: 2, Desc: true}, {Col: 3}, {Col: 6, Desc: true}, {Col: 4}, {Col: 99}}
	text := FormatTableOrder(keys, fields)
	if want := `Name ASC, "total sum" DESC, 4 ASC, "say ""hi""" DESC, City ASC`; text != want {
		t.Errorf("formatted %q, want %q", text, want)
	}
	back, err := ParseTableOrder(text, fields)
	if err != nil || !reflect.DeepEqual(back, keys[:5]) {
		t.Errorf("read back %+v (%v), want %+v", back, err, keys[:5])
	}
	odd := []TableField{{Title: "12"}, {Title: "desc"}, {Title: ""}, {Title: "a,b"}}
	all := []TableSortKey{{Col: 0}, {Col: 1}, {Col: 2}, {Col: 3}}
	text = FormatTableOrder(all, odd)
	if back, err := ParseTableOrder(text, odd); err != nil || !reflect.DeepEqual(back, all) {
		t.Errorf("%q read back %+v (%v), want %+v", text, back, err, all)
	}
	if FormatTableOrder(nil, fields) != "" {
		t.Error("no keys formatted as something")
	}
}
