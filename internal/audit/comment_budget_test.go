package audit

import (
	"fmt"
	"go/ast"
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

// A scope is one half of the repository, each with its own budget that falls
// independently. They are separate because they were migrated separately and
// because the PRODUCTION budget is finished: it is empty, and every file in it
// is frozen at zero. Folding the unfinished test budget into it would put a
// number back into a file whose whole statement is that it has none.
type scope struct {
	name       string
	budgetFile string
	// wantTests selects which files the walk counts.
	wantTests bool
	// minWalked is a vacuity guard. Every assertion here is driven by a
	// directory walk, and a walk that finds nothing reports a clean
	// repository, so a broken walk must fail rather than pass.
	minWalked int
	// exempt maps a repo-relative path to the top-level identifier whose
	// DECLARATION in that file is the premise of its exemption.
	//
	// It belongs to the scope rather than the package because a liveness check
	// only means anything in the scope that actually walks the file: asking the
	// production scope whether a _test.go exemption is live reports "dead" for
	// a file it never looks at.
	exempt map[string]string
}

var (
	production = scope{
		name:       "production",
		budgetFile: "testdata/comment_budget.txt",
		wantTests:  false,
		minWalked:  100,
	}
	tests = scope{
		name:       "tests",
		budgetFile: "testdata/comment_budget_tests.txt",
		wantTests:  true,
		minWalked:  100,
		exempt: map[string]string{
			// Its pointer "hits" ARE the shapes the detectors match, quoted in
			// prose to document what each one catches. Rewriting them would
			// delete the documentation of the rule to satisfy the rule.
			"internal/audit/comment_budget_test.go": "pointerPatterns",
		},
	}
)

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

// declaresTopLevel reports whether file declares a top-level var, const, type
// or func with the given name.
//
// It parses rather than greps, and that is the whole point. The first version
// of this check used strings.Contains, which is VACUOUS for the one file that
// matters: comment_budget_test.go declares its own exemption, so the premise
// string appears in it no matter what the premise is. A mutation replacing
// "pointerPatterns" with a name nothing declares still PASSED, because the
// replacement was itself now written in the file being searched. A declaration
// name cannot be faked by a string literal.
func declaresTopLevel(t *testing.T, path, name string) bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Errorf("parsing %s for its exemption premise: %v", path, err)
		return false
	}
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Name.Name == name {
				return true
			}
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch sp := spec.(type) {
				case *ast.ValueSpec:
					for _, id := range sp.Names {
						if id.Name == name {
							return true
						}
					}
				case *ast.TypeSpec:
					if sp.Name.Name == name {
						return true
					}
				}
			}
		}
	}
	return false
}

// checkExemptions holds each of a scope's exemptions to its premise AND to
// being live.
//
// Exemptions are BY NAME, never by shape: a shape-based excuse ("any comment
// containing a quoted pointer") is one a future site can satisfy by accident,
// while one naming a file can only be satisfied by being that file. The reason
// lives with the guard, in the scope declaration above, because that is where a
// reader wonders why something was let through.
//
// The PREMISE is that the file still declares the named identifier — that it is
// still the file that DEFINES the detectors, rather than one that inherited the
// path. If that stops holding, the exemption excuses something it was never
// argued for.
//
// LIVENESS matters for a reason easy to miss: an exemption naming a site with
// nothing to exempt is not dormant, it is a standing pre-authorisation for
// whatever gets written under that name next. Silence becomes assent. So a
// clean exempted file FAILS here, and the fix is to delete the entry.
func checkExemptions(t *testing.T, sc scope, root string, hits map[string]int) {
	t.Helper()
	for rel, premise := range sc.exempt {
		abs := filepath.Join(root, rel)
		if _, err := os.Stat(abs); err != nil {
			t.Errorf("%s scope exempts %s, which is not there (%v); an exemption "+
				"naming a missing file excuses whatever is written under that "+
				"name next — delete the entry", sc.name, rel, err)
			continue
		}
		if !declaresTopLevel(t, abs, premise) {
			t.Errorf("%s scope exempts %s because it declares %s, and it no longer "+
				"does; the exemption is a bypass wearing a justification — "+
				"re-argue it or delete the entry", sc.name, rel, premise)
		}
		if hits[rel] == 0 {
			t.Errorf("%s scope exempts %s, which carries NO pointer comment, so the "+
				"exemption is dead. A dead exemption does not lapse — it waits, and "+
				"excuses the next thing written there. Delete the entry.", sc.name, rel)
		}
	}
}

