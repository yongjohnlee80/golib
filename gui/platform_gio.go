package gui

import (
	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
)

// The Gio side of the platform contract: the only file in the input path that
// names Gio's key package. Events flow in here as InputEvent; TextInputCmds
// flow back out as Gio commands.

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

// execTextInputCmd executes one TextInputCmd as the Gio command the toolkit
// takes, on the window's input source and addressed to its input tag. It is
// the only place a TextInputCmd becomes a Gio command; the backend above it
// speaks the contract alone. The zero values convert to the commands Gio takes
// for them: a SnippetReply{} is the empty snippet it is reset with, and a
// SelectionTell{} the selection reset with no caret.
func execTextInputCmd(src eventSource, tag event.Tag, cmd TextInputCmd) {
	switch cmd := cmd.(type) {
	case SnippetReply:
		src.Execute(key.SnippetCmd{Tag: tag, Snippet: key.Snippet{Range: key.Range{Start: cmd.Start, End: cmd.End}, Text: cmd.Text}})
	case SelectionTell:
		src.Execute(key.SelectionCmd{
			Tag:   tag,
			Range: key.Range{Start: cmd.Start, End: cmd.End},
			Caret: key.Caret{Pos: f32.Pt(cmd.Caret.X, cmd.Caret.Y), Ascent: cmd.Caret.Ascent, Descent: cmd.Caret.Descent},
		})
	case KeyboardShow:
		src.Execute(key.SoftKeyboardCmd{Show: true})
	case FocusTake:
		src.Execute(key.FocusCmd{Tag: tag})
	}
}
