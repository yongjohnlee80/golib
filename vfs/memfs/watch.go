package memfs

import (
	"context"
	"sync"

	"github.com/yongjohnlee80/golib/vfs"
)

// watcher delivers events to one Watch channel. Mutations queue events while holding FS.mu; a
// goroutine drains the queue, so a slow consumer never blocks a writer and no event is dropped.
type watcher struct {
	dir       string
	recursive bool
	out       chan vfs.Event

	mu      sync.Mutex
	cond    *sync.Cond
	queue   []vfs.Event
	stopped bool
}

// Watch streams events under dir until ctx ends or the filesystem is closed.
func (f *FS) Watch(ctx context.Context, dir string, opts ...vfs.WatchOption) (<-chan vfs.Event, error) {
	if err := f.begin(ctx, "watch", dir, false); err != nil {
		return nil, err
	}
	n, ok := f.nodes[dir]
	if !ok {
		f.mu.Unlock()
		return nil, pathErr("watch", dir, vfs.ErrNotExist)
	}
	if !n.dir {
		f.mu.Unlock()
		return nil, pathErr("watch", dir, errNotDir)
	}
	cfg := vfs.ResolveWatch(opts)
	w := &watcher{dir: dir, recursive: cfg.Recursive, out: make(chan vfs.Event, 64)}
	w.cond = sync.NewCond(&w.mu)
	f.watchers[w] = struct{}{}
	f.mu.Unlock()

	go w.run(ctx)
	go func() {
		<-ctx.Done()
		f.mu.Lock()
		delete(f.watchers, w)
		f.mu.Unlock()
		w.stop()
	}()
	return w.out, nil
}

// emit queues ev for every matching watcher. The caller holds f.mu.
func (f *FS) emit(p string, op vfs.Op) {
	for w := range f.watchers {
		if w.matches(p) {
			w.push(vfs.Event{Path: p, Op: op})
		}
	}
}

func (w *watcher) matches(p string) bool {
	if w.recursive {
		return w.dir == "." || (p != w.dir && under(p, w.dir))
	}
	return p != "." && parentOf(p) == w.dir
}

func (w *watcher) push(ev vfs.Event) {
	w.mu.Lock()
	if !w.stopped {
		w.queue = append(w.queue, ev)
		w.cond.Signal()
	}
	w.mu.Unlock()
}

func (w *watcher) stop() {
	w.mu.Lock()
	w.stopped = true
	w.cond.Broadcast()
	w.mu.Unlock()
}

// run drains the queue into out, in order, until stopped; then closes out.
func (w *watcher) run(ctx context.Context) {
	defer close(w.out)
	for {
		w.mu.Lock()
		for len(w.queue) == 0 && !w.stopped {
			w.cond.Wait()
		}
		if w.stopped {
			w.mu.Unlock()
			return
		}
		ev := w.queue[0]
		w.queue = w.queue[1:]
		w.mu.Unlock()
		select {
		case w.out <- ev:
		case <-ctx.Done():
			return
		}
	}
}
