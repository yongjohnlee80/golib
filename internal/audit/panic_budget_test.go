// Package audit holds repo-wide guards that assert properties of the source
// tree itself rather than of any one package's behaviour. It is test-only: it
// exports nothing and is imported by nothing.
//
// The guard here is a panic budget. golib's development contract permits a
// panic at construction time (fail early, before anything is running) and
// forbids one on API misuse a caller can reach at runtime with valid types,
// where the contract is an error return. That rule was written down and
// nothing checked it, so the tree accumulated panic sites whose categories
// nobody had established.
//
// # What this is, precisely
//
// DEBT SCAFFOLDING plus a classified core — not a finished classified budget.
// 20 sites carry a category a human read; the rest are recorded as unreviewed,
// their identities FROZEN, and no new site may join them. Calling the whole
// thing "classified" would be too strong, and an earlier draft of this file
// did (lector, PR #28 r0 MF3).
//
// # Identity: what makes two panics "the same site"
//
// file + function path + a FINGERPRINT of the panic expression + an ordinal
// among identical fingerprints in that function.
//
// The fingerprint is load-bearing, and it is the third design this file has
// had. The first keyed on file + function + ordinal alone, which is POSITIONAL
// rather than identifying: exchanging two panic messages inside one function
// left both keys intact, so a reason written for one branch silently attached
// to the other and every arm stayed green. That draft even carried a comment
// claiming such a swap "would no longer match" — a relation asserted in prose
// that nothing in the code observed, which is the exact defect this repository
// files against other people. Reproduced by lector on dao.New's first two
// panics.
//
// Fingerprinting the panic EXPRESSION alone was not enough either: the
// exchange leaves the set of (function, expression) pairs intact, so both rows
// survive and each recorded reason still matches its own message — while that
// message now sits behind a different condition. So the fingerprint covers the
// expression AND the guarding condition. A category is a claim about the
// control path that reaches the panic, and a claim whose subject can move is
// not pinned.
//
// Function literals get their own path segment ($1, $2 by source order within
// the enclosing declaration) so a panic moved into or out of a closure changes
// identity rather than inheriting the outer declaration's row.
//
// Line numbers are deliberately NOT identity: keying on them meant one comment
// added atop tui/app.go churned 13 rows, and an inventory that churns on edits
// nobody made teaches its readers to regenerate without reading.
//
// # What it cannot see
//
//   - It cannot tell whether a panic is CORRECT. It forces every site to carry
//     a category, every violation to carry a LIVE/LATENT verdict with its
//     evidence, and every new site to be classified rather than grandfathered.
//   - It cannot measure reachability. An earlier draft carried a numeric
//     "callers" column; it double-counted every qualified call, merged every
//     unrelated symbol sharing a spelling (an unrelated struct field named
//     RunTx moved dao.RunTx from 0 to 1), and could not see consumers at all —
//     dao.RunTx has no non-test reference inside golib and 18 in autodb. A
//     corrupted aggregate that churns the inventory on unrelated edits is
//     worse than no number, so the column is gone (lector, r0 MF2). Its job is
//     done instead by the verdict token: a violation must say LIVE or LATENT
//     and cite its evidence, because the reachability of a public API is a
//     cross-repository question this instrument structurally cannot answer.
//   - It covers every .go file in the repository INCLUDING the nested
//     dao/bigquery module, which `go test ./...` from the root does not reach.
//
// Regenerate with GOLIB_PANIC_BUDGET_UPDATE=1 and read the diff: a changed
// fingerprint means the panic expression changed and its reason needs
// re-reading, which is the point.
//
// Why a Go test rather than a CI script: it runs in the suite everyone already
// runs, needs no new pinned CI step, and adds no dependency.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	inventoryPath = "testdata/panic_budget.txt"
	// legacyPath freezes the identities that were already unreviewed when this
	// guard was adopted. Pinning only the COUNT let a new unread panic be
	// admitted by spending one legacy classification — net unchanged, every
	// arm green (lector, r0 MF3). Identity, not arithmetic, has to be frozen.
	legacyPath = "testdata/panic_budget_legacy_unreviewed.txt"
)

