package widget_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/internal/facadegen"
)

// TestTheFacadeIsGenerated: core_facade.go and its vars test are what facadegen writes from
// tui/widget/core now, so a name core exports later cannot go unforwarded, nor a forward outlive
// its name. Run go generate in tui/widget when it fails.
func TestTheFacadeIsGenerated(t *testing.T) {
	facade, vars, err := facadegen.Generate("core")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{"core_facade.go": facade, "core_facade_vars_test.go": vars} {
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is not what facadegen writes from tui/widget/core now: run go generate in tui/widget", name)
		}
	}
}
