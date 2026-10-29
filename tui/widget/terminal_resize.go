package widget

import "sync"

// ptyResizer hands one program its sizes off the UI loop, in the order they were asked for, the
// newest winning: one Resize is in flight at a time, and a size asked for meanwhile replaces any
// not yet sent. A drag-resize's storm of sizes then ends at its last size, where a goroutine per
// size could finish in any order and leave the program at a stale one.
//
// A Resize that fails leaves the program at its last size; nothing on the loop can mend it, and
// the next size asked for tries again.
type ptyResizer struct {
	proc TerminalProcess

	mu      sync.Mutex
	next    [2]int // rows, cols: the newest size not yet sent
	pending bool   // next is to be sent
	busy    bool   // a goroutine is sending
}

// resize asks for rows×cols. It returns at once; a goroutine sends it, unless one is already
// sending, which sends it next.
func (r *ptyResizer) resize(rows, cols int) {
	r.mu.Lock()
	r.next, r.pending = [2]int{rows, cols}, true
	start := !r.busy
	r.busy = true
	r.mu.Unlock()
	if start {
		go r.send()
	}
}

// send hands the program each newest size until none is left.
func (r *ptyResizer) send() {
	for {
		r.mu.Lock()
		if !r.pending {
			r.busy = false
			r.mu.Unlock()
			return
		}
		size := r.next
		r.pending = false
		r.mu.Unlock()
		_ = r.proc.Resize(size[0], size[1]) // a failure keeps the last size; see the type's doc
	}
}
