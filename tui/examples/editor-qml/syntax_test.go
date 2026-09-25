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
