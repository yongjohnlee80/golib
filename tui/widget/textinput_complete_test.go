package widget_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// resultFields are a result's fields: one whose title needs quoting, one spelled like a keyword,
// two sharing a prefix, and two differing only in case.
func resultFields() []widget.TableField {
	return []widget.TableField{
		{Title: "id"}, {Title: "Name"}, {Title: "total sum"}, {Title: "and"},
		{Title: "created_at"}, {Title: "created_by"}, {Title: "city"}, {Title: "City"},
	}
}

// completingInput is a focused TextInput, 50 cells wide, completing resultFields' names.
func completingInput(t *testing.T) (*harness, *widget.TextInput, *shell) {
	t.Helper()
	in := widget.NewTextInput(widget.WithCompleter(widget.TableFieldCompleter(resultFields)))
	sh := newShell(in)
	h := startApp(t, sh, 50, 1)
	h.inject(tab())
	h.barrier(sh)
	return h, in, sh
}

func valueOf(h *harness, in *widget.TextInput) (v string) {
	h.onLoop(func() { v = in.Value() })
	return v
}

// A typed prefix offers the names it begins, shown after the text; Tab puts in the one choice,
// quoted when the name needs it; a word where a value is due offers nothing, and Tab moves on.
func TestTextInput_TabCompletesAFieldName(t *testing.T) {
	h, in, sh := completingInput(t)
	h.inject(typeString("na")...)
	h.barrier(sh)
	if row := h.row(0); !strings.Contains(row, "na  Name") {
		t.Errorf("the choices are not shown after the text: %q", row)
	}
	h.inject(tab())
	h.barrier(sh)
	if got := valueOf(h, in); got != "Name" {
		t.Fatalf("after Tab: %q, want Name", got)
	}
	h.inject(typeString(" > 1 AND to")...)
	h.inject(tab())
	h.barrier(sh)
	if got := valueOf(h, in); got != `Name > 1 AND "total sum"` {
		t.Errorf("a name with a space: %q, want it quoted", got)
	}
	h.inject(typeString(" OR an")...)
	h.inject(tab())
	h.barrier(sh)
	if got := valueOf(h, in); !strings.HasSuffix(got, ` OR "and"`) {
		t.Errorf("a name spelled like a keyword: %q, want it quoted", got)
	}
	h.inject(typeString(" OR id = na")...)
	h.barrier(sh)
	var taken bool
	var items []string
	h.onLoop(func() {
		taken = in.HandleEvent(tab())
		items, _ = in.Completions()
	})
	if taken || items != nil || !strings.HasSuffix(valueOf(h, in), "id = na") {
		t.Errorf("after an operator: Tab taken %v, choices %v, value %q", taken, items, valueOf(h, in))
	}
}

// Several choices: Tab puts in what they all begin with, then each in turn, Shift+Tab back; any
// other key ends the walk.
func TestTextInput_TabWalksSeveralChoices(t *testing.T) {
	h, in, sh := completingInput(t)
	h.inject(typeString("cre")...)
	h.barrier(sh)
	if row := h.row(0); !strings.Contains(row, "created_at  created_by") {
		t.Errorf("the hint: %q, want the first choice's rest and the next", row)
	}
	var seen []string
	for _, k := range []tui.KeyEvent{tab(), tab(), tab(), tab(), shiftTab()} {
		h.inject(k)
		h.barrier(sh)
		seen = append(seen, valueOf(h, in))
	}
	want := []string{"created_", "created_at", "created_by", "created_at", "created_by"}
	if !slices.Equal(seen, want) {
		t.Errorf("Tab, Tab, Tab, Tab, Shift+Tab: %q, want %q", seen, want)
	}
	h.inject(typeString(" < 1 OR ci")...)
	h.inject(tab(), tab())
	h.barrier(sh)
	var items []string
	var chosen int
	h.onLoop(func() { items, chosen = in.Completions() })
	if got := valueOf(h, in); !strings.HasSuffix(got, "OR City") || !slices.Equal(items, []string{"city", "City"}) || chosen != 1 {
		t.Errorf("two names differing in case: %q, choices %v chosen %d", got, items, chosen)
	}
	h.inject(key('x'), tab())
	h.barrier(sh)
	if got := valueOf(h, in); !strings.HasSuffix(got, "OR Cityx") {
		t.Errorf("a key ended the walk, and Tab with nothing to offer: %q", got)
	}
}

// TableFieldName writes a title so both bars read it back as that field.
func TestTableFieldName_ReadsBack(t *testing.T) {
	titles := []string{"id", "Name", "total sum", "and", "asc", "1", "12x", "#tag", "-x", `say "hi"`, "a,b", "it's", "x=y", "Null"}
	var fields []widget.TableField
	for _, ti := range titles {
		fields = append(fields, widget.TableField{Title: ti})
	}
	for i, ti := range titles {
		name := widget.TableFieldName(ti)
		keys, err := widget.ParseTableOrder(name, fields)
		if err != nil || len(keys) != 1 || keys[0].Col != i {
			t.Errorf("%q as %s: ORDER BY read %+v %v", ti, name, keys, err)
		}
		if _, err := widget.ParseTableWhere(name+" IS NULL", fields); err != nil {
			t.Errorf("%q as %s: WHERE: %v", ti, name, err)
		}
	}
	for ti, want := range map[string]string{"id": "id", "total sum": `"total sum"`, "and": `"and"`, `say "hi"`: `"say ""hi"""`} {
		if got := widget.TableFieldName(ti); got != want {
			t.Errorf("%q written %s, want %s", ti, got, want)
		}
	}
}

// The completer: case-insensitive, a word opened with a quote matching past it, nothing inside a
// string, after an operator or LIKE, or for an empty word.
func TestTableFieldCompleter_Offers(t *testing.T) {
	complete := widget.TableFieldCompleter(resultFields)
	for text, want := range map[string][]string{
		"na":            {"Name"},
		"NA":            {"Name"},
		"to":            {`"total sum"`},
		`"to`:           {`"total sum"`},
		`"total s`:      {`"total sum"`},
		"x = 1 AND ci":  {"city", "City"},
		"Name DESC, cr": {"created_at", "created_by"},
		"(na":           {"Name"},
		"":              nil,
		"Name ":         nil,
		"x = na":        nil,
		"x >= na":       nil,
		"x like na":     nil,
		"x = 'na":       nil,
		"x = 'it''s na": nil,
		"zz":            nil,
	} {
		start, got := complete(text, len(text))
		if !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", text, got, want)
			continue
		}
		if got != nil && (start < 0 || start > len(text)) {
			t.Errorf("%q: start %d", text, start)
		}
	}
	if start, got := complete(`x = 1 AND "to`, 13); start != 10 || len(got) != 1 {
		t.Errorf("a quoted word starts at its quote: %d %v", start, got)
	}
}
