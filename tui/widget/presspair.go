package widget

// pressPairer pairs a double-click with the thing pressed, not the cell. The App counts presses on
// one screen CELL in time (MouseEvent.Count), but a view that scrolls or reorders between the two
// presses puts a different row under that cell, so each view keys its presses by its own identity:
// a List by its row index, a Tree by its node, a DataTable by its source row and field.
//
// A press pairs when its count is EXACTLY 2 and it lands on what the previous press did: three
// presses would otherwise activate twice, on counts 2 and 3, while a double-click is one activation.
type pressPairer[K comparable] struct {
	last K
	has  bool
}

// Pair records a press on k with the App's count, and reports whether it completes a double-click.
func (p *pressPairer[K]) Pair(count int, k K) bool {
	paired := count == 2 && p.has && p.last == k
	p.last, p.has = k, true
	return paired
}

// Reset forgets the previous press. A view calls it when its identities change meaning (a new
// source, rows mutated in place), so a press before cannot pair with one after; a pointer key is
// also released for the collector.
func (p *pressPairer[K]) Reset() { *p = pressPairer[K]{} }
