package decl

import (
	"math"
	"strings"
	"testing"
)

// A Tile's minimumWidth and minimumHeight set before its manager is built are its place's own,
// read when the manager adds it; a negative one is 0, and one that is not finite changes nothing.
func TestATilesAxisMinimumsBeforeItsManagerAreItsPlaces(t *testing.T) {
	n := &tileNode{}
	n.setMinimumWidth(20)
	n.setMinimumHeight(6)
	if n.place.MinW != 20 || n.place.MinH != 6 {
		t.Fatalf("set before the manager: MinW %d, MinH %d; want 20 and 6", n.place.MinW, n.place.MinH)
	}
	for _, x := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		n.setMinimumWidth(x)
		n.setMinimumHeight(x)
	}
	if n.place.MinW != 20 || n.place.MinH != 6 {
		t.Fatalf("after non-finite minimums: MinW %d, MinH %d; want 20 and 6 kept", n.place.MinW, n.place.MinH)
	}
	n.setMinimumWidth(-3)
	if n.place.MinW != 0 {
		t.Fatalf("a negative minimumWidth: MinW %d; want 0", n.place.MinW)
	}
}

// A place whose minimum on either axis, or on both, is not finite is refused, naming the minimums.
func TestATilePlacesNonFiniteMinimumIsRefused(t *testing.T) {
	for name, s := range map[string]tilePlaceSpec{
		"minimumWidth NaN":   {minW: math.NaN()},
		"minimumHeight +Inf": {minH: math.Inf(1)},
		"minimumSize -Inf":   {minimum: math.Inf(-1)},
	} {
		if _, err := s.done("a Tile"); err == nil || !strings.Contains(err.Error(), "finite") {
			t.Errorf("%s: %v; want it refused as not finite", name, err)
		}
	}
}

// A Tile's maximumWidth and maximumHeight set before its manager is built are its place's own, as its
// minimums are; a negative one is 0, and one that is not finite changes nothing.
func TestATilesAxisMaximumsBeforeItsManagerAreItsPlaces(t *testing.T) {
	n := &tileNode{}
	n.setMaximumWidth(40)
	n.setMaximumHeight(12)
	if n.place.MaxW != 40 || n.place.MaxH != 12 {
		t.Fatalf("set before the manager: MaxW %d, MaxH %d; want 40 and 12", n.place.MaxW, n.place.MaxH)
	}
	for _, x := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		n.setMaximumWidth(x)
		n.setMaximumHeight(x)
	}
	if n.place.MaxW != 40 || n.place.MaxH != 12 {
		t.Fatalf("after non-finite maximums: MaxW %d, MaxH %d; want 40 and 12 kept", n.place.MaxW, n.place.MaxH)
	}
	n.setMaximumWidth(-3)
	if n.place.MaxW != 0 {
		t.Fatalf("a negative maximumWidth: MaxW %d; want 0, no maximum", n.place.MaxW)
	}
}

// A place whose maximum on either axis is not finite is refused, as a minimum is.
func TestATilePlacesNonFiniteMaximumIsRefused(t *testing.T) {
	for name, s := range map[string]tilePlaceSpec{
		"maximumWidth NaN":   {maxW: math.NaN()},
		"maximumHeight -Inf": {maxH: math.Inf(-1)},
	} {
		if _, err := s.done("a Tile"); err == nil || !strings.Contains(err.Error(), "finite") {
			t.Errorf("%s: %v; want it refused as not finite", name, err)
		}
	}
}
