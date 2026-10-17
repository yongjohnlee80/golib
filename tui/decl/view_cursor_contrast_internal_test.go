package decl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Both cursor states use a readable foreground even when a theme supplied one that blends into
// its highlight. The same viewStyles rule is used by the terminal results table.
func TestViewCursorKeepsTextReadableInBothFocusStates(t *testing.T) {
	p := palette{
		roleText:                  style.RGB(235, 235, 235),
		roleHighlight:             style.RGB(220, 210, 245),
		roleHighlightedText:       style.RGB(240, 235, 250),
		roleInactiveHighlight:     style.RGB(55, 60, 70),
		roleInactiveHighlightText: style.RGB(65, 70, 80),
	}
	s := p.viewStyles()
	for name, st := range map[string]style.Style{"focused": s.CursorRow, "blurred": s.CursorBlurred} {
		fg, okFG := st.GetForeground()
		bg, okBG := st.GetBackground()
		ratio, ok := style.ContrastRatio(fg, bg)
		if !okFG || !okBG || !ok || ratio < 4.5 {
			t.Errorf("%s cursor foreground %v on %v has contrast %.2f", name, fg, bg, ratio)
		}
	}
}

// The ten shipped themes are read from their QML sources, not a duplicated palette fixture.
// This also rejects a theme that drops an active or inactive cursor role and falls back to
// reverse/faint, which would make the table unreadable against some document backgrounds.
func TestEveryBuiltInViewCursorIsReadableInBothFocusStates(t *testing.T) {
	entries, err := os.ReadDir("themes")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".qml") {
			continue
		}
		seen++
		name := strings.TrimSuffix(entry.Name(), ".qml")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("themes", entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			tree, err := (qml.QML{File: entry.Name()}).Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			values := make(map[string]string)
			for _, prop := range tree.Root.Props {
				values[prop.Name] = prop.Value.Raw
			}
			roles := []struct {
				name string
				role Role
			}{
				{"document.text", roleText},
				{"document.highlight", roleHighlight},
				{"document.highlightedText", roleHighlightedText},
				{"app.inactive.highlight", roleInactiveHighlight},
				{"app.inactive.highlightedText", roleInactiveHighlightText},
			}
			p := make(palette)
			for _, wanted := range roles {
				raw, ok := values[wanted.name]
				if !ok {
					t.Fatalf("missing %s", wanted.name)
				}
				col, err := style.ParseColor(raw)
				if err != nil {
					t.Fatalf("%s: %v", wanted.name, err)
				}
				p[wanted.role] = col
			}
			styles := p.viewStyles()
			for _, state := range []struct {
				name string
				st   style.Style
			}{{"focused", styles.CursorRow}, {"blurred", styles.CursorBlurred}} {
				fg, hasFG := state.st.GetForeground()
				bg, hasBG := state.st.GetBackground()
				ratio, measurable := style.ContrastRatio(fg, bg)
				if !hasFG || !hasBG || !measurable || ratio < 4.5 {
					t.Errorf("%s cursor %v on %v: contrast %.2f", state.name, fg, bg, ratio)
				}
			}
		})
	}
	if seen != 10 {
		t.Fatalf("checked %d built-in themes, want 10", seen)
	}
}
