package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// TestEventsOwnerFieldContract asserts that all primary domain bus events
// carry Owner (tui.NodeID) conforming to the package owner invariant.
func TestEventsOwnerFieldContract(t *testing.T) {
	nodeID := tui.NodeID(42)

	submit := core.SubmitEvent{Owner: nodeID, Value: "v"}
	if submit.Owner != nodeID {
		t.Errorf("SubmitEvent.Owner = %d, want %d", submit.Owner, nodeID)
	}

	yank := editor.YankEvent{Owner: nodeID, ClipboardDelivered: true}
	if yank.Owner != nodeID {
		t.Errorf("YankEvent.Owner = %d, want %d", yank.Owner, nodeID)
	}

	change := core.ChangeEvent{Owner: nodeID, Value: "v"}
	if change.Owner != nodeID {
		t.Errorf("ChangeEvent.Owner = %d, want %d", change.Owner, nodeID)
	}

	selChanged := core.SelectionChangedEvent{Owner: nodeID, Index: 1, Label: "l"}
	if selChanged.Owner != nodeID {
		t.Errorf("SelectionChangedEvent.Owner = %d, want %d", selChanged.Owner, nodeID)
	}

	opened := core.OpenedEvent{Owner: nodeID}
	if opened.Owner != nodeID {
		t.Errorf("OpenedEvent.Owner = %d, want %d", opened.Owner, nodeID)
	}

	closed := core.ClosedEvent{Owner: nodeID}
	if closed.Owner != nodeID {
		t.Errorf("ClosedEvent.Owner = %d, want %d", closed.Owner, nodeID)
	}

	act := core.ActivateEvent{Owner: nodeID, Index: 3}
	if act.Owner != nodeID {
		t.Errorf("ActivateEvent.Owner = %d, want %d", act.Owner, nodeID)
	}

	follow := core.FollowTailChangedEvent{Owner: nodeID, Following: true}
	if follow.Owner != nodeID {
		t.Errorf("FollowTailChangedEvent.Owner = %d, want %d", follow.Owner, nodeID)
	}

	tab := core.TabChangedEvent{Owner: nodeID, Index: 2, Label: "t"}
	if tab.Owner != nodeID {
		t.Errorf("TabChangedEvent.Owner = %d, want %d", tab.Owner, nodeID)
	}

	split := core.SplitResizedEvent{Owner: nodeID, Ratio: 0.5}
	if split.Owner != nodeID {
		t.Errorf("SplitResizedEvent.Owner = %d, want %d", split.Owner, nodeID)
	}
}
