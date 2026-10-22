package decl

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// One enum engine serves three spellings, each as before. Tui's enums resolve `Tui.Horizontal` to
// "horizontal". A Type's Enum resolves `Editor.Rendered` to "Editor.Rendered", read back as the
// name, and refuses the unqualified "Rendered". A bare set (indentMarks) publishes no constants.
// Every refusal is written one way: "must be A, B or C, got …".
func TestOneEnumEngineKeepsEachSpelling(t *testing.T) {
	str := func(s string) qml.SpecValue { return strValue(s) }

	if c := orientations.constants(); len(c) != 2 || c["Tui.Horizontal"].Raw != "horizontal" || c["Tui.Vertical"].Raw != "vertical" {
		t.Errorf("Tui constants %v, want Tui.Horizontal → horizontal, Tui.Vertical → vertical", c)
	}
	if _, err := orientations.read(str("horizontal")); err != nil {
		t.Errorf("orientation \"horizontal\": %v", err)
	}
	if _, err := orientations.read(str("sideways")); err == nil || !strings.Contains(err.Error(), "must be Tui.Horizontal or Tui.Vertical") {
		t.Errorf("orientation \"sideways\": %v, want the Tui spelling", err)
	}

	if c := EditorViews.enum().constants(); c["Editor.Raw"].Raw != "Editor.Raw" || c["Editor.Rendered"].Raw != "Editor.Rendered" {
		t.Errorf("Editor constants %v, want each resolving to its qualified name", c)
	}
	if got, err := EditorViews.read(str("Editor.Rendered")); got != "Rendered" || err != nil {
		t.Errorf("view \"Editor.Rendered\" = %q, %v; want the name, Rendered", got, err)
	}
	if _, err := EditorViews.read(str("Rendered")); err == nil || !strings.Contains(err.Error(), "must be Editor.Raw or Editor.Rendered") {
		t.Errorf("view \"Rendered\": %v, want the unqualified name refused, choices in the one spelling", err)
	}
	if _, err := EditorViews.read(qml.SpecValue{Kind: qml.SpecValueBool, Raw: "true"}); err == nil || !strings.Contains(err.Error(), "must be written as a string") {
		t.Errorf("view true: %v, want refused as not a string", err)
	}

	if c := indentMarks.constants(); len(c) != 0 {
		t.Errorf("indentMarks publishes constants %v; a bare set has none", c)
	}
}
