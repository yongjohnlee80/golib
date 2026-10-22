package decl

import (
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TerminalTabsOf exposes the typed runtime container of one declared parent.
// Hosts use Add/Select/Start/SendText/Remove on it, not arbitrary child QML IDs.
// Look up the parent again after reload, as with EditorOf: a rebuilt parent has
// new runtime identities and destroys its previous processes observably.
func TerminalTabsOf(c tui.Component) (*widget.TerminalTabs, bool) {
	tabs, ok := c.(*widget.TerminalTabs)
	return tabs, ok
}

func buildTerminalTabs(b Build) (tui.Component, []string, error) {
	consumed, err := b.ReadProps(map[string]field{})
	if err != nil {
		return nil, nil, err
	}
	tabs := widget.NewTerminalTabs()
	add, menu, changed := b.EmitterWith("addRequested"), b.EmitterWith("menuRequested"), b.EmitterWith("changed")
	tabs.SetActions(func() { add() }, func(id widget.TerminalTabID) { menu(strValue(string(id))) })
	tabs.SetOnChanged(func() { changed() })
	return tabs, consumed, nil
}

func restyleTerminalTabs(c tui.Component, p palette) {
	tabs := c.(*widget.TerminalTabs)
	tabs.SetStyles(p.look(roleWindow, roleWindowText), p.look(roleButton, roleButtonText), p.look(roleHighlight, roleHighlightedText))
	tabs.SetDefaultLook(p.look(roleBase, roleText))
}

var terminalTabsType = Type{
	Name: "TerminalTabs", Children: NoChildren().Because("it owns runtime terminals, not declared children"),
	Build:   buildTerminalTabs,
	restyle: restyleTerminalTabs,
	Getters: map[string]Getter{
		"count": getter(func(t *widget.TerminalTabs) (qml.SpecValue, error) {
			return numberValue(len(t.Tabs())), nil
		}),
		"currentId": getter(func(t *widget.TerminalTabs) (qml.SpecValue, error) {
			return strValue(string(t.Active())), nil
		}),
	},
	Setters: map[string]Setter{
		"emptyText":       setter("a TerminalTabs", stringOf, (*widget.TerminalTabs).SetEmptyText),
		"minimumContrast": setter("a TerminalTabs", numberOf, (*widget.TerminalTabs).SetMinimumContrast),
		"cursorColor":     setter("a TerminalTabs", colorOf, (*widget.TerminalTabs).SetCursorColor),
		"vimKeys":         setter("a TerminalTabs", boolOf, (*widget.TerminalTabs).SetVimKeys),
		// golib's: the family the tabs' cells are drawn in, at the window's cell size; "" the window's
		"cellFont": setter("a TerminalTabs", stringOf, (*widget.TerminalTabs).SetCellFace),
		// golib's: keys their programs never see, left to the Window's Shortcuts (hostkeys.go)
		"hostKeys": setter("a TerminalTabs", hostKeysOf, (*widget.TerminalTabs).SetHostKeys),
		// golib's: the key that toggles their input and normal mode (hostkeys.go)
		"modeKey": setter("a TerminalTabs", modeKeyOf, (*widget.TerminalTabs).SetModeKey),
	},
	Signals:   map[string][]string{"addRequested": {}, "menuRequested": {"id"}, "changed": {}},
	Destroyed: destroyer(func(t *widget.TerminalTabs) { t.ShutdownWithResult() }),
}
