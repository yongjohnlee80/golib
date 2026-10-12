package gui

import (
	"gioui.org/io/event"
	"gioui.org/io/key"
)

// The Gio side of the platform contract: the only file in the input path that
// names Gio's key package. Events flow in here as InputEvent; TextInputCmds
// flow back out as Gio commands (M3).

// inputEventOf converts one Gio event into the contract's InputEvent. It is
// pure: it reads nothing and writes nothing, so a replayed recording drives
// the same conversion the live window made.
//
// ok is false for what the contract does not model: pointer, clipboard and
// transfer events stay the backend's own in v1.
func inputEventOf(e event.Event) (InputEvent, bool) {
	switch e := e.(type) {
	case key.Event:
		return KeyInput{Name: string(e.Name), Mods: modsOf(e.Modifiers), Press: e.State == key.Press}, true
	case key.EditEvent:
		return TextEdit{Start: e.Range.Start, End: e.Range.End, Text: e.Text}, true
	case key.CompositionEvent:
		return Composition{Start: e.Start, End: e.End}, true
	case key.SelectionEvent:
		return SelectionSet{Start: e.Start, End: e.End}, true
	case key.SnippetEvent:
		return SnippetAsk{Start: e.Start, End: e.End}, true
	case key.FocusEvent:
		return FocusChange{Focused: e.Focus}, true
	}
	return nil, false
}
