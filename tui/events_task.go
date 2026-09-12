package tui

// TaskID identifies one App.Go task. Monotonic per App; never reused
// (staleness checks).
type TaskID uint64

// --- addressed deliveries. these go to exactly one node and do not bubble:
//     the addressee asked for the work, so an ancestor seeing its result would
//     be seeing someone else's mail. ---

// TaskResult is the terminal outcome of an App.Go task, addressed to the
// owning component. Err wraps the task's panic when it panicked.
type TaskResult struct {
	Owner NodeID
	ID    TaskID
	Value any
	Err   error
}

func (TaskResult) isEvent() {}

// TaskProgress is an intermediate, addressed update from a still-running
// task that has not finished. Routed exactly like TaskResult.
type TaskProgress struct {
	Owner NodeID
	ID    TaskID
	Value any
}

func (TaskProgress) isEvent() {}
