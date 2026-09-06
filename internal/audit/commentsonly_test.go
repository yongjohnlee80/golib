package audit

import (
	"bytes"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// THE COMMENTS-ONLY CHECK.
//
// A comment migration must not touch code. That sounds like something you can
// simply be careful about; it is not. Rewriting comments at scale is done with
// a tool, and a tool that operates on a slightly wider unit than intended
// edits code without anyone noticing:
//
//   - operating on whole LINES rather than the text after "//" rewrites code
//     that happens to contain the pattern;
//   - "tidying" empty parentheses turns backend.Events() into backend.Events —
//     it compiles, and it reads almost right;
//   - operating LINE BY LINE on a construct that wraps across lines leaves
//     half of it behind, and a balance check still passes because the
//     remainder is balanced.
//
// All three happened during the tui migration. The first was caught by the
// compiler, the second by a parenthesis count someone thought to run, the
// third only by reading the output — and it still shipped three fragments
// ("concern.2)", "log.8.2)") that a reviewer found.
//
// This check catches all three in one pass, and it does so by asking the only
// question that matters: STRIP EVERY COMMENT FROM BOTH REVISIONS, and the
// remaining code must be BYTE-IDENTICAL. If it is not, the tool touched code,
// whatever it was aiming at.
//
// It is a MIGRATION TOOL, not a guard, so it is opt-in and skips by default —
// it needs a base revision to compare against and there is no meaningful
// default for that:
//
//	COMMENTS_ONLY_BASE=origin/main go test ./internal/audit/ -run TestCommentsOnlyChange -v
//
// Run it before pushing any comment-migration change, in any repository. The
// autodb rungs face the same three defect classes and can use it unchanged.
func TestCommentsOnlyChange(t *testing.T) {
	base := os.Getenv("COMMENTS_ONLY_BASE")
	if base == "" {
		t.Skip("set COMMENTS_ONLY_BASE=<rev> to compare the working tree against a revision")
	}

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}

	// codeOf renders src with every comment removed, so only code remains.
	codeOf := func(src []byte) (string, error) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", src, 0) // no ParseComments: they are dropped
		if err != nil {
			return "", err
		}
		f.Comments = nil
		var b bytes.Buffer
		if err := printer.Fprint(&b, fset, f); err != nil {
			return "", err
		}
		return b.String(), nil
	}

	// Every .go file that differs between base and the working tree.
	out, err := exec.Command("git", "-C", root, "diff", "--name-only", base, "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git diff --name-only %s: %v", base, err)
	}
	var changed []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(l) != "" {
			changed = append(changed, strings.TrimSpace(l))
		}
	}
	if len(changed) == 0 {
		t.Fatalf("no .go files differ from %s; there is nothing to check, which is not a pass", base)
	}
	sort.Strings(changed)

	var touched []string
	for _, rel := range changed {
		before, err := exec.Command("git", "-C", root, "show", base+":"+rel).Output()
		if err != nil {
			continue // added file: nothing to compare against
		}
		after, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue // deleted file
		}
		cb, errB := codeOf(before)
		ca, errA := codeOf(after)
		if errB != nil || errA != nil {
			t.Errorf("%s: could not render code without comments (%v / %v)", rel, errB, errA)
			continue
		}
		if cb != ca {
			touched = append(touched, rel)
		}
	}

	if len(touched) > 0 {
		t.Errorf("%d file(s) had their CODE changed by a comments-only change:\n  %s\n"+
			"Strip every comment from both revisions and the code must be byte-identical. "+
			"It is not, so the tool edited code — most likely by operating on whole lines, "+
			"on a construct that wraps across lines, or by 'tidying' punctuation that "+
			"belonged to an identifier.", len(touched), strings.Join(touched, "\n  "))
	}
	t.Logf("%d changed .go file(s); code identical to %s in all of them", len(changed), base)
}
