package decl

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// Both int readers refuse NaN and ±Inf, naming the value and where it came from: Go's int(NaN)
// and int(±Inf) are any value at all. Every other number reads as it always did: truncOf
// truncates, cellsOf refuses a fraction and a negative.
func TestTheIntReadersRefuseNaNAndInfinities(t *testing.T) {
	at := parse.Position{File: "main.qml", Line: 3, Column: 7}
	for _, raw := range []string{"NaN", "+Inf", "-Inf"} {
		v := qml.SpecValue{Kind: qml.SpecValueNumber, Raw: raw, Pos: at}
		for name, read := range map[string]func(qml.SpecValue) (int, error){"truncOf": truncOf, "cellsOf": cellsOf} {
			n, err := read(v)
			if err == nil || !strings.Contains(err.Error(), "finite") || !strings.Contains(err.Error(), raw) ||
				!strings.Contains(err.Error(), at.String()) {
				t.Errorf("%s(%s) = %d, %v; want it refused as not finite, naming %s at %s", name, raw, n, err, raw, at)
			}
		}
	}
	num := func(raw string) qml.SpecValue { return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: raw, Pos: at} }
	if n, err := truncOf(num("33.7")); n != 33 || err != nil {
		t.Errorf("truncOf(33.7) = %d, %v; want 33 (truncated, as always)", n, err)
	}
	if _, err := cellsOf(num("33.7")); err == nil {
		t.Error("cellsOf(33.7) accepted a fraction")
	}
	if n, err := cellsOf(num("12")); n != 12 || err != nil {
		t.Errorf("cellsOf(12) = %d, %v", n, err)
	}
}

// Both int readers refuse a finite number outside int's range, naming the value and where it came
// from, as they refuse NaN and ±Inf: Go's int() of an out-of-range float is implementation-defined.
// Before, truncOf cast 1e300 to whatever the platform made of it.
func TestTheIntReadersRefuseANumberOutsideIntsRange(t *testing.T) {
	at := parse.Position{File: "main.qml", Line: 4, Column: 2}
	for _, raw := range []string{"1e300", "-1e300", "9.3e18", "-9.3e18"} {
		v := qml.SpecValue{Kind: qml.SpecValueNumber, Raw: raw, Pos: at}
		for name, read := range map[string]func(qml.SpecValue) (int, error){"truncOf": truncOf, "cellsOf": cellsOf} {
			n, err := read(v)
			if err == nil || !strings.Contains(err.Error(), raw) || !strings.Contains(err.Error(), at.String()) {
				t.Errorf("%s(%s) = %d, %v; want it refused, naming %s at %s", name, raw, n, err, raw, at)
			}
		}
	}
	if n, err := truncOf(qml.SpecValue{Kind: qml.SpecValueNumber, Raw: "9e18", Pos: at}); err != nil || n != 9000000000000000000 {
		t.Errorf("truncOf(9e18) = %d, %v; want it read, in range", n, err)
	}
}
