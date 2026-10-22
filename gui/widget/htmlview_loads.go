package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// loadHost is what a page's loads need of their view: the loop a load reports to, the resolver
// that reads a src, and where a refused src is reported. An HTMLView is one; a test can be.
type loadHost interface {
	Context() *tui.Context
	Images() tuiwidget.ImageResolver
	Refused(src string)
}

var _ loadHost = (*tuiwidget.HTMLView)(nil)

// loads is what an HTMLView keeps of the things a page loads off the loop (its images, its linked
// stylesheets): the entry for each key, the loads in flight, and those started before the last
// reset, whose results are dropped when they land.
type loads[E any] struct {
	entries map[string]*E
	tasks   map[tui.TaskID]string
	stale   map[tui.TaskID]struct{}
}

func newLoads[E any]() loads[E] {
	return loads[E]{entries: map[string]*E{}, tasks: map[tui.TaskID]string{}, stale: map[tui.TaskID]struct{}{}}
}

// reset forgets every entry and every load on its way: a new resolver reads a key as another file,
// so neither an entry nor a load still in flight under the old one may be kept.
func (t *loads[E]) reset() {
	for id := range t.tasks {
		t.stale[id] = struct{}{}
	}
	t.entries, t.tasks = map[string]*E{}, map[tui.TaskID]string{}
}

// take resolves a task's result to its load, and forgets the task: owned is false for a result
// that is no load of these; stale is true for one started before the last reset, which is dropped.
// Otherwise key is the load's, and e its entry (nil if it is gone).
func (t *loads[E]) take(id tui.TaskID) (key string, e *E, stale, owned bool) {
	if _, ok := t.stale[id]; ok {
		delete(t.stale, id)
		return "", nil, true, true
	}
	key, ok := t.tasks[id]
	if !ok {
		return "", nil, false, false
	}
	delete(t.tasks, id)
	return key, t.entries[key], false, true
}
