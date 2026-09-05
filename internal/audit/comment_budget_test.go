package audit

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// THE COMMENT BUDGET.
//
// A comment that explains WHY must be readable on its own. It has to stay true
// if every design record, review thread and pull request vanished, because the
// next engineer reading this file has none of them open. A comment whose
// content is a pointer — a design-record number, a section anchor, a review
// round, a reviewer's name — explains nothing to that reader: it names a
// document they cannot see, and it was only ever legible to its author on the
// day they wrote it.
//
// A pointer is still allowed, but as the LAST line and only to something the
// reader of this repository can actually open: another file here, or public
// documentation on the web. Never a private document.
//
// This test does not judge prose. It counts the mechanical patterns from the
// list above, per file, and holds each file to a recorded number that MAY ONLY
// FALL. A file at zero is frozen there and can never regain one. That is the
// whole ratchet: packages are migrated one at a time, and nothing that has
// been cleaned can quietly get dirty again.
//
// WHAT THIS BUDGET DOES NOT CATCH, stated plainly so nobody trusts it further
// than it goes: the count is per file, so deleting one pointer and adding
// another in the SAME file nets to zero and passes. Identity-level tracking was
// considered and rejected as too churn-prone for a thousand sites whose text is
// being rewritten anyway. The protection that matters is the zero-freeze, and
// the destination is a budget file with nothing in it.

const budgetFile = "testdata/comment_budget.txt"

// minWalkedFiles is a vacuity guard. Every assertion here is driven by a
// directory walk, and a walk that finds nothing reports a clean repository.
const minWalkedFiles = 100

// pointerPatterns are the shapes rule 9 names as non-compliant CONTENT. Each
// is deliberately narrow: this test's job is to be right about what it flags,
// not to catch every possible phrasing, because a false positive here costs a
// reader's trust in the whole instrument.
var pointerPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	// A design-record number, in any of the spellings this repo has used.
	{"design-record-number", regexp.MustCompile(`(?i)\bADR[-\s]?\d{3,4}\b`)},
	{"design-record-slug", regexp.MustCompile(`(?i)\bgolib-(dao|tui)[-\s]?\d{4}\b`)},
	// A section anchor into a document the reader does not have.
	{"section-anchor", regexp.MustCompile(`§`)},
	// Review-round shorthand and the reviewer's name as the authority.
	{"review-round", regexp.MustCompile(`(?i)\bMF\d+\b|\bnit #?\d+\b|\br\d+ (must-fix|nit)\b`)},
	// A reviewer named as the authority. This list is CLOSED and the fleet
	// grows, so extend it rather than assume it is complete.
	{"reviewer-as-authority", regexp.MustCompile(`(?i)\b(lector|gold-man|juliet|kimmy-vision|ultron-prime|white-vision|wanda-maximoff|zen|jarvis)\b`)},
	// An amendment number carries no rule with it.
	{"amendment-number", regexp.MustCompile(`(?i)\bamendment \d+\b`)},
	// A pull request or issue number used as the explanation.
	{"pull-request-number", regexp.MustCompile(`\bPR #\d+\b`)},
	// A coordinate into a specification table.
	{"matrix-coordinate", regexp.MustCompile(`\brow \d+:`)},
	// A bare review coordinate. These hide in parentheses with nothing else
	// around them — "(r3)" names a review round nobody outside it can read.
	{"review-coordinate", regexp.MustCompile(`\(r\d+\)|\br\d+ (review|round|fix)\b`)},
	// An acceptance-criterion number. "criterion 10" is a line in a document
	// the reader does not have, and it is frequently the ONLY pointer on its
	// line, which is what made it invisible to the first version of this test.
	{"criterion-number", regexp.MustCompile(`(?i)\bcriteri(on|a) \d+\b`)},
	// A document revision. Every use of this in the repository is a coordinate
	// into a design record ("rev 3 put it in the domain"), not a protocol or
	// format version.
	{"document-revision", regexp.MustCompile(`\brev \d+\b`)},
	// A review instruction, with or without the round that issued it. "must-fix
	// from the 2026-06-23 review" names no round and no reviewer, so none of
	// the shapes above sees it.
	{"review-must-fix", regexp.MustCompile(`(?i)\bmust-fix(es)?\b`)},
	// A citation of a knowledge-base page as the authority. This is the class
	// the rule names most explicitly, and it was the one with no detector at
	// all — which left a file carrying one reported CLEAN.
	//
	// "KB" is matched only when followed by the name of a document, or as
	// "(KB" at end of line, where the citation wraps onto the next one. Bare
	// KB at end of line would catch a size such as "a buffer of 64 KB".
	{"kb-document-citation", regexp.MustCompile(`\bKB (convention|policy|playbook|synthesis|ADR|[a-z]+(-[a-z]+)+)\b|\(KB$`)},
	// The continuation line of such a citation, and the requirement numbers
	// inside it: "security-core-hardening R4/R7".
	{"kb-requirement-number", regexp.MustCompile(`\b[a-z]+(-[a-z]+)+ R\d+\b`)},
}