type category string

const (
	// catConstruction: reached only while building a value, before it is in
	// use. The contract explicitly permits these.
	catConstruction category = "construction"

	// catInvariant: a runtime method, but the panic reports a PROGRAMMER error
	// that no data input can cause — a nil child, an index out of range, an
	// operation called from the wrong lifecycle phase.
	catInvariant category = "invariant"

	// catRepanic: re-raising a recovered panic after cleanup.
	catRepanic category = "repanic"

	// catUnreachable: guards a case the type system should make impossible.
	catUnreachable category = "unreachable"

	// catViolation: reachable at runtime with valid types, where the contract
	// should be an error return. Must carry a LIVE or LATENT verdict.
	catViolation category = "violation"

	// catUnreviewed: enumerated but not yet read. Permitted ONLY for the
	// frozen legacy identities in legacyPath.
	catUnreviewed category = "unreviewed"
)

var validCategories = map[category]bool{
	catConstruction: true, catInvariant: true, catRepanic: true,
	catUnreachable: true, catViolation: true, catUnreviewed: true,
}

// maxViolations pins violation ROWS — sites, not defects; one defect can
// occupy several sites. Lowering it belongs in the PR that fixes a site;
// raising it is a reviewed decision, never a fix.
const maxViolations = 4

// verdictRe requires a violation to state whether it is reachable in practice.
// This replaces the removed numeric column: "is this live?" is answered by
// evidence a human recorded, not by a spelling total.
var verdictRe = regexp.MustCompile(`^(LIVE|LATENT): `)

type site struct {
	File   string // repo-relative, slash-separated
	Func   string // "F", "T.M", or with closures "T.M$1"
	Print  string // fingerprint of the panic expression
	Ord    int    // among identical (File, Func, Print)
	Line   int    // for error messages only; NOT identity
	Cat    category
	Reason string
}

func (s site) key() string {
	return fmt.Sprintf("%s\t%s\t%s#%d", s.File, s.Func, s.Print, s.Ord)
}

func (s site) where() string {
	return fmt.Sprintf("%s:%d in %s() [%s]", s.File, s.Line, s.Func, s.Print)
}

func (s site) row() string {
	return strings.Join([]string{
		s.File, s.Func, s.Print, strconv.Itoa(s.Ord), string(s.Cat), s.Reason,
	}, "\t")
}

// ── the census ──────────────────────────────────────────────────────────────

// repoRoot walks up from this test's own directory to the module root, so the
// test cannot silently scan a different tree than the one it lives in.
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

var wsRe = regexp.MustCompile(`\s+`)

func render(fset *token.FileSet, n ast.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	if err := printer.Fprint(&b, fset, n); err != nil {
		return "<unprintable>"
	}
	return strings.TrimSpace(wsRe.ReplaceAllString(b.String(), " "))
}

// guardOf renders the CONDITION of the innermost control statement enclosing
// pos — an if, a switch case, a for or a range.
//
// It is part of identity, and it is the third design this file has had.
// Fingerprinting the panic EXPRESSION alone still missed the bypass lector
// reproduced: exchanging two panic messages inside one function leaves the set
// of (function, expression) pairs unchanged, so both rows survive and each
// recorded reason still matches its own message — while the message now sits
// behind a DIFFERENT condition. A category is a claim about the control path
// reaching the panic, so the path has to be in the identity or the claim can
// migrate silently. (lector, PR #28 r0 MF1.)
func guardOf(f *ast.File, fset *token.FileSet, pos token.Pos) string {
	var best ast.Node
	var bestPos token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil || pos < n.Pos() || pos >= n.End() {
			return true
		}
		var cond ast.Node
		switch node := n.(type) {
		case *ast.IfStmt:
			cond = node.Cond
		case *ast.CaseClause:
			// A default clause has no expressions; its identity is "default".
			if len(node.List) > 0 {
				cond = node.List[0]
			}
		case *ast.ForStmt:
			cond = node.Cond
		case *ast.RangeStmt:
			cond = node.X
		default:
			return true
		}
		if n.Pos() >= bestPos {
			best, bestPos = n, n.Pos()
			if cond == nil {
				best = nil // default clause: recorded as the marker below
			} else {
				best = cond
			}
		}
		return true
	})
	if bestPos == 0 {
		return "<unguarded>"
	}
	if best == nil {
		return "<default>"
	}
	return render(fset, best)
}

