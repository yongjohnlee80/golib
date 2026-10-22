package decltest

import (
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// Every standard type has a minimal declaration, none is kept for a type that is not one, and each
// mounts under the standard vocabulary with its subject: the fixture CheckStyle mounts a style's
// replacement in is true of the types it stands for.
func TestEveryStandardTypeHasAMinimalDeclaration(t *testing.T) {
	standard := map[string]bool{}
	for _, name := range tuidecl.New(tuidecl.StdRegistry(), tuidecl.StdProperties()...).TypeNames() {
		if _, ok := tuidecl.StandardContract(name); ok {
			standard[name] = true
		}
	}
	if len(standard) == 0 {
		t.Fatal("no standard types")
	}
	for name := range standard {
		m, ok := minimals[name]
		if !ok {
			t.Errorf("%s has no minimal declaration", name)
			continue
		}
		p, err := mountMinimal(name, m)
		if err != nil {
			t.Errorf("%s's minimal declaration does not mount: %v", name, err)
			continue
		}
		if _, ok := p.Tree().NodeByID("subject"); !ok {
			t.Errorf("%s's minimal declaration has no subject", name)
		}
		if err := p.Tree().Destroy(); err != nil {
			t.Errorf("%s: destroying it: %v", name, err)
		}
	}
	for name := range minimals {
		if !standard[name] {
			t.Errorf("a minimal declaration is kept for %s, which is no standard type", name)
		}
	}
}
