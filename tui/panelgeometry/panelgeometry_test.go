package panelgeometry_test

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/tui/panelgeometry"
)

var specs = []panelgeometry.Spec{
	{ID: "explorer", Edges: []string{"left", "right"}, Size: [2]int{15, 90}, Length: [2]int{20, 100}},
	{ID: "results", Edges: []string{"bottom", "top"}, Size: [2]int{10, 90}, Length: [2]int{10, 100}},
}

// The keys are the ones AutoDoc has always stored, so moving the codec changes no saved preference.
func TestTheKeysAreTheStoredOnes(t *testing.T) {
	for got, want := range map[string]string{
		panelgeometry.SizeKey("explorer", "left"):    "tui.explorer.left.size",
		panelgeometry.LengthKey("results", "bottom"): "tui.results.bottom.length",
		panelgeometry.FloatKey("terminal"):           "tui.terminal.float",
	} {
		if got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
}

// A rectangle reads back as written, and each malformed one is refused.
func TestAFloatRoundTripsAndABadOneIsRefused(t *testing.T) {
	r := [4]float64{10, 20.5, 50, 40}
	if got, ok := panelgeometry.ParseFloat(panelgeometry.FormatFloat(r)); !ok || got != r {
		t.Fatalf("round trip: %v %v", got, ok)
	}
	if s := panelgeometry.FormatFloat([4]float64{0.4, 50, 5, 5}); s != "0.4,50,5,5" {
		t.Errorf("FormatFloat = %q", s)
	}
	for _, bad := range []string{"", "1,2,3", "1,2,3,4,5", "a,2,30,30", "-1,2,30,30", "1,2,101,30", "1,2,4,30", "1,2,30,4.9"} {
		if _, ok := panelgeometry.ParseFloat(bad); ok {
			t.Errorf("ParseFloat(%q) accepted", bad)
		}
	}
}

// Percent reads an integer within its bounds and nothing else.
func TestPercentReadsOnlyAnIntegerInBounds(t *testing.T) {
	if n, ok := panelgeometry.Percent("30", 15, 90); !ok || n != 30 {
		t.Errorf("30: %d %v", n, ok)
	}
	for _, bad := range []any{"14", "91", "3.5", "", 30, nil} {
		if _, ok := panelgeometry.Percent(bad, 15, 90); ok {
			t.Errorf("Percent(%v) accepted", bad)
		}
	}
}

// Validate accepts exactly each panel's three preferences with good values, and says which way the
// rest are wrong: no such preference, or a bad value for one.
func TestValidateAcceptsOnlyAPanelsOwnPreferences(t *testing.T) {
	for _, ok := range [][2]string{
		{"tui.explorer.left.size", "30"},
		{"tui.explorer.right.length", "100"},
		{"tui.results.bottom.size", "10"},
		{"tui.results.float", "10,20,50,40"},
	} {
		if err := panelgeometry.Validate(specs, ok[0], ok[1]); err != nil {
			t.Errorf("%s = %s: %v", ok[0], ok[1], err)
		}
	}
	for _, c := range []struct {
		key, value string
		want       error
	}{
		{"tui.terminal.left.size", "30", panelgeometry.ErrNotGeometry},   // no such panel
		{"tui.explorer.bottom.size", "30", panelgeometry.ErrNotGeometry}, // an edge it doesn't dock to
		{"tui.explorer.left.width", "30", panelgeometry.ErrNotGeometry},  // no such suffix
		{"tui.explorer.size", "30", panelgeometry.ErrNotGeometry},        // no edge
		{"explorer.left.size", "30", panelgeometry.ErrNotGeometry},       // not a tui key
		{"tui.explorerx.left.size", "30", panelgeometry.ErrNotGeometry},  // a prefix of an id is not the id
		{"tui.explorer.left.size", "14", panelgeometry.ErrBadValue},      // below bounds
		{"tui.explorer.left.length", "101", panelgeometry.ErrBadValue},   // above bounds
		{"tui.results.float", "1,2,3", panelgeometry.ErrBadValue},        // a malformed rectangle
	} {
		err := panelgeometry.Validate(specs, c.key, c.value)
		if !errors.Is(err, c.want) {
			t.Errorf("%s = %s: %v, want %v", c.key, c.value, err, c.want)
		}
	}
}
