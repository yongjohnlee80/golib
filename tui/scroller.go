package tui

// Scroller reports a scrolling view's position, in rows: offset is the first visible row,
// viewport the rows shown, content the rows there are. A backend draws a scrollbar from it.
type Scroller interface {
	Component
	ScrollState() (offset, viewport, content int)
}
