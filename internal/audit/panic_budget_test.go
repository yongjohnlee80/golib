// Package audit holds repo-wide guards that assert properties of the source
// tree itself rather than of any one package's behaviour. It is test-only: it
// exports nothing and is imported by nothing.
//
// The guard here is a panic budget. golib's development contract permits a
// panic at construction time (fail early, before anything is running) and
// forbids one on API misuse a caller can reach at runtime with valid types,
// where the contract is an error return. That rule was written down and
// nothing checked it, so the tree accumulated panic sites whose categories
// nobody had established. This test establishes them once, in
// testdata/panic_budget.txt, and then fails on drift in EITHER direction: a
// new site missing from the inventory, or an inventory row whose site is gone.
//
// WHAT THIS GUARD CAN AND CANNOT SEE, stated up front, because a guard whose
// blind spots are undocumented gets trusted for things it does not check:
//
//   - The enclosing function of each panic is EXACT: it is the innermost
//     function declaration whose source range contains the call, computed from
//     the syntax tree. A regex over `^func` cannot see methods at all — which
//     is how an earlier hand count of positional-bool signatures in this repo
//     came out at 3 when the answer was 7.
//
//   - The reference count is INTRA-REPOSITORY, and it is named golib_refs
//     rather than "callers" for that reason. It counts references to the
//     function's name in golib's own non-test files, excluding the
//     declaration: a plain function by identifier and by qualified selector,
//     a method by selector. It is RECOMPUTED on every run rather than trusted
//     from the file, so it cannot rot into an unchecked claim.
//
//     READ ZERO CAREFULLY. Zero means nothing in GOLIB names it — not that
//     nothing calls it. `dao.RunTx` scores 0 here and has 18 non-test
//     references in autodb alone. For a public API, reachability has to be
//     established across the consuming repositories, which this instrument
//     cannot see; the number's honest use is triage inside golib. What made
//     `dao.Str` latent was BOTH halves: 1 golib reference (from `lit`) and a
//     separately measured zero production callers of `Coalesce` in autodb and
//     both ddex repos. Neither half alone would have shown it.
//
//     A name shared by several types or packages over-counts (dao.New and
//     tui.New land on one key); a call through an interface or a func value
//     under-counts.
//
//   - It cannot tell whether a panic is CORRECT. It forces every site to carry
//     a category, and every rule violation to carry a reason.
//
//   - It covers every .go file in the repository INCLUDING the nested
//     dao/bigquery module, which `go test ./...` from the root does not reach.
//
// Regenerate the inventory with GOLIB_PANIC_BUDGET_UPDATE=1 and re-read the
// diff: every row that changes is a decision, and a category or reason that
// the tool cannot infer is preserved from the existing row.
//
// Why a Go test rather than a CI script: it runs in the suite everyone already
// runs, needs no new pinned CI step, and adds no dependency.
package audit

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const inventoryPath = "testdata/panic_budget.txt"

// category is a claim about why a panic at this site is acceptable — or that
// it is not.
type category string

const (
	// catConstruction: reached only while building a value, before it is in
	// use. The contract explicitly permits these ("construction validates and
	// may panic at New, fail early").
	catConstruction category = "construction"

	// catInvariant: a runtime method, but the panic reports a PROGRAMMER error
	// that no data input can cause — a nil child, an index out of range, an
	// operation called from the wrong lifecycle phase. Idiomatic in Go for a
	// contract a caller cannot violate accidentally with valid values.
	catInvariant category = "invariant"

	// catRepanic: re-raising a recovered panic after cleanup, preserving the
	// original failure. Not a new failure mode.
	catRepanic category = "repanic"

	// catUnreachable: guards a case the type system should make impossible.
	catUnreachable category = "unreachable"

	// catUnreviewed: the site is ENUMERATED but nobody has read it yet. This
	// category exists so the guard can be adopted in one PR without anybody
	// claiming 126 classifications they have not made — an inventory whose
	// rows all said "invariant" because a script defaulted them would be a
	// census wearing a classification's clothes. It is budgeted like a
	// violation and the budget must fall as sites are read.
	catUnreviewed category = "unreviewed"

	// catViolation: reachable at runtime with valid types, where the contract
	// should be an error return. These are the rule violations. They are
	// permitted IN THE INVENTORY so the guard can be adopted before they are
	// fixed — but they are counted and the count is pinned, so the number can
	// only fall without a reviewed edit.
	catViolation category = "violation"
)

