package themes

import (
	"io/fs"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestValidateBuiltInsAndEveryRequiredRole(t *testing.T) {
	for _, name := range Names() {
		src, err := fs.ReadFile(FS(), name+".qml")
		if err != nil {
			t.Fatal(err)
		}
		v, ds := Validate(name+".qml", src)
		if len(ds) != 0 || len(v) != len(Schema()) {
			t.Fatalf("%s: %v (%d roles)", name, ds, len(v))
		}
	}
	src, _ := fs.ReadFile(FS(), "sepia.qml")
	broken := strings.Replace(string(src), `backdrop: "#d4c4a0"`, `unknown: "bad"`, 1)
	broken = strings.Replace(broken, `window: "#dacca9"`, `window: "not-a-colour"`, 1)
	v, ds := Validate("custom.qml", []byte(broken))
	if v != nil {
		t.Fatal("invalid theme returned usable values")
	}
	for _, key := range []string{"app.backdrop", "app.unknown", "app.window"} {
		found := false
		for _, d := range ds {
			if d.Key == key {
				found = true
				if d.Pos.File != "custom.qml" || d.Pos.Line < 1 {
					t.Errorf("no position: %+v", d)
				}
				if text := d.Error(); !strings.Contains(text, "custom.qml:") || !strings.Contains(text, key) {
					t.Errorf("diagnostic does not name file/key: %s", text)
				}
			}
		}
		if !found {
			t.Errorf("missing diagnosis %s: %v", key, ds)
		}
	}
}

func TestValidateRefusesExpressionsDuplicatesAndForeignStructure(t *testing.T) {
	src, err := fs.ReadFile(FS(), "sepia.qml")
	if err != nil {
		t.Fatal(err)
	}
	base := string(src)
	if v, ds := Validate("positive.qml", src); v == nil || len(ds) != 0 {
		t.Fatalf("positive control: %v", ds)
	}
	insert := func(member string) string { return strings.Replace(base, "Theme {", "Theme {\n    "+member, 1) }
	for _, c := range []struct{ name, src, key, message, marker string }{
		{"handler", insert(`onClicked: App.run()`), "Theme", "handlers are not allowed in a constant theme", "onClicked"},
		{"repeated", insert(`app.window: "#112233"`), "app.window", "key is repeated", `window: "#dacca9"`},
		{"import", "import tui 1.0\n" + base, "Theme", "imports are not allowed in a constant theme", "Theme {"},
		{"child", insert(`Text { text: "child" }`), "Text", "child objects are not allowed; use grouped colour properties", "Text {"},
		{"declaration", insert(`property string extra: "white"`), "extra", "property declarations are not allowed; write the colour key directly", "property string extra"},
		{"id", insert(`id: custom`), "id", "a constant theme has no id", "Theme {"},
		{"expression", strings.Replace(base, `window: "#dacca9"`, `window: App.color`, 1), "app.window", "colour must be a quoted string constant", "window: App.color"},
		{"number", strings.Replace(base, `window: "#dacca9"`, `window: 4`, 1), "app.window", "colour must be a quoted string constant", "window: 4"},
		{"root", strings.Replace(base, "Theme {", "Text {", 1), "Theme", "root must be Theme", "Text {"},
		{"unknown", insert(`app.unused: "white"`), "app.unused", "unknown colour key", "app.unused"},
		{"missing", strings.Replace(base, `backdrop: "#d4c4a0"`, "", 1), "app.backdrop", "required colour key is missing", "Theme {"},
	} {
		t.Run(c.name, func(t *testing.T) {
			v, ds := Validate("bad.qml", []byte(c.src))
			if v != nil || len(ds) != 1 {
				t.Fatalf("want only %s defect, got values=%v diagnostics=%v", c.name, v, ds)
			}
			offset := strings.Index(c.src, c.marker)
			if offset < 0 {
				t.Fatal("fixture marker absent")
			}
			before := c.src[:offset]
			line := strings.Count(before, "\n") + 1
			column := utf8.RuneCountInString(before[strings.LastIndex(before, "\n")+1:]) + 1
			d := ds[0]
			if d.Key != c.key || d.Message != c.message || d.Pos.File != "bad.qml" || d.Pos.Line != line || d.Pos.Column != column {
				t.Errorf("got %+v, want bad.qml:%d:%d %s: %s", d, line, column, c.key, c.message)
			}
			if text := d.Error(); !strings.Contains(text, "bad.qml:") || !strings.Contains(text, c.key+": "+c.message) {
				t.Errorf("unusable diagnostic: %s", text)
			}
		})
	}
}

func TestValidateReportsTheParsersSyntaxPosition(t *testing.T) {
	src, err := fs.ReadFile(FS(), "sepia.qml")
	if err != nil {
		t.Fatal(err)
	}
	base := string(src)
	v, ds := Validate("unfinished.qml", []byte(base[:strings.LastIndex(base, "}")]))
	// An unterminated node is reported at its opening brace, not at the type's first rune.
	before := base[:strings.Index(base, "Theme {")+len("Theme ")]
	line := strings.Count(before, "\n") + 1
	column := utf8.RuneCountInString(before[strings.LastIndex(before, "\n")+1:]) + 1
	if v != nil || len(ds) != 1 {
		t.Fatalf("syntax failure: %v %v", v, ds)
	}
	if d := ds[0]; d.Key != "Theme" || d.Pos.File != "unfinished.qml" || d.Pos.Line != line || d.Pos.Column != column || d.Message == "" {
		t.Errorf("unpositioned syntax failure: %+v", d)
	}
}
