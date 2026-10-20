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
		{"tui.results.float", ""}, // docked again: AutoDoc's dockPanel writes "", and a store with no unset keeps it
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
		{"tui.explorer.left.size", "", panelgeometry.ErrBadValue},        // "" docks a float only: a size is a number
		{"tui.explorer.left.length", "", panelgeometry.ErrBadValue},
	} {
		err := panelgeometry.Validate(specs, c.key, c.value)
		if !errors.Is(err, c.want) {
			t.Errorf("%s = %s: %v, want %v", c.key, c.value, err, c.want)
		}
	}
}

// SizePercent reads an integer or up to two decimals within bounds, and nothing else: no sign,
// space, exponent, bare dot or third decimal.
func TestSizePercentReadsTwoDecimalsInBounds(t *testing.T) {
	for in, want := range map[string]float64{"30": 30, "25.25": 25.25, "25.5": 25.5, "15": 15, "90": 90, "90.00": 90} {
		if n, ok := panelgeometry.SizePercent(in, 15, 90); !ok || n != want {
			t.Errorf("SizePercent(%q) = %v %v, want %v", in, n, ok, want)
		}
	}
	for _, bad := range []any{"14.99", "90.01", "25.255", "25.", ".5", "-20", "+20", " 20", "2e1", "", "a", 30, nil} {
		if _, ok := panelgeometry.SizePercent(bad, 15, 90); ok {
			t.Errorf("SizePercent(%v) accepted", bad)
		}
	}
}

// FormatPercent writes what SizePercent reads back, rounded to two decimals, as short as it reads;
// a one-cell step of a 9999-cell extent survives the round trip.
func TestFormatPercentRoundTripsSizePercent(t *testing.T) {
	for p, want := range map[float64]string{25: "25", 25.25: "25.25", 25.251: "25.25", 25.255001: "25.26", 33.333333: "33.33"} {
		if got := panelgeometry.FormatPercent(p); got != want {
			t.Errorf("FormatPercent(%v) = %q, want %q", p, got, want)
		}
	}
	const extent = 9999
	for cells := 1; cells < extent; cells += 37 {
		back, ok := panelgeometry.SizePercent(panelgeometry.FormatPercent(100*float64(cells)/extent), 0, 100)
		if got := int(back*extent/100 + 0.5); !ok || got != cells {
			t.Fatalf("%d cells → %v → %d cells", cells, back, got)
		}
	}
}

// A Fractional spec's size takes two decimals; an integer spec's still refuses them; an edge key
// takes exactly one of the spec's edges.
func TestValidateFractionalSizesAndTheEdgeKey(t *testing.T) {
	tiles := []panelgeometry.Spec{
		{ID: "explorer", Edges: []string{"left", "right"}, Size: [2]int{20, 90}, Fractional: true},
		{ID: "results", Edges: []string{"bottom", "top"}, Size: [2]int{10, 90}},
	}
	if k := panelgeometry.EdgeKey("explorer"); k != "tui.explorer.edge" {
		t.Errorf("EdgeKey = %q", k)
	}
	for _, ok := range [][2]string{
		{"tui.explorer.left.size", "25.25"},
		{"tui.explorer.right.size", "25"},
		{"tui.explorer.edge", "right"},
		{"tui.results.edge", "top"},
		{"tui.results.bottom.size", "40"},
	} {
		if err := panelgeometry.Validate(tiles, ok[0], ok[1]); err != nil {
			t.Errorf("%s = %s: %v", ok[0], ok[1], err)
		}
	}
	for _, c := range []struct{ key, value string }{
		{"tui.explorer.left.size", "25.255"}, // a third decimal
		{"tui.explorer.left.size", "95"},     // above bounds
		{"tui.explorer.left.size", "19.99"},  // below bounds
		{"tui.results.bottom.size", "40.5"},  // not Fractional
		{"tui.explorer.edge", "bottom"},      // not one of its edges
		{"tui.explorer.edge", ""},
	} {
		if err := panelgeometry.Validate(tiles, c.key, c.value); !errors.Is(err, panelgeometry.ErrBadValue) {
			t.Errorf("%s = %s: %v, want ErrBadValue", c.key, c.value, err)
		}
	}
}
