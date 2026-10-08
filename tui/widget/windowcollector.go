package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"slices"
)

// MinimizedWindow is a value descriptor for one originating window mount.
// Applications use Restore/Close; controls use InvokeInput to preserve confinement.
type MinimizedWindow struct {
	Owner tui.NodeID
	Key   string
	Title string
	core  *WindowCore
}

func (e MinimizedWindow) live() bool {
	return e.core != nil && e.core.live() && e.core.owner == e.Owner
}

// Restore is an application-owned restore, not restricted by an input modal.
func (e MinimizedWindow) Restore() bool { return e.live() && e.core.Restore() }

// Close is an application-owned close using the configured window policy.
func (e MinimizedWindow) Close() bool { return e.live() && e.core.Close() }

// InvokeInput checks the target window's node, not the source control's node.
func (e MinimizedWindow) InvokeInput(a tui.Action) bool { return e.live() && e.core.InvokeInput(a) }

// WindowCollector is a loop-owned model seam; Put replaces a mount and Remove is
// idempotent. Implementations do not fail or perform window/UI operations inline.
type WindowCollector interface {
	Put(MinimizedWindow)
	Remove(tui.NodeID)
}

// MinimizedWindows is an ordered, presentation-free collector. Its zero value
// is usable. All model access and observations belong to the application loop.
type MinimizedWindows struct {
	entries   []MinimizedWindow
	observers map[uint64]func()
	next      uint64
}

// NewMinimizedWindows creates a collector; it creates no taskbar or global state.
func NewMinimizedWindows() *MinimizedWindows { return &MinimizedWindows{} }

// Entries returns a detached value snapshot in insertion order.
func (m *MinimizedWindows) Entries() []MinimizedWindow { return slices.Clone(m.entries) }

// Put inserts or replaces one live-mount descriptor.
func (m *MinimizedWindows) Put(e MinimizedWindow) {
	for i, old := range m.entries {
		if old.Owner == e.Owner {
			m.entries[i] = e
			m.notifyWindowEntries()
			return
		}
	}
	m.entries = append(m.entries, e)
	m.notifyWindowEntries()
}

// Remove forgets a mount, and does nothing when it is already absent.
func (m *MinimizedWindows) Remove(owner tui.NodeID) {
	for i, e := range m.entries {
		if e.Owner == owner {
			m.entries = slices.Delete(m.entries, i, i+1)
			m.notifyWindowEntries()
			return
		}
	}
}

// Observe subscribes a loop-owned callback and returns an idempotent unsubscribe.
func (m *MinimizedWindows) Observe(fn func()) func() {
	if fn == nil {
		return func() {}
	}
	if m.observers == nil {
		m.observers = make(map[uint64]func())
	}
	m.next++
	id := m.next
	m.observers[id] = fn
	return func() { delete(m.observers, id) }
}

func (m *MinimizedWindows) notifyWindowEntries() {
	callbacks := make([]func(), 0, len(m.observers))
	for _, fn := range m.observers {
		callbacks = append(callbacks, fn)
	}
	for _, fn := range callbacks {
		fn()
	}
}
