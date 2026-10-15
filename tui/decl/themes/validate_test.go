package themes

import (
	"io/fs"
	"strings"
	"testing"
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
			}
		}
		if !found {
			t.Errorf("missing diagnosis %s: %v", key, ds)
		}
	}
}

func TestValidateRefusesExpressionsDuplicatesAndForeignStructure(t *testing.T) {
	for _, src := range []string{
		`Theme { app.window: "white"; app.window: "black" }`,
		`Theme { app.window: App.color }`,
		`Theme { property string other: "white" }`,
		`Theme { onClicked: App.run() }`,
		`Theme { Text {} }`,
		`import tui 1.0
Theme {}`,
		`Text {}`,
		`Theme { app { window: `,
	} {
		v, ds := Validate("bad.qml", []byte(src))
		if v != nil || len(ds) == 0 {
			t.Errorf("accepted %s: %v %v", src, v, ds)
		}
	}
}
