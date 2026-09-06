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
// did.
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
// files against other people. Reproduced on dao.New's first two panics.
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
//     worse than no number, so the column is gone. Its job is
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
	"reflect"
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
	// arm green. Identity, not arithmetic, has to be frozen.
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

	// catContract: reachable at runtime, and the API DOCUMENTS the panic as its
	// deliberate behaviour — opt-in, stated at the option that enables it, and
	// ratified in review. Distinct from catViolation, which is a panic the
	// contract does not want; and distinct from catInvariant, which is
	// programmer error no data can cause.
	//
	// The distinction is not academic: this file classified tui/queue.go's
	// overflow panic as a LIVE violation "so a library crashes its host
	// process", from reading queue.go alone. The option that enables it says
	// "apps preferring fail-fast crash detection over memory growth opt in
	// here", the default is unlimited, and App.Post repeats it. A budget that
	// cannot tell a documented opt-in contract from a
	// defect will send someone to "fix" a behaviour a consumer chose.
	catContract category = "contract"

	// catUnreviewed: enumerated but not yet read. Permitted ONLY for the
	// frozen legacy identities in legacyPath.
	catUnreviewed category = "unreviewed"
)

var validCategories = map[category]bool{
	catConstruction: true, catInvariant: true, catRepanic: true,
	catUnreachable: true, catViolation: true, catUnreviewed: true,
	catContract: true,
}

// maxViolations pins violation ROWS — sites, not defects; one defect can
// occupy several sites. Lowering it belongs in the PR that fixes a site;
// raising it is a reviewed decision, never a fix.
//
// IT IS ZERO. The history is worth keeping, because the number moved in both
// directions and only the last move was a fix:
//
//	4 -> 3  tui/queue.go's overflow panic was RECLASSIFIED, not fixed: it is a
//	        documented opt-in contract rather than a rule violation (catContract).
//	3 -> 4  dao.Str gained the StringQuoter capability, which ADDED a runtime
//	        panic path — a dialect that HAS a quoting rule refusing a particular
//	        string. A reviewed decision, not a fix: the alternative was Str
//	        guessing an escaping rule on the dialect's behalf, which is an
//	        injection rather than a panic. The three older rows got NARROWER in
//	        the same change, so the count went up while the exposure went down —
//	        exactly the case this cap exists to make someone say out loud.
//	4 -> 0  dao.Str was REMOVED. All four rows were its sites, and they existed
//	        because Str had to turn a Go string into a SQL literal with no
//	        portable way to do it — MySQL's escaping depends on session state,
//	        and a declaration is written before any connection exists. Coalesce
//	        now takes two Expr values, so the same mistakes are COMPILE errors
//	        (see dao/testdata/negative) and there is nothing left to refuse at
//	        render time.
//
// Zero is a destination, not a streak: a new violation row is a reviewed
// decision that has to argue for itself against an empty budget.
const maxViolations = 0

// verdictRe requires a violation to state whether it is reachable in practice.
// This replaces the removed numeric column: "is this live?" is answered by
// evidence a human recorded, not by a spelling total.
var verdictRe = regexp.MustCompile(`^(LIVE|LATENT): `)

// printRe pins the fingerprint grammar. 64 bits, not the 40 an earlier draft
// truncated to.
var printRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

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

// render prints a node from the syntax tree. It does NOT rewrite the output:
// printer.Fprint already normalises syntactic whitespace (it re-prints from
// the AST, so `a  +  b` and `a + b` produce one string), while a string
// literal keeps its raw token text.
//
// An earlier draft ran `\s+` -> " " over the printed output, which reached
// INSIDE literals: panic("a  b") and panic("a b") hashed identically, and the
// comment above it claimed tokens were not normalised — a false statement
// about its own code.
func render(fset *token.FileSet, n ast.Node) string {
	if n == nil || reflect.ValueOf(n).IsNil() {
		return ""
	}
	var b strings.Builder
	if err := printer.Fprint(&b, fset, n); err != nil {
		return "<unprintable>"
	}
	return strings.TrimSpace(b.String())
}

