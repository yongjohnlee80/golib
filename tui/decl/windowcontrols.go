package decl

import (
	"fmt"
	"slices"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui/widget/core"
)

// windowControlNames are the words a Drawer's windowControls takes, in the order the controls are
// drawn, left to right: _ + x, as the title bar and a tab strip draw them.
var windowControlNames = []struct {
	name string
	op   core.WindowOperation
}{{"minimize", core.WindowMinimize}, {"maximize", core.WindowMaximize}, {"close", core.WindowClose}}

// windowOpsOf reads windowControls: any of "minimize", "maximize" and "close", separated by spaces,
// each once; "" for none. The controls are drawn in the title bar's order, whatever the order
// written.
func windowOpsOf(v qml.SpecValue) ([]core.WindowOperation, error) {
	s, err := stringOf(v)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, w := range strings.Fields(s) {
		if !slices.ContainsFunc(windowControlNames, func(c struct {
			name string
			op   core.WindowOperation
		}) bool {
			return c.name == w
		}) {
			return nil, fmt.Errorf("windowControls: %q: want minimize, maximize or close (at %s)", w, v.Pos)
		}
		if seen[w] {
			return nil, fmt.Errorf("windowControls: %q twice (at %s)", w, v.Pos)
		}
		seen[w] = true
	}
	var ops []core.WindowOperation
	for _, c := range windowControlNames {
		if seen[c.name] {
			ops = append(ops, c.op)
		}
	}
	return ops, nil
}