// fingerprint hashes the panic's arguments TOGETHER WITH the condition
// guarding it. Whitespace is normalised so gofmt and line wrapping do not
// change identity, but the tokens are not: rewording a message, or moving it
// behind a different condition, DOES change the fingerprint, which retires the
// row and forces its recorded reason to be re-read. That is the point.
func fingerprint(fset *token.FileSet, f *ast.File, call *ast.CallExpr) string {
	var b strings.Builder
	for i, a := range call.Args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(render(fset, a))
	}
	joined := b.String() + "\x00" + guardOf(f, fset, call.Pos())
	sum := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(sum[:])[:10]
}

// funcPathOf builds the innermost enclosing scope path for a position: the
// declaration name, plus a $n segment per enclosing function literal.
func funcPathOf(f *ast.File, pos token.Pos) string {
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || pos < fd.Pos() || pos >= fd.End() {
			continue
		}
		path := funcName(fd)
		var lits []*ast.FuncLit
		ast.Inspect(fd, func(n ast.Node) bool {
			if fl, ok := n.(*ast.FuncLit); ok {
				lits = append(lits, fl)
			}
			return true
		})
		for i, fl := range lits {
			if pos >= fl.Pos() && pos < fl.End() {
				path += "$" + strconv.Itoa(i+1)
			}
		}
		return path
	}
	return "<file-scope>"
}

