package audit

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// THE PROMOTION SELF-CALL GUARD.
//
// Go has no virtual dispatch. When a method on an embeddable base type calls
// another method ON ITS OWN RECEIVER, it calls the BASE's version — even when
// the type that embedded it has overridden that method.
//
// So a dialect that embeds a base and overrides QuoteIdent still gets the
// BASE's quoting inside any base method that quotes internally. The SQL comes
// out quoted by the wrong engine's rules, the build is green, every test that
// exercises the overriding dialect through its own methods passes, and the
// defect surfaces as a query that fails against the real database — several
// layers away from the line that caused it.
//
// This is not a hypothetical. dao.GenericDialect.BuildUpsertSuffix quotes with
// its own receiver's QuoteIdent, and two of the four dialects override
// QuoteIdent. It happens to be harmless today only because those same two also
// override BuildUpsertSuffix, so they never reach the base's body. It is one
// override away from being a real bug, and nothing in the compiler or the test
// suite would say so.
//
// The fix at each site is to take the collaborator as a PARAMETER rather than
// reach for the receiver — pass the dialect, so the caller's own QuoteIdent
// does the work. See dao.StandardUpsertSuffix.
//
// The allowlist below may only SHRINK.

const promotionBudget = "testdata/promotion_selfcalls.txt"

// A site is LIVE when some embedder overrides the callee but inherits the
// caller — that embedder is actually running the wrong method today. Otherwise
// it is LATENT: correct now, wrong the moment someone overrides one more
// method. Both are listed; a LIVE one is a failure regardless of the
// allowlist, because an allowlist is for accepted debt and a live defect is
// not debt.
type selfCall struct {
	file, base, caller, callee string
	line                       int
	live                       []string // embedders currently taking the wrong path
}

func (s selfCall) key() string {
	return fmt.Sprintf("%s\t%s.%s -> %s", s.file, s.base, s.caller, s.callee)
}

// signatureOf renders a comparable signature: a same-name method with a
// different one is a wrapper, not an override.
func signatureOf(ft *ast.FuncType) string {
	render := func(fl *ast.FieldList) string {
		if fl == nil {
			return ""
		}
		var parts []string
		for _, f := range fl.List {
			var b strings.Builder
			_ = printer.Fprint(&b, token.NewFileSet(), f.Type)
			n := len(f.Names)
			if n == 0 {
				n = 1
			}
			for i := 0; i < n; i++ {
				parts = append(parts, b.String())
			}
		}
		return strings.Join(parts, ",")
	}
	return "(" + render(ft.Params) + ")(" + render(ft.Results) + ")"
}