// scopePath returns the full structural ancestor path from the enclosing
// function declaration down to a panic call: every control statement on the
// way, WITH THE EDGE TAKEN, plus a per-scope index for each function literal.
//
// This is the fourth identity design in this file, and each earlier one was
// defeated by a mutation rather than by reading:
//
//   - file+func+ordinal was positional: exchanging two panic messages inside
//     one function moved a reason onto different code, silently.
//   - adding a fingerprint of the panic EXPRESSION did not fix that: the set
//     of (function, expression) pairs survives an exchange intact.
//   - adding only the INNERMOST guard still missed outer guards, then-vs-else
//     polarity, the switch subject, every expression after the first in a
//     multi-expression case, and select arms — so a panic could move between
//     genuinely different control paths without changing identity
//     — `if x { if y {…} }` and `if !x { if y {…} }` collided on
//     9ed2235862.
//
// A category is a claim about the control path that reaches a panic. If the
// path is not in the identity, the claim can migrate to code it was never
// written about.
func scopePath(fset *token.FileSet, stack []ast.Node) (funcPath, guardPath string) {
	var fnSegs, guardSegs []string
	for i, n := range stack {
		var next ast.Node
		if i+1 < len(stack) {
			next = stack[i+1]
		}
		switch node := n.(type) {
		case *ast.FuncDecl:
			fnSegs = append(fnSegs, funcName(node))
		case *ast.FuncLit:
			fnSegs = append(fnSegs, "func$"+strconv.Itoa(litIndexInScope(stack[:i], node)))
		case *ast.IfStmt:
			edge := "?"
			switch {
			case next == ast.Node(node.Body):
				edge = "then"
			case node.Else != nil && next == node.Else:
				edge = "else"
			case node.Cond != nil && next == ast.Node(node.Cond):
				edge = "cond"
			case node.Init != nil && next == node.Init:
				edge = "init"
			}
			guardSegs = append(guardSegs, "if("+render(fset, node.Cond)+")>"+edge)
		case *ast.SwitchStmt:
			guardSegs = append(guardSegs, "switch("+render(fset, node.Tag)+")")
		case *ast.TypeSwitchStmt:
			guardSegs = append(guardSegs, "typeswitch("+render(fset, node.Assign)+")")
		case *ast.CaseClause:
			if len(node.List) == 0 {
				guardSegs = append(guardSegs, "case(default)")
				break
			}
			// EVERY expression, not just the first: `case a, b:` and
			// `case a, c:` are different paths.
			parts := make([]string, 0, len(node.List))
			for _, e := range node.List {
				parts = append(parts, render(fset, e))
			}
			guardSegs = append(guardSegs, "case("+strings.Join(parts, ",")+")")
		case *ast.SelectStmt:
			guardSegs = append(guardSegs, "select")
		case *ast.CommClause:
			if node.Comm == nil {
				guardSegs = append(guardSegs, "comm(default)")
			} else {
				guardSegs = append(guardSegs, "comm("+render(fset, node.Comm)+")")
			}
		case *ast.ForStmt:
			guardSegs = append(guardSegs, "for("+render(fset, node.Init)+";"+
				render(fset, node.Cond)+";"+render(fset, node.Post)+")")
		case *ast.RangeStmt:
			guardSegs = append(guardSegs, "range("+render(fset, node.X)+")")
		}
	}
	funcPath = strings.Join(fnSegs, ".")
	if funcPath == "" {
		funcPath = "<file-scope>"
	}
	return funcPath, strings.Join(guardSegs, "/")
}

// litIndexInScope numbers a function literal among its SIBLINGS — the literals
// whose nearest enclosing block is the same block as this one's — in source
// order, 1-based.
//
// Scoping matters: indexing among every literal in the enclosing declaration
// (a global preorder counter) meant adding a closure anywhere earlier
// renumbered every later one, churning rows for an unrelated edit; indexing
// within the immediate parent EXPRESSION gave every literal $1, because a
// `defer func(){}()` parent contains exactly one. Siblings in a block is the
// scope that matches how a reader thinks about them.
func litIndexInScope(ancestors []ast.Node, target *ast.FuncLit) int {
	// Nearest enclosing block of the target.
	var scope *ast.BlockStmt
	for i := len(ancestors) - 1; i >= 0; i-- {
		if b, ok := ancestors[i].(*ast.BlockStmt); ok {
			scope = b
			break
		}
	}
	if scope == nil {
		return 1
	}
	idx := 1
	var blocks []ast.Node
	ast.Inspect(scope, func(n ast.Node) bool {
		if n == nil {
			blocks = blocks[:len(blocks)-1]
			return true
		}
		blocks = append(blocks, n)
		fl, ok := n.(*ast.FuncLit)
		if !ok || fl == target {
			return true
		}
		// Only count literals whose OWN nearest enclosing block is `scope`:
		// a literal nested inside another literal belongs to a deeper scope
		// and must not shift its siblings' numbers.
		var own *ast.BlockStmt
		for i := len(blocks) - 2; i >= 0; i-- {
			if b, ok := blocks[i].(*ast.BlockStmt); ok {
				own = b
				break
			}
		}
		if own == scope && fl.Pos() < target.Pos() {
			idx++
		}
		return true
	})
	return idx
}

