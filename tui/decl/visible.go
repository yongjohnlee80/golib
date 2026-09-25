package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// VISIBLE — Qt's Item.visible, on every type.
//
//	TableView { visible: App.resultsAsTable }
//
// A hidden item takes no space, is not painted, is not a tab stop, and hides
// everything under it (golib's tui.Hideable). Bound to a source, it follows
// it; removed by a reload, the item is shown again.

const visibleProp = "visible"

// hideable is a component that can be shown and hidden: every golib widget
// (widget.Base) and container (tui.MultiChild).
type hideable interface {
	SetVisible(bool)
}

func setVisible(typ string, c any, v qml.SpecValue) error {
	on, err := boolOf(v)
	if err != nil {
		return fmt.Errorf("visible: %w", err)
	}
	h, ok := c.(hideable)
	if !ok {
		return fmt.Errorf("a %s cannot be hidden: it takes no place on screen to hide (at %s)", typ, v.Pos)
	}
	h.SetVisible(on)
	return nil
}