// commentViolations returns, per repo-relative file path, how many COMMENT
// LINES carry at least one pointer pattern. A line is counted once however
// many patterns it matches, so the number is "lines to rewrite".
func commentViolations(t *testing.T, sc scope) (map[string]int, int) {
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
		// Each scope walks its own half. Production and test comments were
		// migrated as separate programs with separate budgets, so a file
		// counted here is never counted by the other scope.
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") != sc.wantTests {
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
	// Exemptions are checked against the RAW counts, then removed, so a dead or
	// unjustified exemption fails even though its file is excluded below.
	checkExemptions(t, sc, root, counts)
	for rel := range sc.exempt {
		delete(counts, rel)
	}

	if walked < sc.minWalked {
		t.Fatalf("%s scope: walked only %d source files, expected at least %d; the "+
			"walk is broken and this budget would report a clean repository",
			sc.name, walked, sc.minWalked)
	}
	return counts, walked
}

func readBudget(t *testing.T, sc scope) map[string]int {
	t.Helper()
	raw, err := os.ReadFile(sc.budgetFile)
	if err != nil {
		t.Fatalf("read %s: %v", sc.budgetFile, err)
	}
	out := map[string]int{}
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("%s:%d: want '<count> <path>', got %q", sc.budgetFile, i+1, line)
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatalf("%s:%d: %q is not a count", sc.budgetFile, i+1, fields[0])
		}
		if n <= 0 {
			t.Fatalf("%s:%d: a budget of %d is not a budget — delete the line, "+
				"which freezes the file at zero", sc.budgetFile, i+1, n)
		}
		out[fields[1]] = n
	}
	return out
}

func assertCommentBudget(t *testing.T, sc scope) {
	t.Helper()
	actual, walked := commentViolations(t, sc)
	budget := readBudget(t, sc)

	// The total is REPORTED here rather than recorded in the budget file. A
	// stored copy is derived data that two migration rungs both have to touch,
	// so it conflicted on every parallel pass over a number neither rung
	// actually disagreed about. Computing it in one place removes the conflict
	// class instead of resolving it repeatedly.
	budgeted := 0
	for _, n := range budget {
		budgeted += n
	}
	t.Logf("%s comment budget: %d pointer lines in %d files (destination: 0)",
		sc.name, budgeted, len(budget))

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
	t.Logf("%s: %d comment lines carry a pointer, across %d of %d files walked",
		sc.name, total, len(actual), walked)
}

// The two scopes are asserted separately so a failure names which half of the
// repository regressed, and so the finished production budget cannot be
// relaxed by work on the unfinished test one.
func TestCommentBudget(t *testing.T) {
	assertCommentBudget(t, production)
}

// TestCommentBudget_Tests holds the _test.go half to its own falling budget.
//
// This scope was DELIBERATELY excluded from the production migration — the
// exclusion is recorded in that budget's header — on the reviewer's condition
// that it be tracked rather than dropped. This is that tracking, and it is a
// real ratchet rather than a note: the numbers may only fall, and a file that
// reaches zero leaves the budget and is frozen there.
//
// A pointer in a test is not automatically the same defect as a pointer in
// production code, and that is worth stating because it is the argument for
// migrating them at all. A test comment reading "criterion 9" or "ADR-0013
// §3.1" is trying to say WHICH REQUIREMENT this test pins — genuinely useful
// information, wearing a form the reader cannot resolve. The migration keeps
// the information and drops the coordinate: state the requirement, so the test
// says what it is defending instead of naming a document that defends it.
func TestCommentBudget_Tests(t *testing.T) {
	assertCommentBudget(t, tests)
}
