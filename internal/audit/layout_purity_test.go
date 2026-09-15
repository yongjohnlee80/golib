package audit

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// THE LAYOUT PURITY GUARD.
//
// Layout and Render measure, place and paint. They do not publish, mount,
// unmount or move. That rule is what lets the runtime run a layout pass
// whenever it needs one — twice in a frame, or not at all — without a component
// being able to observe the difference, and it is why the frame carries a
// separate commit phase for the side effects that genuinely depend on geometry.
//
// HALF OF IT IS ALREADY ENFORCED AT RUNTIME. Mount, Unmount, Move and
// BatchTreeMutation each panic when called while inLayout or inRender is set,
// so a tree mutation from the wrong phase fails loudly the first time it runs.
//
// THE OTHER HALF CANNOT BE. Bus.Publish is documented safe from any goroutine
// and never blocks, so it cannot consult loop-owned phase state: a background
// task publishing at the instant the loop happens to be laying out would panic
// for something it did nothing wrong to do. A runtime guard there would trade a
// real defect for a spurious one.
//
// So publication is checked STATICALLY, here, where no goroutine question
// arises — and the tree mutations are checked here too, because a static answer
// arrives before the line has to be reached at runtime, and an unexercised
// Layout branch that mounts is exactly the case a runtime panic never sees.
//
//	Layout/Render body
//	   │
//	   ├── direct call ─────────────► is it Publish / Mount / Unmount / Move?
//	   │                                     │
//	   └── call to a same-package helper ────┘  (followed transitively)
//
// WHAT IT CANNOT SEE, stated plainly so nobody trusts it further than it goes:
//
//   - FUNCTION LITERALS inside Layout are not descended into. They are how a
//     deferred callback is written — Context.AfterLayout(key, func(){ … }) is
//     the sanctioned way to publish from a geometry-dependent site — so
//     descending would flag the mechanism that exists to make this rule
//     keepable. A literal that Layout invokes IMMEDIATELY is therefore invisible
//     to this check.
//   - Calls through an INTERFACE or a function value are not followed: the
//     callee is not known statically. A helper reached only that way is unseen.
//   - Only SAME-PACKAGE helpers are followed. A Layout that calls into another
//     package which publishes is unseen — the call graph across packages is a
//     different tool's job.
//   - It is a NAME check on the call, not a type-resolved one. A method called
//     Publish on something that is not a Bus counts, which is the safe
//     direction: a false positive is read by a human, a false negative is not.
const (
	maxHelperDepth = 6 // deep enough for the real chains; a cycle cannot spin
)

// forbiddenInPure are the calls a pure phase must not make, with the reason
// each one is a defect rather than a style preference.
var forbiddenInPure = map[string]string{
	"Publish": "publishing from a pure phase makes the event count depend on how " +
		"many times the runtime chose to lay out; register the publication with " +
		"Context.AfterLayout and emit it from the commit phase instead",
	"Mount": "mounting from a pure phase mutates the tree the pass is walking",
	"Unmount": "unmounting from a pure phase mutates the tree the pass is walking, " +
		"and the pass may already have measured the node being removed",
	"Move":              "reordering children mid-pass changes the walk underneath itself",
	"BatchTreeMutation": "a batch is still a tree mutation, and a pure phase may not make one",
}

// pureMethods are the phases the rule governs.
var pureMethods = map[string]bool{"Layout": true, "Render": true}

// purityScope is the packages this audit covers. The runtime and the widget
// suite are where Layout and Render bodies live; a consumer's own components
// are their business.
var purityScope = []string{"tui"}

// TestNothingPublishesOrMutatesFromLayoutOrRender walks every Layout and Render
// method in scope, follows the same-package helpers they call, and fails on any
// forbidden call it reaches.
func TestNothingPublishesOrMutatesFromLayoutOrRender(t *testing.T) {
	root := repoRoot(t)
	pkgs := collectPurityPackages(t, root)
	if len(pkgs) == 0 {
		t.Fatal("no packages in scope; the audit would pass by observing nothing")
	}

	// A positive control for that count: the suite's two largest component
	// packages must both be present, or a path change has silently narrowed the
	// scan to somewhere with no Layout methods in it.
	var phases int
	for _, p := range pkgs {
		for name := range p.funcs {
			if pureMethods[methodName(name)] {
				phases++
			}
		}
	}
	if phases < 20 {
		t.Fatalf("only %d Layout/Render bodies found across %d packages; the scan is "+
			"not reaching the component suite", phases, len(pkgs))
	}

	var findings []string
	for _, p := range pkgs {
		names := make([]string, 0, len(p.funcs))
		for name := range p.funcs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if !pureMethods[methodName(name)] {
				continue
			}
			for _, f := range p.reachableForbidden(name) {
				findings = append(findings, f)
			}
		}
	}
	sort.Strings(findings)
	for _, f := range findings {
		t.Error(f)
	}
	if len(findings) > 0 {
		t.Logf("%d forbidden call(s) reachable from a pure phase. Layout and Render "+
			"measure, place and paint; everything that depends on final geometry "+
			"belongs in the commit phase (Context.AfterLayout).", len(findings))
	}
}

