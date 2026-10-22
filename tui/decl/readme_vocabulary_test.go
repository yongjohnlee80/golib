package decl_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// The README's vocabulary table is what a document is written against, so it keeps up with the
// adapter: every standard type has a row, and every name its Contract declares (constructor-only
// properties, setters, readable properties, signals, methods) is written in that row's column, in
// backticks.
// Prose around the names is free; a name missing from its column is a type the README fell behind.
func TestTheREADMEsVocabularyNamesEveryContract(t *testing.T) {
	src, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	rows := vocabularyRows(t, string(src))
	_, a := mount(t, listScreen, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })
	for _, typ := range a.TypeNames() {
		c, standard := tuidecl.StandardContract(typ)
		if !standard {
			continue // an engine type (Repeater, DelegateChooser): expanded before build, no Contract
		}
		row, ok := rows[typ]
		if !ok {
			t.Errorf("%s has no row in the README's vocabulary table", typ)
			continue
		}
		// a property that is a ctor property and a setter is a runtime one (ClassifyProperty asks the
		// setters first), written in the setters column; only a constructor-only one is a ctor's
		ctorOnly := make([]string, 0, len(c.Ctor))
		for _, p := range c.Ctor {
			if !slices.Contains(c.Setters, p) {
				ctorOnly = append(ctorOnly, p)
			}
		}
		signals := make([]string, 0, len(c.Signals))
		for s := range c.Signals {
			signals = append(signals, s)
		}
		slices.Sort(signals)
		for _, col := range []struct {
			name  string
			i     int
			names []string
		}{
			{"ctor properties", 0, ctorOnly},
			{"setters", 1, c.Setters},
			{"read in a handler", 2, c.Getters},
			{"signals", 3, signals},
			{"methods", 4, c.Methods},
		} {
			for _, n := range col.names {
				if !row[col.i][n] {
					t.Errorf("%s: %q is not in its README row's %s column", typ, n, col.name)
				}
			}
		}
	}
}

// codeSpan is a backticked span in a table cell.
var codeSpan = regexp.MustCompile("`([^`]+)`")

// vocabularyRows are the vocabulary table's rows by type: for each of the five columns after the
// type, the names written there in backticks, each cut at its first "(" (open() is open,
// accepted(selectedFolder) is accepted). A type with two rows has the union.
func vocabularyRows(t *testing.T, readme string) map[string][5]map[string]bool {
	t.Helper()
	start := strings.Index(readme, "| Type | ctor properties | setters | read in a handler | signals | methods |")
	if start < 0 {
		t.Fatal("the README has no vocabulary table")
	}
	rows := map[string][5]map[string]bool{}
	for _, line := range strings.Split(readme[start:], "\n")[2:] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(line, "|")
		if len(cells) < 8 {
			t.Fatalf("a vocabulary row with %d cells: %q", len(cells), line)
		}
		var named [5]map[string]bool
		for i := range named {
			named[i] = map[string]bool{}
			for _, m := range codeSpan.FindAllStringSubmatch(cells[i+2], -1) {
				name, _, _ := strings.Cut(m[1], "(")
				named[i][name] = true
			}
		}
		for _, m := range codeSpan.FindAllStringSubmatch(cells[1], -1) {
			typ := m[1]
			row, seen := rows[typ]
			if !seen {
				row = named
			} else {
				for i := range row {
					for n := range named[i] {
						row[i][n] = true
					}
				}
			}
			rows[typ] = row
		}
	}
	return rows
}

// The vocabulary table names nothing a type lacks: a name dropped from a type leaves its row too.
// A column's names here are its backticked identifiers outside parentheses; the parentheses are what
// a name means, and may mention others (an enum's values, a source, another property). Each is
// judged by the adapter's own hooks, which know the universal properties as well as the type's
// (MenuItem's visible). The signals column is not judged: a type declares only its signals with
// parameters; a builder asks for one without (b.Emitter) only when it builds.
func TestTheREADMEsVocabularyNamesNothingATypeLacks(t *testing.T) {
	src, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, a := mount(t, listScreen, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })
	for typ, cols := range topLevelNames(t, string(src)) {
		if _, standard := tuidecl.StandardContract(typ); !standard {
			continue
		}
		has := [5]func(string) bool{
			func(n string) bool { return a.ClassifyProperty(typ, n) != decl.PropUnknown },
			func(n string) bool { return a.ClassifyProperty(typ, n) == decl.PropRuntime },
			func(n string) bool { return slices.Contains(a.ReadablesOf(typ), n) },
			nil,
			func(n string) bool { return slices.Contains(a.MethodsOf(typ), n) },
		}
		names := [5]string{"ctor properties", "setters", "read in a handler", "signals", "methods"}
		for i, col := range cols {
			if has[i] == nil {
				continue
			}
			for _, n := range col {
				if !has[i](n) {
					t.Errorf("%s: %q is in its README row's %s column, and the type has no such", typ, n, names[i])
				}
			}
		}
	}
}

// identifier is a name a column lists: a property, signal or method, dotted for a grouped one.
var identifier = regexp.MustCompile(`^[a-z][A-Za-z0-9]*(\.[a-z][A-Za-z0-9]*)?$`)

// topLevelNames are the vocabulary table's rows by type: for each column after the type, the
// backticked identifiers outside parentheses, each cut at its first "(".
func topLevelNames(t *testing.T, readme string) map[string][5][]string {
	t.Helper()
	start := strings.Index(readme, "| Type | ctor properties | setters | read in a handler | signals | methods |")
	if start < 0 {
		t.Fatal("the README has no vocabulary table")
	}
	out := map[string][5][]string{}
	for _, line := range strings.Split(readme[start:], "\n")[2:] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(line, "|")
		var cols [5][]string
		for i := range cols {
			depth, inCode, span := 0, false, ""
			for _, r := range cells[i+2] {
				switch {
				case r == '`' && inCode:
					inCode = false
					name, _, _ := strings.Cut(span, "(")
					if depth == 0 && identifier.MatchString(name) {
						cols[i] = append(cols[i], name)
					}
				case r == '`':
					inCode, span = true, ""
				case inCode:
					span += string(r)
				case r == '(':
					depth++
				case r == ')' && depth > 0:
					depth--
				}
			}
		}
		for _, m := range codeSpan.FindAllStringSubmatch(cells[1], -1) {
			out[m[1]] = cols
		}
	}
	return out
}