var validCategories = map[category]bool{
	catConstruction: true, catInvariant: true, catRepanic: true,
	catUnreachable: true, catViolation: true, catUnreviewed: true,
}

// maxViolations pins the number of catViolation ROWS — sites, not defects; one
// defect can occupy several sites. It is measured, then pinned: see the
// inventory header for the current classification. Lowering it belongs in the
// same PR that fixes a site; raising it is a reviewed decision, never a fix.
const maxViolations = 4

// maxUnreviewed pins how many sites are still unread. It is a DEBT counter:
// it may only fall, and the PR that reads a batch of sites lowers it in the
// same change. It is deliberately not zero on adoption — pretending otherwise
// is what this category exists to prevent.
const maxUnreviewed = 126

type site struct {
	File   string // repo-relative, slash-separated
	Func   string // "F" for a function, "T.M" for a method, "<file-scope>" otherwise
	Ord    int    // 1-based: the Nth panic inside that function
	Line   int    // computed for error messages only; NOT part of identity
	Cat    category
	Refs   int // references within golib's own non-test code — see the package comment
	Reason string
}

// key identifies a site by file, enclosing function and ordinal within that
// function — deliberately NOT by line number. Keying on the line made every
// unrelated edit above a panic invalidate its row: one comment added at the
// top of tui/app.go would have churned 13 rows, and an inventory that churns
// on edits nobody made teaches its readers to regenerate without reading,
// which is the one behaviour that would make this guard worthless.
func (s site) key() string { return fmt.Sprintf("%s\t%s#%d", s.File, s.Func, s.Ord) }

// where is for humans in failure messages; the line is recomputed each run.
func (s site) where() string { return fmt.Sprintf("%s:%d in %s()", s.File, s.Line, s.Func) }

func (s site) row() string {
	return strings.Join([]string{
		s.File, s.Func, strconv.Itoa(s.Ord), string(s.Cat),
		strconv.Itoa(s.Refs), s.Reason,
	}, "\t")
}

// ── the census ──────────────────────────────────────────────────────────────

// repoRoot walks up from this test's own directory to the module root. Derived
// from the test's location rather than a relative guess, so the test cannot
// silently scan a different tree than the one it lives in.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for dir := wd; ; {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", wd)
		}
		dir = parent
	}
}

type parsedFile struct {
	rel  string
	file *ast.File
	fset *token.FileSet
}

func parseTree(t *testing.T, root string) []parsedFile {
	t.Helper()
	var out []parsedFile
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "testdata", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}
		out = append(out, parsedFile{rel: filepath.ToSlash(rel), file: f, fset: fset})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return out
}

// census returns every panic call site with its exact enclosing function.
func census(t *testing.T, files []parsedFile) []site {
	t.Helper()
	var out []site
	for _, pf := range files {
		// Collect declarations with their ranges, then attribute each panic to
		// the INNERMOST containing declaration. Position containment is exact
		// and needs no traversal bookkeeping to get right.
		type rng struct {
			name     string
			from, to token.Pos
		}
		var decls []rng
		for _, d := range pf.file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			decls = append(decls, rng{name: funcName(fd), from: fd.Pos(), to: fd.End()})
		}
		ast.Inspect(pf.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "panic" {
				return true
			}
			name := "<file-scope>"
			var best token.Pos
			for _, d := range decls {
				if call.Pos() >= d.from && call.Pos() < d.to && d.from >= best {
					name, best = d.name, d.from
				}
			}
			out = append(out, site{
				File: pf.rel,
				Line: pf.fset.Position(call.Lparen).Line,
				Func: name,
			})
			return true
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	// Ordinals are assigned in source order within each (file, func), so they
	// are stable as long as the panics inside a function keep their relative
	// order. Reordering two panics inside one function DOES swap their rows —
	// which is correct: their reasons would no longer match.
	seen := map[string]int{}
	for i := range out {
		fk := out[i].File + "\t" + out[i].Func
		seen[fk]++
		out[i].Ord = seen[fk]
	}
	return out
}

func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	typ := fd.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	// A generic receiver arrives as IndexExpr / IndexListExpr; its X is the type.
	switch idx := typ.(type) {
	case *ast.IndexExpr:
		typ = idx.X
	case *ast.IndexListExpr:
		typ = idx.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name + "." + fd.Name.Name
	}
	return fd.Name.Name
}