// commentViolations returns, per repo-relative file path, how many COMMENT
// LINES carry at least one pointer pattern. A line is counted once however
// many patterns it matches, so the number is "lines to rewrite".
func commentViolations(t *testing.T) (map[string]int, int) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}

	counts := map[string]int{}
	walked := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		// Tests are out of scope for this pass. The migration plan is about
		// the comments a reader of the LIBRARY meets; test comments are a
		// separate, larger job and pretending otherwise would freeze a number
		// nobody intends to drive down.
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		walked++

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			// A file that cannot be parsed cannot be counted, and a file that
			// is not counted is not held to any budget. Skipping it silently
			// would make "unparseable" a way out of this test — including for
			// a file that stops parsing by accident. Fail, and name the path.
			return fmt.Errorf("parse %s: %w", path, perr)
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		for _, group := range f.Comments {
			for _, c := range group.List {
				// A directive is an instruction to a tool, not documentation.
				if strings.HasPrefix(c.Text, "//go:") {
					continue
				}
				for i, line := range strings.Split(c.Text, "\n") {
					for _, p := range pointerPatterns {
						if p.re.MatchString(line) {
							counts[rel]++
							_ = i
							break
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repo: %v", err)
	}
	if walked < minWalkedFiles {
		t.Fatalf("walked only %d source files, expected at least %d; the walk is "+
			"broken and this budget would report a clean repository",
			walked, minWalkedFiles)
	}
	return counts, walked
}

func readBudget(t *testing.T) map[string]int {
	t.Helper()
	raw, err := os.ReadFile(budgetFile)
	if err != nil {
		t.Fatalf("read %s: %v", budgetFile, err)
	}
	out := map[string]int{}
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("%s:%d: want '<count> <path>', got %q", budgetFile, i+1, line)
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatalf("%s:%d: %q is not a count", budgetFile, i+1, fields[0])
		}
		if n <= 0 {
			t.Fatalf("%s:%d: a budget of %d is not a budget — delete the line, "+
				"which freezes the file at zero", budgetFile, i+1, n)
		}
		out[fields[1]] = n
	}
	return out
}

func TestCommentBudget(t *testing.T) {
	actual, walked := commentViolations(t)
	budget := readBudget(t)

	var overBudget, unlisted, stale []string

	for file, got := range actual {
		want, listed := budget[file]
		if !listed {
			// Not in the budget means frozen at zero: either a file that was
			// migrated, or one written after the rule took effect.
			unlisted = append(unlisted, fmt.Sprintf("%s: %d (budgeted 0)", file, got))
			continue
		}
		if got > want {
			overBudget = append(overBudget, fmt.Sprintf("%s: %d, budgeted %d", file, got, want))
		}
		if got < want {
			// The budget is EXACT, not a ceiling. Lowering it in the same
			// change is what makes the ratchet a ratchet: an improvement that
			// is not recorded can be silently undone later.
			stale = append(stale, fmt.Sprintf("%s: %d, budgeted %d — lower it to %d", file, got, want, got))
		}
	}
	for file, want := range budget {
		if _, present := actual[file]; !present {
			stale = append(stale, fmt.Sprintf("%s: file is clean or gone, budgeted %d — delete the line", file, want))
		}
	}

	sort.Strings(overBudget)
	sort.Strings(unlisted)
	sort.Strings(stale)

	if len(overBudget) > 0 {
		t.Errorf("%d file(s) gained comment pointers:\n  %s\n"+
			"Lead with the requirement in plain language: what must hold, and what "+
			"goes wrong if it does not. A pointer may follow on its own REFERENCE: "+
			"line, but only to something a reader of this repository can open — "+
			"another file here, or public documentation.",
			len(overBudget), strings.Join(overBudget, "\n  "))
	}
	if len(unlisted) > 0 {
		t.Errorf("%d file(s) are frozen at zero and are no longer clean:\n  %s\n"+
			"A file leaves this budget once, and does not come back.",
			len(unlisted), strings.Join(unlisted, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("%d budget line(s) no longer match:\n  %s\n"+
			"The budget is exact, not a ceiling — record the improvement so it cannot be undone.",
			len(stale), strings.Join(stale, "\n  "))
	}

	total := 0
	for _, n := range actual {
		total += n
	}
	t.Logf("%d comment lines carry a pointer, across %d of %d files walked",
		total, len(actual), walked)
}
