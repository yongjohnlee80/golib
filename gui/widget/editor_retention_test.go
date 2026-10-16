package widget

import (
	"runtime"
	"testing"
	"weak"
)

// A dropped editor is collected, its tab strip with it: the panel-bar registry (gui.OnPanelBar) keys
// the strip weakly, and its colour callback holds the panel weakly too. A callback holding the editor
// kept the strip, its own weak key, reachable from the global registry, so no editor was ever
// collected (Lector, golib #347 post-merge).
func TestADroppedEditorIsCollected(t *testing.T) {
	e := NewEditor()
	ed, strip := weak.Make(e), weak.Make(e.Tabs())
	e = nil
	for i := 0; i < 20 && (ed.Value() != nil || strip.Value() != nil); i++ {
		runtime.GC()
	}
	if ed.Value() != nil || strip.Value() != nil {
		t.Fatalf("a dropped editor is still reachable (editor %v, strip %v): something global keeps it",
			ed.Value() != nil, strip.Value() != nil)
	}
}