// refCounts counts references per name across golib's own non-test files. See
// the package comment for what this over- and under-counts, and for why zero
// does not mean unreachable.
func refCounts(files []parsedFile) map[string]int {
	counts := map[string]int{}
	for _, pf := range files {
		// Skip the identifier that IS each declaration's name.
		declNames := map[*ast.Ident]bool{}
		for _, d := range pf.file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				declNames[fd.Name] = true
			}
		}
		ast.Inspect(pf.file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.SelectorExpr:
				// A method or qualified call: count by the selected name.
				counts["."+node.Sel.Name]++
			case *ast.Ident:
				if !declNames[node] {
					counts[node.Name]++
				}
			}
			return true
		})
	}
	return counts
}

// refsTo resolves a census Func name against the counted references.
//
// A plain function is called BOTH ways in a multi-package module: unqualified
// from inside its own package (a bare identifier) and qualified from outside
// (`dao.RunTx`, a selector). Counting only the identifier missed every
// cross-package call and reported RunTx as having none; both keys are summed.
func refsTo(counts map[string]int, fn string) int {
	if i := strings.IndexByte(fn, '.'); i >= 0 {
		// A method is only ever reached through a selector.
		return counts["."+fn[i+1:]]
	}
	return counts[fn] + counts["."+fn]
}

// ── the inventory ───────────────────────────────────────────────────────────

func loadInventory(t *testing.T) map[string]site {
	t.Helper()
	raw, err := os.ReadFile(inventoryPath)
	if err != nil {
		t.Fatalf("read %s: %v", inventoryPath, err)
	}
	got := map[string]site{}
	for i, line := range strings.Split(string(raw), "\n") {
		if s := strings.TrimSpace(line); s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 5 {
			t.Fatalf("%s:%d: want 5 or 6 tab-separated fields, got %d: %q",
				inventoryPath, i+1, len(parts), line)
		}
		ord, err := strconv.Atoi(parts[2])
		if err != nil {
			t.Fatalf("%s:%d: bad ordinal %q", inventoryPath, i+1, parts[2])
		}
		refs, err := strconv.Atoi(parts[4])
		if err != nil {
			t.Fatalf("%s:%d: bad golib_refs value %q", inventoryPath, i+1, parts[4])
		}
		s := site{File: parts[0], Func: parts[1], Ord: ord,
			Cat: category(parts[3]), Refs: refs}
		if len(parts) == 6 {
			s.Reason = strings.TrimSpace(parts[5])
		}
		if !validCategories[s.Cat] {
			t.Fatalf("%s:%d: unknown category %q", inventoryPath, i+1, s.Cat)
		}
		if s.Cat == catViolation && s.Reason == "" {
			t.Fatalf("%s:%d: a %q row must carry a reason", inventoryPath, i+1, catViolation)
		}
		if prev, dup := got[s.key()]; dup {
			t.Fatalf("%s:%d: duplicate row for %s#%d (also %s)", inventoryPath, i+1,
				s.File, s.Ord, prev.Func)
		}
		got[s.key()] = s
	}
	return got
}

