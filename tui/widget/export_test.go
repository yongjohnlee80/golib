package widget

// Test-only seams. Compiled into the package's test binary and invisible to
// consumers, so proving an internal contract costs no public API.

// SetWriterBudgetForTest shrinks the pending-byte budget of v's writer handle.
//
// The blocking contract is scale-free — Write blocks once pending would exceed
// the budget, whatever the budget is — but the COST of proving it is not.
// Filling the production 256 KiB budget queues nine or more 32 KiB chunks, and
// every queued chunk is an app.Update the loop still has to ingest and render
// while the app shuts down. Measured on one CPU under -race, that backlog costs
// ~731ms to drain at 512 KiB and ~5.0s at 2 MiB, which is how
// TestBufferViewBoundedPending came to fail against the harness's fixed 5s
// shutdown budget on a loaded CI runner while passing locally every time.
//
// Shrinking the budget removes that floor instead of trading one wall-clock
// guess for a larger one. The production default stays pinned by
// WriterBudgetDefaultForTest.
//
// Call before mounting: the handle reads its budget on the writing goroutine,
// so changing it under a live writer would be a race rather than a fixture.
func SetWriterBudgetForTest(v *BufferView, n int) {
	v.wr.mu.Lock()
	defer v.wr.mu.Unlock()
	v.wr.budget = n
}

// WriterBudgetDefaultForTest is the production pending-byte budget, exposed so
// a test can assert the shipped value rather than restate the number in a
// comment. Without it, shrinking the budget in one test would leave nothing
// asserting what consumers actually get.
const WriterBudgetDefaultForTest = writerBudget

// WriterChunkForTest is the enqueue granularity, exposed so a test can compute
// how many chunks a given write produces instead of hard-coding a count that
// silently stops matching if the granularity changes.
const WriterChunkForTest = writerChunk

// WriterBudgetOfForTest reports the budget v's handle is actually using.
//
// Needed so the default can be asserted on a real view rather than on the
// constant alone: comparing writerBudget to 256<<10 only proves the constant
// equals itself, and would stay green if newBufWriter stopped reading it.
func WriterBudgetOfForTest(v *BufferView) int {
	v.wr.mu.Lock()
	defer v.wr.mu.Unlock()
	return v.wr.budget
}
