package xmlpos

import (
	"encoding/xml"
	"strings"
	"testing"
)

// Line counts from 1 and follows the decoder: the third element starts on line 3.
func TestLineFollowsTheDecoder(t *testing.T) {
	d := xml.NewDecoder(strings.NewReader("<a>\n<b/>\n<c/>\n</a>"))
	if got := Line(d); got != 1 {
		t.Fatalf("before reading: line %d, want 1", got)
	}
	for {
		tok, err := d.Token()
		if err != nil {
			t.Fatalf("no <c>: %v", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "c" {
			break
		}
	}
	if got := Line(d); got != 3 {
		t.Fatalf("after <c/>: line %d, want 3", got)
	}
}
