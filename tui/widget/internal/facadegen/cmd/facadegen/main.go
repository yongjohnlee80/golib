// Command facadegen writes tui/widget's facades over the packages its widgets moved into
// (<name>_facade.go) and their vars tests (<name>_facade_vars_test.go), run from tui/widget by go
// generate (facade.go).
package main

import (
	"fmt"
	"os"

	"github.com/yongjohnlee80/golib/tui/widget/internal/facadegen"
)

func main() {
	for _, p := range facadegen.Packages {
		facade, vars, err := facadegen.Generate(p.Dir, p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "facadegen: %s: %v\n", p.Dir, err)
			os.Exit(1)
		}
		for name, src := range map[string][]byte{p.Name + "_facade.go": facade, p.Name + "_facade_vars_test.go": vars} {
			if err := os.WriteFile(name, src, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "facadegen:", err)
				os.Exit(1)
			}
		}
	}
}
