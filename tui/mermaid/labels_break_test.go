package mermaid

import (
	"strings"
	"testing"
)

// A <br> in a label is a line break the parser keeps ("\n"), and a label in cells is one row: the
// break reads as a space, so the words on either side stay apart in a flowchart's node, on its edge
// and in a mindmap's node. It was measured as nothing and drawn as nothing: "effectivenessand".
func TestALabelsLineBreakReadsAsASpace(t *testing.T) {
	for name, src := range map[string]string{
		"a flowchart node": "flowchart LR\n  A[\"On effectiveness<br/>and features\"] --> B[Done]\n",
		"a flowchart edge": "flowchart LR\n  A[Start] -->|On effectiveness<br>and features| B[Done]\n",
		"a mindmap node":   "mindmap\n  root((Research))\n    On effectiveness<br/>and features\n    Tools\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, got := paint(t, src)
			if !strings.Contains(got, "On effectiveness and features") || strings.Contains(got, "effectivenessand") {
				t.Errorf("the label's break does not read as a space:\n%s", got)
			}
		})
	}
}
