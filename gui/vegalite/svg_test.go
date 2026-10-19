package vegalite

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// A chart laid out paints on an SVG canvas as a well-formed drawing: a path per bar, and every
// label it lays out as a <text> saying it.
func TestAChartPaintsAsSVG(t *testing.T) {
	l := mustLay(t, barSpec, 600)
	c := gui.NewSVGCanvas(l.Size(), nil)
	l.Paint(c)
	out := c.Bytes()
	d := xml.NewDecoder(bytes.NewReader(out))
	for {
		_, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("not well-formed (%v):\n%s", err, out)
		}
	}
	s := string(out)
	if n := strings.Count(s, "<path "); n < len(l.bars()) {
		t.Errorf("%d paths for %d bars", n, len(l.bars()))
	}
	for _, label := range l.texts() {
		if !strings.Contains(s, ">"+label+"</text>") {
			t.Errorf("label %q is not written:\n%s", label, s)
		}
	}
}
