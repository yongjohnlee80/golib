package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TestEventsOwnerFieldContract asserts that all primary domain bus events
// carry Owner (tui.NodeID) conforming to the package owner invariant.
func TestEventsOwnerFieldContract(t *testing.T) {
	nodeID := tui.NodeID(42)

	submit := widget.SubmitEvent{Owner: nodeID, Value: "v"}
	if submit.Owner != nodeID {
		t.Errorf("SubmitEvent.Owner = %d, want %d", submit.Owner, nodeID)
	}

	yank := widget.YankEvent{Owner: nodeID, ClipboardDelivered: true}
	if yank.Owner != nodeID {
		t.Errorf("YankEvent.Owner = %d, want %d", yank.Owner, nodeID)
	}

	change := widget.ChangeEvent{Owner: nodeID, Value: "v"}
	if change.Owner != nodeID {
		t.Errorf("ChangeEvent.Owner = %d, want %d", change.Owner, nodeID)
	}

	selChanged := widget.SelectionChangedEvent{Owner: nodeID, Index: 1, Label: "l"}
	if selChanged.Owner != nodeID {
		t.Errorf("SelectionChangedEvent.Owner = %d, want %d", selChanged.Owner, nodeID)
	}

	opened := widget.OpenedEvent{Owner: nodeID}
	if opened.Owner != nodeID {
		t.Errorf("OpenedEvent.Owner = %d, want %d", opened.Owner, nodeID)
	}

	closed := widget.ClosedEvent{Owner: nodeID}
	if closed.Owner != nodeID {
		t.Errorf("ClosedEvent.Owner = %d, want %d", closed.Owner, nodeID)
	}

	act := widget.ActivateEvent{Owner: nodeID, Index: 3}
	if act.Owner != nodeID {
		t.Errorf("ActivateEvent.Owner = %d, want %d", act.Owner, nodeID)
	}

	follow := widget.FollowTailChangedEvent{Owner: nodeID, Following: true}
	if follow.Owner != nodeID {
		t.Errorf("FollowTailChangedEvent.Owner = %d, want %d", follow.Owner, nodeID)
	}

	tab := widget.TabChangedEvent{Owner: nodeID, Index: 2, Label: "t"}
	if tab.Owner != nodeID {
		t.Errorf("TabChangedEvent.Owner = %d, want %d", tab.Owner, nodeID)
	}

	split := widget.SplitResizedEvent{Owner: nodeID, Ratio: 0.5}
	if split.Owner != nodeID {
		t.Errorf("SplitResizedEvent.Owner = %d, want %d", split.Owner, nodeID)
	}

	dismiss := widget.DismissEvent{Owner: nodeID}
	if dismiss.Owner != nodeID {
		t.Errorf("DismissEvent.Owner = %d, want %d", dismiss.Owner, nodeID)
	}
}