// maybeRegenerate rewrites the inventory when GOLIB_PANIC_BUDGET_UPDATE=1,
// preserving the category and reason of rows whose site still exists.
func maybeRegenerate(t *testing.T, found []site, inv map[string]site, counts map[string]int) {
	if os.Getenv("GOLIB_PANIC_BUDGET_UPDATE") != "1" {
		return
	}
	t.Helper()
	var b strings.Builder
	b.WriteString(inventoryHeader)
	for _, s := range found {
		s.Refs = refsTo(counts, s.Func)
		if prev, ok := inv[s.key()]; ok {
			s.Cat, s.Reason = prev.Cat, prev.Reason
		}
		if s.Cat == "" {
			// An unread site is recorded as unread, never guessed into a
			// category that would make the inventory look settled.
			s.Cat = catUnreviewed
			s.Reason = "not yet read"
		}
		b.WriteString(s.row())
		b.WriteByte('\n')
	}
	if err := os.WriteFile(inventoryPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	t.Fatalf("%s regenerated with %d rows — review the diff and re-run without "+
		"GOLIB_PANIC_BUDGET_UPDATE", inventoryPath, len(found))
}

const inventoryHeader = `# golib panic budget — one row per panic() call site in non-test code.
#
#   file <TAB> enclosing func <TAB> ordinal <TAB> category <TAB> golib_refs <TAB> reason
#
# A site is identified by file + function + ordinal (the Nth panic inside that
# function), NOT by line number: keying on the line made every unrelated edit
# above a panic churn its row, and an inventory that churns on edits nobody
# made teaches its readers to regenerate without reading.
#
# Categories (see internal/audit/panic_budget_test.go for the full contract):
#   construction  reached only while building a value, before it is in use — permitted
#   invariant     a runtime method, but reports programmer error no data input can cause
#   repanic       re-raising a recovered panic after cleanup
#   unreachable   guards a case the type system should make impossible
#   violation     REACHABLE AT RUNTIME WITH VALID TYPES — the contract should be an error
#   unreviewed    enumerated but not yet read by a human; a DEBT that may only fall
#
# golib_refs is RECOMPUTED on every run and asserted against this file, so it
# cannot rot: references to the function's name in GOLIB's own non-test code,
# excluding the declaration.
#
# ZERO DOES NOT MEAN UNREACHABLE. It means nothing in golib names it. dao.RunTx
# scores 0 and has 18 non-test references in autodb. For a public API,
# reachability must be established in the consuming repos, which this
# instrument cannot see. Over-counts a name shared across packages (dao.New and
# tui.New share a key); under-counts interface and func-value calls.
#
# Regenerate: GOLIB_PANIC_BUDGET_UPDATE=1 go test ./internal/audit/
# Every changed row is a decision. A new site defaults to "CLASSIFY ME".
`

// ── the assertions ──────────────────────────────────────────────────────────

// TestPanicBudget_InventoryMatchesTree fails in BOTH directions: an unrecorded
// panic site, or a recorded site that no longer exists. One direction alone
// would pass forever while the other rotted.
func TestPanicBudget_InventoryMatchesTree(t *testing.T) {
	files := parseTree(t, repoRoot(t))
	found := census(t, files)
	if len(found) == 0 {
		// A census that finds nothing agrees with every inventory. This repo
		// has panics; zero means the walk or the parse broke, not that the
		// tree is clean.
		t.Fatal("census found no panic sites at all — the instrument is broken, not the tree clean")
	}
	inv := loadInventory(t)
	maybeRegenerate(t, found, inv, refCounts(files))

	seen := map[string]bool{}
	var unrecorded []string
	for _, s := range found {
		seen[s.key()] = true
		if _, ok := inv[s.key()]; !ok {
			unrecorded = append(unrecorded, s.where())
		}
	}
	var stale []string
	for k, rec := range inv {
		if !seen[k] {
			stale = append(stale, fmt.Sprintf("%s (panic #%d in %s(), %s)",
				strings.ReplaceAll(k, "\t", " "), rec.Ord, rec.Func, rec.Cat))
		}
	}
	sort.Strings(unrecorded)
	sort.Strings(stale)

	if len(unrecorded) > 0 {
		t.Errorf("%d panic site(s) missing from %s — add a row with a category and a reason, "+
			"or return an error instead:\n  %s",
			len(unrecorded), inventoryPath, strings.Join(unrecorded, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("%d inventory row(s) point at sites that no longer exist — delete them, or "+
			"regenerate:\n  %s", len(stale), strings.Join(stale, "\n  "))
	}
	t.Logf("census: %d panic sites across %d files", len(found), countFiles(found))
}

// TestPanicBudget_RefCountsAreRecomputed asserts every inventory golib_refs
// value against the tree. Without this the numbers would be prose in a data
// file — a claimed relation that nothing checks, which is the shape this
// repository's review conventions exist to catch.
func TestPanicBudget_RefCountsAreRecomputed(t *testing.T) {
	files := parseTree(t, repoRoot(t))
	counts := refCounts(files)
	inv := loadInventory(t)

	// The instrument must be able to observe a difference at all: if every
	// resolved count were zero, matching the file would prove nothing.
	nonZero := 0
	for _, rec := range inv {
		if refsTo(counts, rec.Func) > 0 {
			nonZero++
		}
	}
	if nonZero == 0 {
		t.Fatal("every recomputed golib_refs value is zero — the counter is not observing the tree")
	}

	var wrong []string
	for _, rec := range inv {
		if got := refsTo(counts, rec.Func); got != rec.Refs {
			wrong = append(wrong, fmt.Sprintf("%s#%d (%s): file says %d, tree says %d",
				rec.File, rec.Ord, rec.Func, rec.Refs, got))
		}
	}
	sort.Strings(wrong)
	if len(wrong) > 0 {
		t.Errorf("%d golib_refs value(s) disagree with the tree — regenerate:\n  %s",
			len(wrong), strings.Join(wrong, "\n  "))
	}
	t.Logf("%d/%d rows resolve to a non-zero golib_refs value", nonZero, len(inv))
}

// TestPanicBudget_ViolationsDoNotGrow gives the inventory teeth: without it a
// new runtime-reachable panic could be admitted just by adding a row.
func TestPanicBudget_ViolationsDoNotGrow(t *testing.T) {
	inv := loadInventory(t)
	var violations []string
	for _, rec := range inv {
		if rec.Cat == catViolation {
			violations = append(violations, fmt.Sprintf("%s panic #%d in %s() [%d golib refs] — %s",
				rec.File, rec.Ord, rec.Func, rec.Refs, rec.Reason))
		}
	}
	sort.Strings(violations)
	switch {
	case len(violations) > maxViolations:
		t.Errorf("%d violation rows, budget is %d. A panic reachable at runtime with valid "+
			"types must return an error instead; raising maxViolations is a reviewed "+
			"decision, not a fix:\n  %s",
			len(violations), maxViolations, strings.Join(violations, "\n  "))
	case len(violations) < maxViolations:
		t.Errorf("only %d violation rows remain but maxViolations is still %d — lower the "+
			"constant in the same PR that fixed them, or the budget stops constraining "+
			"anything", len(violations), maxViolations)
	}
	t.Logf("violations at budget (%d):\n  %s", len(violations), strings.Join(violations, "\n  "))
}

// TestPanicBudget_UnreviewedDebtOnlyFalls pins the unread backlog. Without it,
// a new panic could be waved through by marking it unreviewed.
func TestPanicBudget_UnreviewedDebtOnlyFalls(t *testing.T) {
	inv := loadInventory(t)
	n := 0
	for _, rec := range inv {
		if rec.Cat == catUnreviewed {
			n++
		}
	}
	switch {
	case n > maxUnreviewed:
		t.Errorf("%d unreviewed sites, budget is %d — a new panic may not be admitted by "+
			"marking it unreviewed; classify it, or return an error instead", n, maxUnreviewed)
	case n < maxUnreviewed:
		t.Errorf("%d unreviewed sites remain but maxUnreviewed is still %d — lower the "+
			"constant in the PR that read them, or the debt counter stops counting", n, maxUnreviewed)
	}
	t.Logf("unreviewed backlog at budget: %d of %d sites", n, len(inv))
}

// TestPanicBudget_CategoriesAreDistinguishable answers the review requirement
// that this census distinguish production reachability rather than merely
// enumerate. A census whose every row carried one category, or recorded no
// caller counts, would satisfy the arms above and tell a reader nothing.
func TestPanicBudget_CategoriesAreDistinguishable(t *testing.T) {
	inv := loadInventory(t)
	byCat := map[category]int{}
	withCallers := 0
	for _, rec := range inv {
		byCat[rec.Cat]++
		if rec.Refs > 0 {
			withCallers++
		}
	}
	if len(byCat) < 3 {
		t.Errorf("the inventory uses only %d categories (%v) — a census that cannot tell "+
			"construction from a runtime path is an enumeration, not a classification",
			len(byCat), byCat)
	}
	if withCallers == 0 {
		t.Error("no row records a golib_refs value — without one the census ranks a widely " +
			"referenced runtime path together with one nothing in the tree names")
	}
	cats := make([]string, 0, len(byCat))
	for c, n := range byCat {
		cats = append(cats, fmt.Sprintf("%s=%d", c, n))
	}
	sort.Strings(cats)
	t.Logf("categories: %s (%d rows carry a non-zero golib_refs)", strings.Join(cats, " "), withCallers)
}

func countFiles(ss []site) int {
	f := map[string]bool{}
	for _, s := range ss {
		f[s.File] = true
	}
	return len(f)
}
