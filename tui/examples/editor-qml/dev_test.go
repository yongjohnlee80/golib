package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// dev_test.go runs the editor as `editor-qml -dev dir` does: its QML read from
// a directory, and followed. An edit to editor.qml reaches the running screen,
// and what was typed survives it.

// devCopy writes the QML this program embeds into a directory of its own.
func devCopy(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "editor.qml"), layout, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, src := range []fs.FS{themeFiles, dialogFiles} {
		err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				if d != nil && d.IsDir() && p != "." {
					return os.MkdirAll(filepath.Join(dir, p), 0o755)
				}
				return err
			}
			b, err := fs.ReadFile(src, p)
			if err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, p), b, 0o644)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDevModeFollowsEditsAndKeepsWhatWasTyped(t *testing.T) {
	dir := devCopy(t)
	r := startOpts(t, Options{Dev: dir, Now: fixedNow, Tick: time.Hour}, 80, 14)
	r.key(t, runeKey('i'))
	for _, ch := range "hello" {
		r.key(t, runeKey(ch))
	}
	r.waitFor(t, "the typed text", func(s string) bool { return strings.Contains(s, "hello") })

	src, err := os.ReadFile(filepath.Join(dir, "editor.qml"))
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(src), `title: "&File"`, `title: "&Fyle"`, 1)
	if edited == string(src) {
		t.Fatal("the fixture no longer has the File menu this test edits")
	}
	if err := os.WriteFile(filepath.Join(dir, "editor.qml"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	r.waitFor(t, "the edited menu", func(s string) bool { return strings.Contains(s, "Fyle") })
	if !strings.Contains(r.screen(), "hello") {
		t.Errorf("the reload lost what was typed:\n%s", r.screen())
	}
}

// TestDevModeShowsARefusedEditInTheStatusLine: the screen stays, and says why.
func TestDevModeShowsARefusedEditInTheStatusLine(t *testing.T) {
	dir := devCopy(t)
	r := startOpts(t, Options{Dev: dir, Now: fixedNow, Tick: time.Hour}, 80, 14)
	src, _ := os.ReadFile(filepath.Join(dir, "editor.qml"))
	edited := strings.Replace(string(src), `title: "&File"`, `titel: "&File"`, 1)
	if err := os.WriteFile(filepath.Join(dir, "editor.qml"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	r.waitFor(t, "the refusal in the status line", func(s string) bool { return strings.Contains(s, "titel") })
	if !strings.Contains(r.screen(), "File") {
		t.Errorf("a refused edit lost the screen:\n%s", r.screen())
	}
}

// TestTheThemeMenuUnderDevLeavesTheFileAlone: -dev switches theme from the
// file as it stands, in memory; editor.qml on disk keeps its own import.
func TestTheThemeMenuUnderDevLeavesTheFileAlone(t *testing.T) {
	dir := devCopy(t)
	r := startOpts(t, Options{Dev: dir, Now: fixedNow, Tick: time.Hour}, 80, 14)
	r.pickTheme(t, "Mono")
	f := r.labelAt(t, 0, "File")
	r.expect(t, []look{{"mono's File access key", f, 0, ansi(0), ansi(7)}})
	src, err := os.ReadFile(filepath.Join(dir, "editor.qml"))
	if err != nil {
		t.Fatal(err)
	}
	if themeOf(src) != "retro" {
		t.Errorf("the menu rewrote editor.qml on disk: it imports %q", themeOf(src))
	}
}
