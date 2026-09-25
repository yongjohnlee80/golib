package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// syntax_test.go: the editor highlights a QML file with its own highlighter
// (golib's), coloured by the imported theme — and leaves other files alone.

func writeFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const qmlFile = "import QtQuick 2.0\n// a comment\nRectangle { width: 10 }\n"

func TestAQMLFileIsHighlightedInTheThemesColours(t *testing.T) {
	r := start(t, writeFile(t, "view.qml", qmlFile))
	y := rowOf(r.rows(), "import QtQuick")
	if fg := r.cell(t, r.labelAt(t, y, "QtQuick"), y).FG; fg != rgb(0x55, 0xff, 0x55) {
		t.Errorf("the previewed module: fg %+v, want green", fg)
	}
	cy := rowOf(r.rows(), "// a comment")
	r.waitFor(t, "the keyword in white", func(string) bool {
		return r.cell(t, r.labelAt(t, y, "import"), y).FG == rgb(0xff, 0xff, 0xff)
	})
	r.expect(t, []look{
		{"the keyword", r.labelAt(t, y, "import"), y, rgb(0xff, 0xff, 0xff), cgaBlue},
		{"the module", r.labelAt(t, y, "QtQuick"), y, rgb(0x55, 0xff, 0x55), cgaBlue},
		{"the comment", r.labelAt(t, cy, "// a"), cy, cgaGrey, cgaBlue},
	})
}

func TestAPlainFileIsNotHighlighted(t *testing.T) {
	r := start(t, writeFile(t, "notes.txt", qmlFile))
	y := rowOf(r.rows(), "import QtQuick")
	r.expect(t, []look{{"the text", r.labelAt(t, y, "import"), y, cgaYellow, cgaBlue}})
}

// TestTheSyntaxColoursFollowTheThemeImport: under mono the keyword is bright.
func TestTheSyntaxColoursFollowTheThemeImport(t *testing.T) {
	r := startOpts(t, Options{Path: writeFile(t, "view.qml", qmlFile), Layout: withImport(t, "import editor.theme.mono 1.0"),
		Now: fixedNow, Tick: time.Hour}, 80, 14)
	y := rowOf(r.rows(), "import QtQuick")
	r.waitFor(t, "the keyword bright", func(string) bool {
		return r.cell(t, r.labelAt(t, y, "import"), y).FG == ansi(15)
	})
}

// TestTheOpenPreviewHighlightsAQMLFile: the Open dialog's preview highlights
// a file by its name, in the syntax colours it inherits from the Window — and
// leaves a plain file plain.
func TestTheOpenPreviewHighlightsAQMLFile(t *testing.T) {
	folderWith(t, map[string]string{"view.qml": qmlFile, "zz.txt": qmlFile})
	r := startSized(t, "", 100, 24)
	r.openOpen(t)
	r.key(t, down) // view.qml
	r.shows(t, "import QtQuick")
	y := rowOf(r.rows(), "import QtQuick")
	r.waitFor(t, "the previewed keyword in white", func(string) bool {
		return r.cell(t, r.labelAt(t, y, "import"), y).FG == rgb(0xff, 0xff, 0xff)
	})
	cy := rowOf(r.rows(), "// a comment")
	if fg := r.cell(t, r.labelAt(t, cy, "// a"), cy).FG; fg != cgaGrey {
		t.Errorf("the previewed comment: fg %+v, want grey", fg)
	}

	r.key(t, down) // zz.txt: the same text, not a QML file
	// Plain, the keyword and the module wear the one text colour; highlighted
	// they differ (white and green).
	r.waitFor(t, "the plain file unhighlighted", func(string) bool {
		yy := rowOf(r.rows(), "import QtQuick")
		return yy >= 0 && r.cell(t, r.labelAt(t, yy, "import"), yy).FG == r.cell(t, r.labelAt(t, yy, "QtQuick"), yy).FG
	})
}

func TestSyntaxForPicksByExtension(t *testing.T) {
	for path, want := range map[string]string{
		"a/view.qml": "QML", "VIEW.QML": "QML", "lib.js": "JavaScript", "m.mjs": "JavaScript",
		"notes.txt": "", "": "", "qml": "",
	} {
		if got := syntaxFor(path); got != want {
			t.Errorf("syntaxFor(%q) = %q, want %q", path, got, want)
		}
	}
}
