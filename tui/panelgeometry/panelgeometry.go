// Package panelgeometry is how a program keeps its docked panels' geometry as preferences: the keys,
// the shape of each value, and the check that a key and value are one a program may store. It has no
// UI dependency, so a server that keeps a program's preferences can import it to validate them.
//
// A panel keeps three things, in percent of the window:
//
//	tui.<panel>.<edge>.size     its depth from the edge it docks to: an integer in its bounds
//	tui.<panel>.<edge>.length   its extent along that edge: an integer in its bounds
//	tui.<panel>.float           where it floats once moved: "x,y,w,h", each 0..100, w and h >= 5;
//	                            "" docks it again (Reset layout, a move to a new edge)
//
// Size and length are kept per edge, so a panel docked left and then bottom keeps both shapes.
package panelgeometry

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Spec is one panel a program docks: its preference id, the places it may dock to (left, right, top,
// bottom, or a program's own, such as AutoDoc's terminal's center), and the inclusive bounds of its
// size and length.
type Spec struct {
	ID           string
	Edges        []string
	Size, Length [2]int
}

// SizeKey is panel's size preference at edge.
func SizeKey(panel, edge string) string { return key(panel, edge, "size") }

// LengthKey is panel's length preference at edge.
func LengthKey(panel, edge string) string { return key(panel, edge, "length") }

// FloatKey is panel's floating-rectangle preference.
func FloatKey(panel string) string { return "tui." + panel + ".float" }

func key(panel, edge, what string) string { return "tui." + panel + "." + edge + "." + what }

// Percent reads a size or length preference's stored value: a decimal string within lo..hi.
func Percent(v any, lo, hi int) (int, bool) {
	s, _ := v.(string)
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= lo && n <= hi
}

// ParseFloat reads a floating rectangle, "x,y,w,h": four percentages 0..100, a width and a height of
// at least 5.
func ParseFloat(s string) ([4]float64, bool) {
	var r [4]float64
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return r, false
	}
	for i, part := range parts {
		n, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || !(n >= 0 && n <= 100) {
			return r, false
		}
		r[i] = n
	}
	return r, r[2] >= 5 && r[3] >= 5
}

// FormatFloat is r as stored: each percentage as short as it reads ("50", "0.4").
func FormatFloat(r [4]float64) string {
	parts := make([]string, 4)
	for i, v := range r {
		parts[i] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strings.Join(parts, ",")
}

// ErrNotGeometry is a key that is no panel geometry preference of the specs; ErrBadValue is one
// whose value is the wrong shape or out of its bounds.
var (
	ErrNotGeometry = errors.New("panelgeometry: not a panel geometry preference")
	ErrBadValue    = errors.New("panelgeometry: bad value")
)

// Validate reports whether key and value are a geometry preference of one of specs: an exact panel
// id, an edge that panel docks to, a size, length or float suffix, and a value of the right shape in
// that panel's bounds. A key that is no such preference is ErrNotGeometry; a value that is wrong for
// its key is ErrBadValue.
func Validate(specs []Spec, key, value string) error {
	rest, ok := strings.CutPrefix(key, "tui.")
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotGeometry, key)
	}
	for _, s := range specs {
		tail, ok := strings.CutPrefix(rest, s.ID+".")
		if !ok {
			continue
		}
		if tail == "float" {
			if value == "" {
				return nil // docked again: the program stores "" where it can't delete the key
			}
			if _, ok := ParseFloat(value); !ok {
				return fmt.Errorf("%w: %s = %q: want x,y,w,h, each 0..100, w and h >= 5", ErrBadValue, key, value)
			}
			return nil
		}
		edge, what, ok := strings.Cut(tail, ".")
		if !ok || !slices.Contains(s.Edges, edge) {
			return fmt.Errorf("%w: %q", ErrNotGeometry, key)
		}
		var bounds [2]int
		switch what {
		case "size":
			bounds = s.Size
		case "length":
			bounds = s.Length
		default:
			return fmt.Errorf("%w: %q", ErrNotGeometry, key)
		}
		if _, ok := Percent(value, bounds[0], bounds[1]); !ok {
			return fmt.Errorf("%w: %s = %q: want an integer %d..%d", ErrBadValue, key, value, bounds[0], bounds[1])
		}
		return nil
	}
	return fmt.Errorf("%w: %q", ErrNotGeometry, key)
}
