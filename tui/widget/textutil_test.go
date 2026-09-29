package widget

import (
	"reflect"
	"testing"
)

func runeWidth(s string) int {
	return len([]rune(s))
}

func TestTextUtilClustersAndCellsBefore(t *testing.T) {
	s := "hello"
	cs := clusters(s)
	if !reflect.DeepEqual(cs, []string{"h", "e", "l", "l", "o"}) {
		t.Errorf("clusters(%q) = %v", s, cs)
	}
	if got := cellsBefore(cs, 3, runeWidth); got != 3 {
		t.Errorf("cellsBefore = %d, want 3", got)
	}
	if got := clusters(""); got != nil {
		t.Errorf("clusters(\"\") = %v, want nil", got)
	}
}

func TestTextUtilTruncate(t *testing.T) {
	if got := truncate("short", 10, runeWidth); got != "short" {
		t.Errorf("truncate short: %q", got)
	}
	if got := truncate("a very long string", 6, runeWidth); got != "a ver…" {
		t.Errorf("truncate long: %q, want %q", got, "a ver…")
	}
	if got := truncate("hello", 1, runeWidth); got != "…" {
		t.Errorf("truncate w=1: %q, want %q", got, "…")
	}
	if got := truncate("hello", 0, runeWidth); got != "" {
		t.Errorf("truncate w=0: %q, want \"\"", got)
	}
}

func TestTextUtilWrapLineAndRanges(t *testing.T) {
	s := "alpha beta gamma"
	rows := wrapLine(s, 6, runeWidth)
	wantRows := []string{"alpha", "beta", "gamma"}
	if !reflect.DeepEqual(rows, wantRows) {
		t.Errorf("wrapLine(%q, 6) = %v, want %v", s, rows, wantRows)
	}

	cs := clusters(s)
	ranges := wrapRanges(cs, 6, runeWidth)
	if len(ranges) != 3 {
		t.Fatalf("wrapRanges len = %d, want 3: %v", len(ranges), ranges)
	}
}

// Each Elide mode fits the text to the width where Qt's Qt::TextElideMode
// drops it, counting a wide grapheme as the cells it takes; text that fits is
// untouched in every mode.
func TestTextUtilElide(t *testing.T) {
	cells := func(s string) int {
		n := 0
		for _, r := range s {
			if r >= 0x3000 { // CJK: two cells
				n += 2
			} else {
				n++
			}
		}
		return n
	}
	path := "shared/adrs/0206-store.md"
	for _, c := range []struct {
		s    string
		w    int
		mode Elide
		want string
	}{
		{path, 12, ElideRight, "shared/adrs…"},
		{path, 12, ElideLeft, "…06-store.md"},
		{path, 12, ElideMiddle, "shared…re.md"}, // an odd share: the head takes the extra cell
		{path, 12, ElideNone, "shared/adrs/"},
		{path, 25, ElideLeft, path},
		{path, 1, ElideLeft, "…"},
		{path, 0, ElideLeft, ""},
		{"notes/日本語.md", 8, ElideLeft, "…本語.md"},                        // 8 cells: 日 as well would make 10
		{"shared/adrs/0206-store.md", 17, ElidePath, "…/0206-store.md"}, // "…/adrs/0206-store.md" is 20
		{"shared/adrs/0206-store.md", 21, ElidePath, "…/adrs/0206-store.md"},
		{"shared/adrs/0206-store.md", 12, ElidePath, "…06-store.md"}, // not even the name fits: as ElideLeft
		{"shared/adrs/0206-store.md", 25, ElidePath, path},
		{"日本語メモ", 6, ElideMiddle, "日…モ"}, // 5 cells: 本 or メ would make 7
	} {
		if got := elide(c.s, c.w, c.mode, cells); got != c.want {
			t.Errorf("elide(%q, %d, %d) = %q, want %q", c.s, c.w, c.mode, got, c.want)
		}
		if cells(elide(c.s, c.w, c.mode, cells)) > max(c.w, 0) {
			t.Errorf("elide(%q, %d, %d) is wider than %d", c.s, c.w, c.mode, c.w)
		}
	}
}
