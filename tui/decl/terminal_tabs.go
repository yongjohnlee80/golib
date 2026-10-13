package decl

import (
	"fmt"

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
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("TerminalTabs owns runtime terminals, not declared children (at %s)", b.Pos)
	}
	consumed, err := readProps(b.Props, map[string]field{})
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
	Name:    "TerminalTabs",
	Build:   buildTerminalTabs,
	restyle: restyleTerminalTabs,
	Getters: map[string]Getter{
		"count": func(c tui.Component) (qml.SpecValue, error) {
			return numberValue(len(c.(*widget.TerminalTabs).Tabs())), nil
		},
		"currentId": func(c tui.Component) (qml.SpecValue, error) {
			return strValue(string(c.(*widget.TerminalTabs).Active())), nil
		},
	},
	Setters: map[string]Setter{
		"emptyText":       setter("a TerminalTabs", stringOf, (*widget.TerminalTabs).SetEmptyText),
		"minimumContrast": setter("a TerminalTabs", numberOf, (*widget.TerminalTabs).SetMinimumContrast),
		"cursorColor":     setter("a TerminalTabs", colorOf, (*widget.TerminalTabs).SetCursorColor),
		"vimKeys":         setter("a TerminalTabs", boolOf, (*widget.TerminalTabs).SetVimKeys),
	},
	Signals:   map[string][]string{"addRequested": {}, "menuRequested": {"id"}, "changed": {}},
	Destroyed: func(c tui.Component) { c.(*widget.TerminalTabs).ShutdownWithResult() },
}
