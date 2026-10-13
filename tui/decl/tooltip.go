package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// TOOLTIP — Qt's ToolTip.text, on every type: the text a control reveals in a card while the
// pointer rests on it, and when the keyboard brings focus to it (tui/tooltip.go).
//
//	Text { text: App.description; toolTipText: App.description }
//
// Bound to a source, it follows it; "" shows none, and removed by a reload, the item has none.

const toolTipProp = "toolTipText"

// toolTipped is a component that can carry a tooltip: every golib widget (widget.Base).
type toolTipped interface {
	SetToolTip(string)
}

func setToolTip(typ string, c any, v qml.SpecValue) error {
	text, err := stringOf(v)
	if err != nil {
		return fmt.Errorf("%s: %w", toolTipProp, err)
	}
	t, ok := c.(toolTipped)
	if !ok {
		return fmt.Errorf("a %s cannot show a tooltip (at %s)", typ, v.Pos)
	}
	t.SetToolTip(text)
	return nil
}