// fingerprint hashes the panic's arguments together with the full control path
// that reaches it. 64 bits, and its grammar is validated on load so a truncated
// or hand-edited value cannot silently match nothing.
func fingerprint(args string, guardPath string) string {
	sum := sha256.Sum256([]byte(args + "\x00" + guardPath))
	return hex.EncodeToString(sum[:])[:16]
}

func census(t *testing.T, files []parsedFile) []site {
	t.Helper()
	var out []site
	for _, pf := range files {
		var stack []ast.Node
		ast.Inspect(pf.file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "panic" {
				return true
			}
			fnPath, guardPath := scopePath(pf.fset, stack)
			var b strings.Builder
			for i, a := range call.Args {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(render(pf.fset, a))
			}
			out = append(out, site{
				File:  pf.rel,
				Func:  fnPath,
				Print: fingerprint(b.String(), guardPath),
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
	// Ordinals disambiguate only genuinely identical panics on identical paths.
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
		if !printRe.MatchString(parts[2]) {
			t.Fatalf("%s:%d: fingerprint %q is not 16 lowercase hex digits — a truncated or "+
				"hand-edited value would silently match no site", inventoryPath, i+1, parts[2])
		}
		s := site{File: parts[0], Func: parts[1], Print: parts[2], Ord: ord,
			Cat: category(parts[4])}
		if len(parts) == 6 {
			s.Reason = strings.TrimSpace(parts[5])
		}
		if !validCategories[s.Cat] {
			t.Fatalf("%s:%d: unknown category %q", inventoryPath, i+1, s.Cat)
		}
		if s.Cat == catContract && s.Reason == "" {
			t.Fatalf("%s:%d: a contract row must cite WHERE the panic is documented — the "+
				"option, the method doc, or the ADR — or it is indistinguishable from an "+
				"unexamined one", inventoryPath, i+1)
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
// legacy classification — net unchanged, every arm green.
// This pins IDENTITY: only the frozen set may be unreviewed.
func TestPanicBudget_UnreviewedIsFrozen(t *testing.T) {
	inv := loadInventory(t)
	legacy := loadLegacy(t)
	if len(legacy) == 0 {
		// The backlog is gone: every panic site has been read. The freeze was
		// never the goal — it was scaffolding that stopped the unread set
		// growing while it was being worked down, and an empty set means it has
		// done its job.
		//
		// So the rule gets STRONGER rather than being switched off. "Only these
		// listed sites may be unreviewed" becomes "no site may be unreviewed",
		// which is the same constraint with the exception removed. This branch
		// asserts that, and it is not a weaker check: it fails on any unreviewed
		// row at all, where the branch below tolerates the listed ones.
		var unread []string
		for k, rec := range inv {
			if rec.Cat == catUnreviewed {
				unread = append(unread, strings.ReplaceAll(k, "\t", " "))
			}
		}
		sort.Strings(unread)
		if len(unread) > 0 {
			t.Fatalf("the frozen legacy set is empty, so NO site may be unreviewed, "+
				"but %d still are:\n  %s", len(unread), strings.Join(unread, "\n  "))
		}
		t.Logf("every panic site is classified; the unreviewed backlog is closed")
		return
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
	// EXACT EQUALITY, not containment. Checking only that unreviewed rows are a
	// subset of the freeze left a stale grandfather slot: classify a row and
	// leave its identity in the freeze file, and the slot stays available for
	// something else later, with every arm green.
	var vanished []string
	for k := range legacy {
		rec, inInv := inv[k]
		switch {
		case !inInv:
			vanished = append(vanished, strings.ReplaceAll(k, "\t", " ")+
				" — no such site; if the code is gone, remove the row in the same change")
		case rec.Cat != catUnreviewed:
			vanished = append(vanished, strings.ReplaceAll(k, "\t", " ")+
				" — now classified as "+string(rec.Cat)+"; remove it from the freeze in the "+
				"same change, or it stays a spare grandfather slot")
		}
	}
	sort.Strings(vanished)
	if len(vanished) > 0 {
		t.Errorf("%d frozen legacy identit(ies) are not currently unreviewed sites. The freeze "+
			"and the unreviewed set must be EXACTLY equal:\n  %s",
			len(vanished), strings.Join(vanished, "\n  "))
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
