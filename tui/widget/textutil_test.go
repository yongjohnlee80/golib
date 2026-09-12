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
