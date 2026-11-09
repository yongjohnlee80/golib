package widget_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/internal/facadegen"
)

// TestTheFacadeIsGenerated: each <name>_facade.go and its vars test are what facadegen writes from
// its package now, so a name a package exports later cannot go unforwarded, nor a forward outlive
// its name. Run go generate in tui/widget when it fails.
func TestTheFacadeIsGenerated(t *testing.T) {
	for _, p := range facadegen.Packages {
		facade, vars, err := facadegen.Generate(p.Dir, p)
		if err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string][]byte{p.Name + "_facade.go": facade, p.Name + "_facade_vars_test.go": vars} {
			got, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s is not what facadegen writes from %s now: run go generate in tui/widget", name, p.Dir)
			}
		}
	}
}
