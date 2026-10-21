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
	rowsEpoch // Reset advances it: the rows' identity, as a press saw it
	last      K
	has       bool
}

// rowsEpoch is a view's row identity over time: the one authority a view consults to learn whether
// the rows an operation began on are still there. advance starts a new epoch (a new source, rows
// changed in place, a new listing); Survives runs a step that may tell an owner, who may advance it
// there and then. pressPairer holds one for the views that pair presses (List, Tree, TableCore);
// a view that pairs none (FileList) holds its own.
type rowsEpoch struct{ n uint64 }

// advance starts a new epoch: the rows an operation in progress began on are gone.
func (e *rowsEpoch) advance() { e.n++ }

// Survives runs step and reports whether the rows are still the ones it began on: false when step
// advanced the epoch (an owner it told replaced or refreshed them). A nil step survives.
func (e *rowsEpoch) Survives(step func()) bool {
	n := e.n
	if step != nil {
		step()
	}
	return e.n == n
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
func (p *pressPairer[K]) Reset() { *p = pressPairer[K]{rowsEpoch: rowsEpoch{n: p.n + 1}} }
