package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
)

// TOOLTIP — Qt's ToolTip.text, on every type: the text a control reveals in a card while the
// pointer rests on it, and when the keyboard brings focus to it (tui/tooltip.go).
//
//	Text { text: App.description; toolTipText: App.description }
//
// Bound to a source, it follows it; "" shows none, and removed by a reload, the item has none. A
// qsTrId message is shown in the App's language.

const toolTipProp = "toolTipText"

// toolTipped is a component that can carry a tooltip: every golib widget (widget.Base).
type toolTipped interface {
	SetToolTip(string)
}

// toolTipMessaged is one that can carry a translated tooltip: widget.Base too.
type toolTipMessaged interface {
	SetToolTipMessage(tui.Message)
}

func setToolTip(typ string, c any, v qml.SpecValue) error {
	text, err := textOf(v)
	if err != nil {
		return fmt.Errorf("%s: %w", toolTipProp, err)
	}
	t, ok := c.(toolTipped)
	if !ok {
		return fmt.Errorf("a %s cannot show a tooltip (at %s)", typ, v.Pos)
	}
	if !text.IsMessage() {
		t.SetToolTip(text.Plain)
		return nil
	}
	m, ok := c.(toolTipMessaged)
	if !ok {
		return fmt.Errorf("a %s cannot show a translated tooltip (at %s)", typ, v.Pos)
	}
	m.SetToolTipMessage(text.Message)
	return nil
}