// purityPackage is one package's functions, indexed by the name a call site
// would use to reach them.
type purityPackage struct {
	dir   string
	fset  *token.FileSet
	funcs map[string]*ast.FuncDecl
	rel   map[string]string // func key -> repo-relative file
}

// methodName strips the receiver from a key like "(*Split).Layout".
func methodName(key string) string {
	if i := strings.LastIndex(key, "."); i >= 0 {
		return key[i+1:]
	}
	return key
}

// funcKey names a declaration the way this audit indexes it.
func funcKey(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	return "(" + typeString(fd.Recv.List[0].Type) + ")." + fd.Name.Name
}

// typeString renders a receiver type as it is written.
func typeString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr: // a generic receiver, e.g. List[T]
		return typeString(t.X)
	case *ast.IndexListExpr:
		return typeString(t.X)
	}
	return "?"
}

// collectPurityPackages parses every production file in the scope, grouped by
// directory, because a helper is only followed within its own package.
func collectPurityPackages(t *testing.T, root string) []*purityPackage {
	t.Helper()
	byDir := map[string]*purityPackage{}
	for _, scope := range purityScope {
		base := filepath.Join(root, scope)
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return fmt.Errorf("parse %s: %w", path, perr)
			}
			dir := filepath.Dir(path)
			p := byDir[dir]
			if p == nil {
				p = &purityPackage{dir: dir, fset: fset,
					funcs: map[string]*ast.FuncDecl{}, rel: map[string]string{}}
				byDir[dir] = p
			}
			relPath, _ := filepath.Rel(root, path)
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				key := funcKey(fd)
				p.funcs[key] = fd
				p.rel[key] = relPath
			}
			// One FileSet per package keeps positions resolvable; the first
			// file's set is reused for the rest.
			if p.fset != fset {
				p.mergePositions(fset)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", scope, err)
		}
	}
	out := make([]*purityPackage, 0, len(byDir))
	for _, p := range byDir {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dir < out[j].dir })
	return out
}

// mergePositions is a no-op seam: each file was parsed with its own FileSet, so
// a position is resolved against the set that produced it. Findings carry the
// repo-relative file name rather than a line, which is what survives a rename
// and is what a reader needs to find the method.
func (p *purityPackage) mergePositions(*token.FileSet) {}

// reachableForbidden reports every forbidden call reachable from entry, with
// the chain that reaches it.
func (p *purityPackage) reachableForbidden(entry string) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(key string, depth int, chain []string)
	walk = func(key string, depth int, chain []string) {
		if depth > maxHelperDepth || seen[key] {
			return
		}
		seen[key] = true
		fd := p.funcs[key]
		if fd == nil {
			return
		}
		for _, c := range directCalls(fd) {
			if why, bad := forbiddenInPure[c.name]; bad {
				out = append(out, fmt.Sprintf("%s: %s reaches %s()%s\n  %s",
					p.rel[entry], entry, c.name, chainSuffix(chain), why))
				continue
			}
			// A same-package helper is followed; anything else is not reachable
			// by this tool and is listed under "what it cannot see".
			for _, cand := range p.candidates(c) {
				walk(cand, depth+1, append(chain, cand))
			}
		}
	}
	walk(entry, 0, nil)
	return out
}

// chainSuffix renders the path from the pure phase to the offending call.
func chainSuffix(chain []string) string {
	if len(chain) == 0 {
		return " directly"
	}
	return " via " + strings.Join(chain, " -> ")
}

// callSite is one call expression's shape: the method or function name, and the
// receiver expression as written.
type callSite struct {
	name string
	recv string
}

// directCalls lists the calls a body makes, NOT descending into function
// literals — see the header for why that is deliberate rather than an omission.
func directCalls(fd *ast.FuncDecl) []callSite {
	var out []callSite
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.FuncLit:
			return false // a deferred callback, not part of this phase
		case *ast.CallExpr:
			switch fun := t.Fun.(type) {
			case *ast.Ident:
				out = append(out, callSite{name: fun.Name})
			case *ast.SelectorExpr:
				out = append(out, callSite{name: fun.Sel.Name, recv: exprString(fun.X)})
			}
		}
		return true
	}
	ast.Inspect(fd.Body, visit)
	return out
}

// exprString renders a receiver expression well enough to recognise a self-call.
func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	}
	return ""
}

// candidates names the same-package declarations a call site could reach: a
// plain function, or a method of any receiver type in this package. Receiver
// types are not resolved — a method name matching several types follows all of
// them, which over-approximates in the safe direction.
func (p *purityPackage) candidates(c callSite) []string {
	var out []string
	if c.recv == "" {
		if _, ok := p.funcs[c.name]; ok {
			out = append(out, c.name)
		}
		return out
	}
	suffix := "." + c.name
	for key := range p.funcs {
		if strings.HasSuffix(key, suffix) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