func TestPromotionSelfCalls(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}

	type fileAST struct {
		rel  string
		file *ast.File
		fset *token.FileSet
	}
	var files []fileAST
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", path, perr)
		}
		rel, _ := filepath.Rel(root, path)
		files = append(files, fileAST{filepath.ToSlash(rel), f, fset})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repo: %v", err)
	}
	if len(files) < 100 {
		t.Fatalf("walked only %d files; the walk is broken and this guard would "+
			"report a clean repository", len(files))
	}

	// 1. Which named types are EMBEDDED in some struct? Those are the types
	//    whose methods can be promoted, and only they can have this defect.
	embedded := map[string]bool{}
	// 2. methods[type][method] = SIGNATURE. The signature is load-bearing: a
	//    method with the same NAME but a different signature is a wrapper, not
	//    an override, and Go resolves the base's self-call to the base's own
	//    method. Matching on name alone reports those wrappers as defects —
	//    which it did, on TextArea.cellsAt, before this was fixed.
	methods := map[string]map[string]string{}
	// 3. embedders[base] — the types that embed each base.
	embedders := map[string][]string{}

	var baseName func(ast.Expr) string
	baseName = func(e ast.Expr) string {
		switch t := e.(type) {
		case *ast.Ident:
			return t.Name
		case *ast.SelectorExpr:
			return t.Sel.Name // dao.GenericDialect -> GenericDialect
		case *ast.StarExpr:
			return baseName(t.X)
		}
		return ""
	}

	for _, pf := range files {
		ast.Inspect(pf.file, func(n ast.Node) bool {
			if ts, ok := n.(*ast.TypeSpec); ok {
				if st, ok := ts.Type.(*ast.StructType); ok {
					for _, fld := range st.Fields.List {
						if len(fld.Names) == 0 { // embedded
							if name := baseName(fld.Type); name != "" {
								embedded[name] = true
								embedders[name] = append(embedders[name], ts.Name.Name)
							}
						}
					}
				}
			}
			if fd, ok := n.(*ast.FuncDecl); ok && fd.Recv != nil && len(fd.Recv.List) == 1 {
				rt := baseName(fd.Recv.List[0].Type)
				if rt != "" {
					if methods[rt] == nil {
						methods[rt] = map[string]string{}
					}
					methods[rt][fd.Name.Name] = signatureOf(fd.Type)
				}
			}
			return true
		})
	}

	// 4. Find methods on an embedded base that call a sibling on the receiver.
	var found []selfCall
	for _, pf := range files {
		for _, decl := range pf.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 || fd.Body == nil {
				continue
			}
			base := baseName(fd.Recv.List[0].Type)
			if base == "" || !embedded[base] {
				continue
			}
			// The receiver must be named to be called through.
			if len(fd.Recv.List[0].Names) == 0 {
				continue
			}
			recv := fd.Recv.List[0].Names[0].Name
			if recv == "_" {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok || id.Name != recv {
					return true
				}
				if _, isSibling := methods[base][sel.Sel.Name]; !isSibling {
					return true // not a sibling method of this base
				}
				sc := selfCall{
					file: pf.rel, base: base, caller: fd.Name.Name, callee: sel.Sel.Name,
					line: pf.fset.Position(call.Pos()).Line,
				}
				// LIVE when an embedder overrides the CALLEE but not the CALLER:
				// it runs the base's caller, which reaches the base's callee.
				// An embedder overrides the callee only when its signature
				// MATCHES. A same-name method with a different signature is a
				// wrapper that supplies extra arguments, and the base's
				// self-call cannot resolve to it.
				baseSig := methods[base][sc.callee]
				for _, emb := range embedders[base] {
					embSig, has := methods[emb][sc.callee]
					if !has || embSig != baseSig {
						continue
					}
					if _, overridesCaller := methods[emb][sc.caller]; !overridesCaller {
						sc.live = append(sc.live, emb)
					}
				}
				found = append(found, sc)
				return true
			})
		}
	}

	// 5. Compare to the allowlist, which may only shrink.
	raw, err := os.ReadFile(promotionBudget)
	if err != nil {
		t.Fatalf("read %s: %v", promotionBudget, err)
	}
	allowed := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		allowed[line] = true
	}

	seen := map[string]bool{}
	var unlisted, liveFindings []string
	for _, sc := range found {
		k := sc.key()
		seen[k] = true
		if len(sc.live) > 0 {
			sort.Strings(sc.live)
			liveFindings = append(liveFindings, fmt.Sprintf("%s:%d %s.%s calls %s — %s override %s and inherit %s, so they run the BASE's %s",
				sc.file, sc.line, sc.base, sc.caller, sc.callee,
				strings.Join(sc.live, ", "), sc.callee, sc.caller, sc.callee))
		}
		if !allowed[k] {
			unlisted = append(unlisted, fmt.Sprintf("%s:%d  %s", sc.file, sc.line, k))
		}
	}
	var stale []string
	for k := range allowed {
		if !seen[k] {
			stale = append(stale, k+" — no longer present; delete the line")
		}
	}
	sort.Strings(unlisted)
	sort.Strings(stale)
	sort.Strings(liveFindings)

	if len(liveFindings) > 0 {
		t.Errorf("%d LIVE promotion defect(s) — an embedder is running the base's method today:\n  %s\n"+
			"Take the collaborator as a parameter instead of reaching for the receiver.",
			len(liveFindings), strings.Join(liveFindings, "\n  "))
	}
	if len(unlisted) > 0 {
		t.Errorf("%d new self-call(s) on an embeddable base:\n  %s\n"+
			"A method that calls a sibling on its own receiver ignores every override. "+
			"Pass the collaborator in, as dao.StandardUpsertSuffix does.",
			len(unlisted), strings.Join(unlisted, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("%d allowlist line(s) no longer match:\n  %s\n"+
			"The allowlist is exact — record the improvement so it cannot be undone.",
			len(stale), strings.Join(stale, "\n  "))
	}
	t.Logf("%d self-call site(s) across %d files walked", len(found), len(files))
}
