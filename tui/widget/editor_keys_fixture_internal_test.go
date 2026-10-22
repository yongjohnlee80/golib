package widget

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var updateKeyFixtures = flag.Bool("update-key-fixtures", false, "rewrite testdata/editorkeys from the current tables")

// The editor's key tables as they were before the key system moved to command declarations (ADR
// 1792624213): the three legacy constructors' maps, every editor command in order with its scope and
// ordered keys, and every Action's name and value. The declarations must reproduce the maps
// exactly; the commands may only gain the ids the ADR lists; no existing Action may change value.
func TestTheEditorsKeyTablesMatchTheirFixtures(t *testing.T) {
	check := func(name, got string) {
		t.Helper()
		path := filepath.Join("testdata", "editorkeys", name)
		if *updateKeyFixtures {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s differs from its fixture:\n%s", name, firstDiff(string(want), got))
		}
	}
	for name, km := range map[string]Keymap{"vim.txt": VimKeymap(), "nano.txt": NanoKeymap(), "standard.txt": StandardKeymap()} {
		check(name, keymapFixture(km))
	}
	var cmds strings.Builder
	for _, c := range EditorCommands() {
		keys := make([]string, len(c.Keys))
		for i, k := range c.Keys {
			keys[i] = k.String()
		}
		fmt.Fprintf(&cmds, "%s\t%s\t%s\n", c.ID, c.Scope, strings.Join(keys, " "))
	}
	check("commands.txt", cmds.String())
	var acts strings.Builder
	for a := Action(0); a < actMax; a++ {
		fmt.Fprintf(&acts, "%d\t%s\n", int(a), a)
	}
	check("actions.txt", acts.String())
}

// keymapFixture is a Keymap as sorted lines: each chord's mode, code and modifiers, then its action.
func keymapFixture(km Keymap) string {
	lines := make([]string, 0, len(km))
	for kc, act := range km {
		lines = append(lines, fmt.Sprintf("%d %d ctrl=%t alt=%t super=%t shift=%t\t%s",
			int(kc.Mode), kc.Code, kc.Ctrl, kc.Alt, kc.Super, kc.Shift, act))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// firstDiff is the first line where want and got differ, for a readable failure.
func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < max(len(w), len(g)); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return fmt.Sprintf("line %d:\n  want %q\n  got  %q", i+1, wl, gl)
		}
	}
	return "(lengths differ)"
}
