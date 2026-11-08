// Package text is the text model tui/widget's text widgets share: a document's lines (Doc), a
// cursor over them (Buffer), a place in them (Pos), the log of changes to their line structure
// (EditLog, LineEdit), what takes a change back (UndoOp, through a LineRecorder), the soft-wrap
// geometry TextArea and the Editor lay lines out by (WrapView), and what a scrolling widget last
// kept in view (ViewMark).
//
// It is internal: only tui/widget and the packages under it import it, so none of its names is
// public API. tui/widget imports it as txt, since text is a common local name there.
package text
