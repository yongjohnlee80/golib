package audit

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// THE PRIVATE-REFERENCE GUARD.
//
// golib is public. Decisions numbered by Unix time (an ADR with a ten-digit id) live in a
// private knowledge base, so a source comment or a README that cites one sends a public reader to
// a document they cannot open. Cite a decision golib's own docs/ hold, a public pull request
// (golib #434), or say what was decided, inline.

// privateADR is a decision id of the private knowledge base: ten digits of Unix time.
var privateADR = regexp.MustCompile(`(?i)\badr[ -]?1[0-9]{9}\b`)

// privateRefs is every cite of a private decision in the repository's Go and Markdown files.
func privateRefs(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name != "." && (strings.HasPrefix(name, ".") || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(p); ext != ".go" && ext != ".md" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if m := privateADR.FindString(line); m != "" {
				rel, _ := filepath.Rel(root, p)
				out = append(out, rel+":"+strconv.Itoa(i+1)+": "+m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}
	return out
}

func TestNoPrivateDecisionIsCited(t *testing.T) {
	// the positive control: the pattern matches the forms that were cited, and not golib's own ADRs
	// built from parts, so this file cites nothing itself
	for _, s := range []string{"(ADR " + "1791385086)", "ADR-" + "1792624213 adds", "adr " + "1792414684"} {
		if !privateADR.MatchString(s) {
			t.Fatalf("the pattern misses %q; the guard is not looking", s)
		}
	}
	for _, s := range []string{"ADR-0005 §2.4", "adr-0009-web-backend.md", "golib #434"} {
		if privateADR.MatchString(s) {
			t.Fatalf("the pattern matches golib's own %q", s)
		}
	}
	for _, at := range privateRefs(t, repoRoot(t)) {
		t.Errorf("%s cites a private decision: cite docs/, a public PR, or say what was decided", at)
	}
}
