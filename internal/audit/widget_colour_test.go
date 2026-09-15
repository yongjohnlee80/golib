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

// TestWidgetDefaultsAndRenderPathsUseSemanticColours protects the theming
// boundary without policing unrelated production code. Constructor/default
// functions and Render methods are the seeds; same-package helpers reachable
// from them are followed. Tests and examples are outside the scanned package.
// The ANSI content parser remains legal unless a widget default or render path
// starts calling it and thereby makes its literal colours presentation policy.
func TestWidgetDefaultsAndRenderPathsUseSemanticColours(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "tui", "widget")
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		base := filepath.Base(path)
		if !strings.HasSuffix(base, ".go") || strings.HasSuffix(base, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		files[rel] = src
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	findings, seeds, err := widgetColourFindings(files)
	if err != nil {
		t.Fatal(err)
	}
	if seeds < 30 {
		t.Fatalf("only %d constructor/default/render seeds found; scan scope silently narrowed", seeds)
	}
	for _, finding := range findings {
		t.Error(finding)
	}
}

type colourFunc struct {
	file       string
	decl       *ast.FuncDecl
	styleNames map[string]bool
}

func widgetColourFindings(files map[string][]byte) ([]string, int, error) {
	funcs := map[string]*colourFunc{}
	for name, src := range files {
		f, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
		if err != nil {
			return nil, 0, fmt.Errorf("parse %s: %w", name, err)
		}
		styleNames := map[string]bool{}
		for _, imp := range f.Imports {
			if strings.Trim(imp.Path.Value, `"`) != "github.com/yongjohnlee80/golib/tui/style" {
				continue
			}
			alias := "style"
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			styleNames[alias] = true
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			funcs[funcKey(fd)] = &colourFunc{file: name, decl: fd, styleNames: styleNames}
		}
	}

	var seeds []string
	for key := range funcs {
		name := methodName(key)
		if name == "Render" || strings.HasPrefix(name, "New") || strings.HasPrefix(name, "Default") {
			seeds = append(seeds, key)
		}
	}
	sort.Strings(seeds)

	seen := map[string]bool{}
	var findings []string
	var walk func(string)
	walk = func(key string) {
		if seen[key] {
			return
		}
		seen[key] = true
		fn := funcs[key]
		if fn == nil {
			return
		}
		for _, call := range directCalls(fn.decl) {
			if fn.styleNames[call.recv] {
				switch call.name {
				case "ANSI", "ANSI256", "RGB":
					findings = append(findings, fn.file+": "+key+" reaches literal style."+call.name)
				}
			}
			if call.recv == "" {
				walk(call.name)
				continue
			}
			// Method receiver types are deliberately over-approximated. That can
			// inspect more same-package helpers, but never code outside the widget
			// package and never tests/examples.
			suffix := "." + call.name
			for candidate := range funcs {
				if strings.HasSuffix(candidate, suffix) {
					walk(candidate)
				}
			}
		}
	}
	for _, seed := range seeds {
		walk(seed)
	}
	sort.Strings(findings)
	return findings, len(seeds), nil
}

// The positive control prevents a refactor from turning the guard into a scan
// that always succeeds because it no longer recognizes the style import or
// constructor seed.
func TestWidgetColourAuditPositiveControl(t *testing.T) {
	files := map[string][]byte{"fixture.go": []byte(`package widget
import "github.com/yongjohnlee80/golib/tui/style"
func NewSample() { paintSample() }
func paintSample() { _ = style.RGB(1, 2, 3) }
`)}
	got, seeds, err := widgetColourFindings(files)
	if err != nil {
		t.Fatal(err)
	}
	if seeds != 1 || len(got) != 1 || !strings.Contains(got[0], "style.RGB") {
		t.Fatalf("positive control: seeds=%d findings=%v", seeds, got)
	}
}
