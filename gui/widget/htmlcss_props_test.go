package widget

import (
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// Every registered property has a handler, and a property none registers leaves the style as it
// was, as a browser ignores an unknown property; a registered one sets it.
func TestTheCSSPropertiesAreRegistered(t *testing.T) {
	for name, set := range cssProps {
		if set == nil {
			t.Errorf("property %q is registered with no handler", name)
		}
	}
	parent := &computed{fontSize: 16}
	c := &computed{fontSize: 16}
	before := *c
	c.apply(cssDecl{prop: "scroll-snap-type", value: "x mandatory"}, parent, 16, gui.Size{W: 800, H: 600})
	if !reflect.DeepEqual(*c, before) {
		t.Errorf("an unknown property changed the style: %+v", *c)
	}
	c.apply(cssDecl{prop: "text-align", value: " CENTER "}, parent, 16, gui.Size{W: 800, H: 600})
	if c.align == before.align {
		t.Error("text-align: center, as written with spaces and capitals, did not set the alignment")
	}
}