func census(t *testing.T, files []parsedFile) []site {
	t.Helper()
	var out []site
	for _, pf := range files {
		ast.Inspect(pf.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "panic" {
				return true
			}
			out = append(out, site{
				File:  pf.rel,
				Func:  funcPathOf(pf.file, call.Pos()),
				Print: fingerprint(pf.fset, pf.file, call),
				Line:  pf.fset.Position(call.Lparen).Line,
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
	// Ordinals disambiguate only genuinely identical panics in one function.
	seen := map[string]int{}
	for i := range out {
		k := out[i].File + "\t" + out[i].Func + "\t" + out[i].Print
		seen[k]++
		out[i].Ord = seen[k]
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
		ord, err := strconv.Atoi(parts[3])
		if err != nil {
			t.Fatalf("%s:%d: bad ordinal %q", inventoryPath, i+1, parts[3])
		}
		s := site{File: parts[0], Func: parts[1], Print: parts[2], Ord: ord,
			Cat: category(parts[4])}
		if len(parts) == 6 {
			s.Reason = strings.TrimSpace(parts[5])
		}
		if !validCategories[s.Cat] {
			t.Fatalf("%s:%d: unknown category %q", inventoryPath, i+1, s.Cat)
		}
		if s.Cat == catViolation {
			if s.Reason == "" {
				t.Fatalf("%s:%d: a violation row must carry a reason", inventoryPath, i+1)
			}
			if !verdictRe.MatchString(s.Reason) {
				t.Fatalf("%s:%d: a violation reason must begin with \"LIVE: \" or \"LATENT: \" — "+
					"reachability is the question the removed numeric column could not answer; "+
					"got %q", inventoryPath, i+1, s.Reason)
			}
		}
		if prev, dup := got[s.key()]; dup {
			t.Fatalf("%s:%d: duplicate row for %s (also %s)", inventoryPath, i+1,
				strings.ReplaceAll(s.key(), "\t", " "), prev.Func)
		}
		got[s.key()] = s
	}
	return got
}

// loadLegacy reads the frozen set of identities that were unreviewed at
// adoption. Anything not in here may never be unreviewed.
func loadLegacy(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("read %s: %v", legacyPath, err)
	}
	got := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if s := strings.TrimSpace(line); s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		got[line] = true
	}
	return got
}

func maybeRegenerate(t *testing.T, found []site, inv map[string]site) {
	if os.Getenv("GOLIB_PANIC_BUDGET_UPDATE") != "1" {
		return
	}
	t.Helper()
	var b strings.Builder
	b.WriteString(inventoryHeader)
	for _, s := range found {
		if prev, ok := inv[s.key()]; ok {
			s.Cat, s.Reason = prev.Cat, prev.Reason
		}
		if s.Cat == "" {
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
#   file <TAB> func path <TAB> fingerprint <TAB> ordinal <TAB> category <TAB> reason
#
# A site is identified by file + function path + a FINGERPRINT of the panic
# expression AND THE CONDITION GUARDING IT + an ordinal among identical
# fingerprints. Not by line number (an edit above a panic would churn its row)
# and NOT by position: an earlier draft keyed on file+func+ordinal, so
# exchanging two panic messages inside one function silently moved a reason
# onto different code with every check green. Fingerprinting the expression
# alone did not fix that either — both rows survive the exchange and each still
# matches its own message, while the message now sits behind a different
# condition. A category is a claim about the control path reaching the panic,
# so the guard is in the identity. Closures get a $n path segment, so moving a
# panic into or out of one also changes its identity.
#
# Categories:
#   construction  reached only while building a value — permitted by golib.md
#   invariant     a runtime method, but reports programmer error no data can cause
#   repanic       re-raising a recovered panic after cleanup
#   unreachable   guards a case the type system should make impossible
#   violation     REACHABLE AT RUNTIME WITH VALID TYPES — must state LIVE: or LATENT:
#   unreviewed    enumerated, not yet read — ONLY for the frozen legacy identities
#                 in panic_budget_legacy_unreviewed.txt; no new site may join
#
# There is deliberately no numeric reachability column. The one this file used
# to carry double-counted qualified calls, merged every symbol sharing a
# spelling, and could not see consumers at all (dao.RunTx: 0 references inside
# golib, 18 in autodb). Reachability of a public API is a cross-repository
# question this instrument cannot answer, so a violation states its verdict and
# cites its evidence instead.
#
# Regenerate: GOLIB_PANIC_BUDGET_UPDATE=1 go test ./internal/audit/
`

// ── the assertions ──────────────────────────────────────────────────────────

// TestPanicBudget_InventoryMatchesTree fails in BOTH directions: an unrecorded
// site, or a row whose site is gone. One direction alone would pass forever
// while the other rotted.
func TestPanicBudget_InventoryMatchesTree(t *testing.T) {
	files := parseTree(t, repoRoot(t))
	found := census(t, files)
	if len(found) == 0 {
		t.Fatal("census found no panic sites at all — the instrument is broken, not the tree clean")
	}
	inv := loadInventory(t)
	maybeRegenerate(t, found, inv)

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
			stale = append(stale, fmt.Sprintf("%s (%s, %s)",
				strings.ReplaceAll(k, "\t", " "), rec.Func, rec.Cat))
		}
	}
	sort.Strings(unrecorded)
	sort.Strings(stale)

	if len(unrecorded) > 0 {
		t.Errorf("%d panic site(s) missing from %s — a new site needs a category and a "+
			"reason, or an error return instead. A CHANGED FINGERPRINT also lands here: the "+
			"panic expression moved or was reworded, so its recorded reason must be re-read "+
			"rather than carried over:\n  %s",
			len(unrecorded), inventoryPath, strings.Join(unrecorded, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("%d inventory row(s) point at sites that no longer exist — delete them, or "+
			"regenerate:\n  %s", len(stale), strings.Join(stale, "\n  "))
	}
	t.Logf("census: %d panic sites across %d files", len(found), countFiles(found))
}

// TestPanicBudget_UnreviewedIsFrozen makes the unread backlog a real
// constraint. Pinning only its COUNT let a new unread panic in by spending one
// legacy classification — net unchanged, every arm green (lector, r0 MF3).
// This pins IDENTITY: only the frozen set may be unreviewed.
func TestPanicBudget_UnreviewedIsFrozen(t *testing.T) {
	inv := loadInventory(t)
	legacy := loadLegacy(t)
	if len(legacy) == 0 {
		t.Fatal("the frozen legacy set is empty — the freeze is not observing anything")
	}

	var intruders []string
	unreviewed := map[string]bool{}
	for k, rec := range inv {
		if rec.Cat != catUnreviewed {
			continue
		}
		unreviewed[k] = true
		if !legacy[k] {
			intruders = append(intruders, strings.ReplaceAll(k, "\t", " "))
		}
	}
	sort.Strings(intruders)
	if len(intruders) > 0 {
		t.Errorf("%d site(s) are marked unreviewed but are NOT in the frozen legacy set — a "+
			"new panic may not be admitted unread, and classifying a legacy row does not buy "+
			"a slot:\n  %s", len(intruders), strings.Join(intruders, "\n  "))
	}
	// The freeze may only shrink deliberately: a frozen identity that has left
	// the tree must leave the file in the same change.
	var vanished []string
	for k := range legacy {
		if _, ok := inv[k]; !ok {
			vanished = append(vanished, strings.ReplaceAll(k, "\t", " "))
		}
	}
	sort.Strings(vanished)
	if len(vanished) > 0 {
		t.Errorf("%d frozen legacy identit(ies) are absent from the inventory — if that code "+
			"is gone, remove them from %s in the same change so the freeze cannot silently "+
			"shrink:\n  %s", len(vanished), legacyPath, strings.Join(vanished, "\n  "))
	}
	t.Logf("unreviewed: %d of %d sites, all within the %d frozen legacy identities",
		len(unreviewed), len(inv), len(legacy))
}

// TestPanicBudget_ViolationsDoNotGrow gives the inventory teeth in both
// directions: a new violation cannot be admitted, and a fixed one cannot be
// left with the constant still slack.
func TestPanicBudget_ViolationsDoNotGrow(t *testing.T) {
	inv := loadInventory(t)
	var violations, live, latent []string
	for _, rec := range inv {
		if rec.Cat != catViolation {
			continue
		}
		entry := fmt.Sprintf("%s in %s() — %s", rec.File, rec.Func, rec.Reason)
		violations = append(violations, entry)
		if strings.HasPrefix(rec.Reason, "LIVE: ") {
			live = append(live, entry)
		} else {
			latent = append(latent, entry)
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
			"constant in the PR that fixed them, or the budget stops constraining anything",
			len(violations), maxViolations)
	}
	t.Logf("violations: %d (%d LIVE, %d LATENT)", len(violations), len(live), len(latent))
}

// TestPanicBudget_ClassificationIsMeaningful answers the review requirement
// that this distinguish reachability rather than merely enumerate. A census
// whose rows all said one thing, or whose violations recorded no verdict,
// would satisfy the arms above and tell a reader nothing.
func TestPanicBudget_ClassificationIsMeaningful(t *testing.T) {
	inv := loadInventory(t)
	byCat := map[category]int{}
	for _, rec := range inv {
		byCat[rec.Cat]++
	}
	classified := len(inv) - byCat[catUnreviewed]
	if len(byCat) < 3 {
		t.Errorf("the inventory uses only %d categories (%v) — a census that cannot tell "+
			"construction from a runtime path is an enumeration, not a classification",
			len(byCat), byCat)
	}
	if classified == 0 {
		t.Error("every row is unreviewed — this is an enumeration with no classified core")
	}
	cats := make([]string, 0, len(byCat))
	for c, n := range byCat {
		cats = append(cats, fmt.Sprintf("%s=%d", c, n))
	}
	sort.Strings(cats)
	t.Logf("categories: %s (%d of %d classified)", strings.Join(cats, " "), classified, len(inv))
}

func countFiles(ss []site) int {
	f := map[string]bool{}
	for _, s := range ss {
		f[s.File] = true
	}
	return len(f)
}
