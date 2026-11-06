package vegalite

import (
	"errors"
	"testing"

	pvl "github.com/yongjohnlee80/golib/parse/vegalite"
)

// A caller of Lay matches its refusals with this package's sentinels, as before parse/vegalite
// held them: they are parse/vegalite's own values, so errors.Is answers alike against either.
func TestLaysRefusalsMatchTheSentinelsOfBothPackages(t *testing.T) {
	_, err := lay(t, `{"data":{"values":[]},"transform":[{"filter":"true"}],"mark":"bar"}`, 400)
	if !errors.Is(err, ErrUnsupported) || !errors.Is(err, pvl.ErrUnsupported) {
		t.Errorf("a transform: %v; want ErrUnsupported, gui's and parse's alike", err)
	}
	var rows string
	for i := range pvl.MaxRows + 1 {
		if i > 0 {
			rows += ","
		}
		rows += `{"c":"a","v":1}`
	}
	_, err = lay(t, `{"data":{"values":[`+rows+`]},"mark":"bar","encoding":{"x":{"field":"c","type":"nominal"},"y":{"field":"v","type":"quantitative"}}}`, 400)
	if !errors.Is(err, ErrTooLarge) || !errors.Is(err, pvl.ErrTooLarge) {
		t.Errorf("%d rows: %v; want ErrTooLarge, gui's and parse's alike", pvl.MaxRows+1, err)
	}
}
