package widget

// pressPairer is a view's pointer presses on its rows: it pairs a double-click with the thing
// pressed, not the cell, and it knows when the rows a press named are gone. The App counts presses on
// one screen CELL in time (MouseEvent.Count), but a view that scrolls or reorders between the two
// presses puts a different row under that cell, so each view keys its presses by its own identity:
// a List by its row index, a Tree by its node, a DataTable by its source row and field.
//
// A press pairs when its count is EXACTLY 2 and it lands on what the previous press did: three
// presses would otherwise activate twice, on counts 2 and 3, while a double-click is one activation.
//
// A view's rows change meaning when it is given a new source or its rows change in place, and the
// view calls Reset then. That can happen in the middle of a press: the press moves the cursor, and
// the owner told of the move (WithListOnCursor, TableOnCursor) may replace the rows there and then.
// Every press therefore goes through Press or Survives, which run the press's first step and report
// whether the rows outlived it. A press whose rows are gone does nothing more: no pair, no
// activation, no menu. This is the one place that rule lives.
type pressPairer[K comparable] struct {
	last  K
	has   bool
	epoch uint64 // Resets so far: the rows' identity, as a press saw it
}

// Survives runs a press's first step, moved, and reports whether the rows are still the ones pressed:
// false when moved led to a Reset (the owner replaced or refreshed them).
func (p *pressPairer[K]) Survives(moved func()) bool {
	epoch := p.epoch
	if moved != nil {
		moved()
	}
	return p.epoch == epoch
}

// Press is a press on k, with the App's count: it runs moved as Survives does and, when the rows
// survived, records the press and reports whether it completes a double-click. live is false when the
// rows are gone; the press is then not recorded, and paired is false.
func (p *pressPairer[K]) Press(count int, k K, moved func()) (live, paired bool) {
	if !p.Survives(moved) {
		return false, false
	}
	paired = count == 2 && p.has && p.last == k
	p.last, p.has = k, true
	return true, paired
}

// Reset forgets the previous press and starts a new epoch. A view calls it when its identities change
// meaning (a new source, rows mutated in place), so a press before cannot pair with one after, and a
// press in progress learns its rows are gone; a pointer key is also released for the collector.
func (p *pressPairer[K]) Reset() { *p = pressPairer[K]{epoch: p.epoch + 1} }
