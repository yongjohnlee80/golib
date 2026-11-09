// Command facadegen writes tui/widget's facade over tui/widget/core (core_facade.go) and its vars
// test (core_facade_vars_test.go), run from tui/widget by go generate (facade.go).
package main

import (
	"fmt"
	"os"

	"github.com/yongjohnlee80/golib/tui/widget/internal/facadegen"
)

func main() {
	facade, vars, err := facadegen.Generate("core")
	if err != nil {
		fmt.Fprintln(os.Stderr, "facadegen:", err)
		os.Exit(1)
	}
	for name, src := range map[string][]byte{"core_facade.go": facade, "core_facade_vars_test.go": vars} {
		if err := os.WriteFile(name, src, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "facadegen:", err)
			os.Exit(1)
		}
	}
}
