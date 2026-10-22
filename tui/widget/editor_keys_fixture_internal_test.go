package widget

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
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
	checkCommands(t, cmds.String())
	var acts strings.Builder
	for a := Action(0); a < actMax; a++ {
		fmt.Fprintf(&acts, "%d\t%s\n", int(a), a)
	}
	checkActions(t, acts.String())
}

// editingCommandIDs are the commands ADR 1792624213 adds: each keyset's Insert editing commands,
// less the actions Nano and Standard already bind in Insert (Home, End and the Page keys, early).
func editingCommandIDs() map[string]bool {
	ids := map[string]bool{}
	common := []string{"insertTab", "newline", "backspace", "deleteForward", "escape", "left", "right", "up", "down"}
	for _, ks := range []string{"vim", "nano", "standard"} {
		names := common
		if ks == "vim" {
			names = append(append([]string(nil), common...), "lineStart", "lineEnd", "pageUp", "pageDown")
		}
		for _, n := range names {
			ids["tui.editor."+ks+".insert."+n] = true
		}
	}
	return ids
}

// checkCommands holds EditorCommands to the fixture: every recorded line, unchanged and in order,
// with only the editing commands added among them.
func checkCommands(t *testing.T, got string) {
	t.Helper()
	path := filepath.Join("testdata", "editorkeys", "commands.txt")
	if *updateKeyFixtures {
		return // the recorded list is the one before the editing commands; it is never rewritten
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	old := strings.Split(strings.TrimSuffix(string(want), "\n"), "\n")
	added := map[string]bool{}
	i := 0
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if i < len(old) && line == old[i] {
			i++
			continue
		}
		added[strings.SplitN(line, "\t", 2)[0]] = true
	}
	if i < len(old) {
		t.Fatalf("commands.txt line %d is gone or changed: %q", i+1, old[i])
	}
	if wantAdded := editingCommandIDs(); !maps.Equal(added, wantAdded) {
		t.Fatalf("commands added: %v; want exactly the editing commands %v", slices.Sorted(maps.Keys(added)), slices.Sorted(maps.Keys(wantAdded)))
	}
}

// checkActions holds the Actions to the fixture: every recorded value and name unchanged, and only
// the editing actions appended after them.
func checkActions(t *testing.T, got string) {
	t.Helper()
	if *updateKeyFixtures {
		return // the recorded list is the one before the editing actions; it is never rewritten
	}
	want, err := os.ReadFile(filepath.Join("testdata", "editorkeys", "actions.txt"))
	if err != nil {
		t.Fatal(err)
	}
	old, rest, ok := strings.Cut(got, string(want))
	if !ok || old != "" {
		t.Fatalf("an existing Action changed its value or name:\n%s", firstDiff(string(want), got))
	}
	if rest != fmt.Sprintf("%d\tInsertTab\n%d\tNewline\n%d\tBackspace\n%d\tDeleteForward\n",
		ActInsertTab, ActNewline, ActBackspace, ActDeleteForward) {
		t.Fatalf("Actions appended after the recorded ones:\n%s", rest)
	}
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
