package yaml_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/yaml"
)

const suiteDir = "testdata/yaml-test-suite"

// suiteTest is one yaml-test-suite test: a directory with in.yaml, or a numbered subtest of one.
type suiteTest struct {
	id, dir string
}

func suiteTests(t *testing.T) []suiteTest {
	t.Helper()
	entries, err := os.ReadDir(suiteDir)
	if err != nil {
		t.Fatal(err)
	}
	var out []suiteTest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(suiteDir, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "in.yaml")); err == nil {
			out = append(out, suiteTest{e.Name(), dir})
			continue
		}
		subs, _ := os.ReadDir(dir)
		for _, s := range subs {
			if s.IsDir() {
				out = append(out, suiteTest{e.Name() + "/" + s.Name(), filepath.Join(dir, s.Name())})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// eventNotation writes events as the suite's test.event states them.
func eventNotation(evs []yaml.Event) string {
	var b strings.Builder
	props := func(ev yaml.Event) {
		if ev.Anchor != "" {
			b.WriteString(" &" + ev.Anchor)
		}
		if ev.Tag != "" {
			b.WriteString(" <" + ev.Tag + ">")
		}
	}
	for _, ev := range evs {
		switch ev.Kind {
		case yaml.EventStreamStart:
			b.WriteString("+STR")
		case yaml.EventStreamEnd:
			b.WriteString("-STR")
		case yaml.EventDocumentStart:
			b.WriteString("+DOC")
			if ev.Explicit {
				b.WriteString(" ---")
			}
		case yaml.EventDocumentEnd:
			b.WriteString("-DOC")
			if ev.Explicit {
				b.WriteString(" ...")
			}
		case yaml.EventMappingStart:
			b.WriteString("+MAP")
			if ev.Style == yaml.StyleFlow {
				b.WriteString(" {}")
			}
			props(ev)
		case yaml.EventMappingEnd:
			b.WriteString("-MAP")
		case yaml.EventSequenceStart:
			b.WriteString("+SEQ")
			if ev.Style == yaml.StyleFlow {
				b.WriteString(" []")
			}
			props(ev)
		case yaml.EventSequenceEnd:
			b.WriteString("-SEQ")
		case yaml.EventAlias:
			b.WriteString("=ALI *" + ev.Alias)
		case yaml.EventScalar:
			b.WriteString("=VAL")
			props(ev)
			b.WriteString(" " + map[yaml.Style]string{yaml.StylePlain: ":", yaml.StyleSingleQuoted: "'",
				yaml.StyleDoubleQuoted: `"`, yaml.StyleLiteral: "|", yaml.StyleFolded: ">"}[ev.Style])
			b.WriteString(strings.NewReplacer(`\`, `\\`, "\b", `\b`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(string(ev.Value)))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestYAMLTestSuite runs every test of the vendored yaml-test-suite: a valid input's events must be
// test.event exactly, and an input marked with an error file must fail to parse.
func TestYAMLTestSuite(t *testing.T) {
	tests := suiteTests(t)
	passed, skipped := 0, 0
	for _, st := range tests {
		in, err := os.ReadFile(filepath.Join(st.dir, "in.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		title, _ := os.ReadFile(filepath.Join(st.dir, "==="))
		_, statErr := os.Stat(filepath.Join(st.dir, "error"))
		wantError := statErr == nil
		if why, ok := skip[st.id]; ok {
			skipped++
			t.Logf("SKIP %s (%s): %s", st.id, strings.TrimSpace(string(title)), why)
			continue
		}
		var evs []yaml.Event
		var perr error
		for ev, err := range yaml.Events(in) {
			if err != nil {
				perr = err
				break
			}
			evs = append(evs, ev)
		}
		_, parseErr := yaml.Parse(in)
		switch {
		case wantError:
			if parseErr == nil {
				t.Errorf("%s (%s): parsed, want an error\n%s", st.id, strings.TrimSpace(string(title)), in)
				continue
			}
		default:
			if perr != nil || parseErr != nil {
				t.Errorf("%s (%s): %v / %v\n%s", st.id, strings.TrimSpace(string(title)), perr, parseErr, in)
				continue
			}
			want, _ := os.ReadFile(filepath.Join(st.dir, "test.event"))
			if got := eventNotation(evs); got != string(want) {
				t.Errorf("%s (%s): events differ\n--- input\n%s--- got\n%s--- want\n%s", st.id, strings.TrimSpace(string(title)), in, got, want)
				continue
			}
		}
		passed++
	}
	t.Logf("yaml-test-suite data-2022-01-17: %d tests, %d passed, %d skipped", len(tests), passed, skipped)
}

// TestNoSkips is the merge gate: every suite test passes, none is skipped.
func TestNoSkips(t *testing.T) {
	for id, why := range skip {
		t.Errorf("yaml-test-suite %s is skipped (%s): the full YAML 1.2 claim needs it to pass", id, why)
	}
}
