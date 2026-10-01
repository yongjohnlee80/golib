//go:build darwin && cgo

package local

/*
#cgo LDFLAGS: -framework CoreServices
#include <CoreServices/CoreServices.h>
#include <dispatch/dispatch.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdlib.h>
#include <sys/param.h>

extern void golibVFSFSEvents(uintptr_t info, size_t n, char **paths, FSEventStreamEventFlags *flags);

static void golib_vfs_fsevents_cb(ConstFSEventStreamRef s, void *info, size_t n, void *paths,
		const FSEventStreamEventFlags flags[], const FSEventStreamEventId ids[]) {
	golibVFSFSEvents((uintptr_t)info, n, (char **)paths, (FSEventStreamEventFlags *)flags);
}

// golib_vfs_stream_create makes a stream over one path: file-level events, the root watched, the
// first event of a quiet spell delivered at once (NoDefer). It returns NULL on failure.
static FSEventStreamRef golib_vfs_stream_create(const char *path, uintptr_t info, double latency) {
	CFStringRef p = CFStringCreateWithCString(NULL, path, kCFStringEncodingUTF8);
	if (p == NULL) {
		return NULL;
	}
	CFArrayRef paths = CFArrayCreate(NULL, (const void **)&p, 1, &kCFTypeArrayCallBacks);
	CFRelease(p);
	if (paths == NULL) {
		return NULL;
	}
	FSEventStreamContext ctx = {0, (void *)info, NULL, NULL, NULL};
	FSEventStreamRef s = FSEventStreamCreate(NULL, golib_vfs_fsevents_cb, &ctx, paths,
		kFSEventStreamEventIdSinceNow, latency,
		kFSEventStreamCreateFlagFileEvents | kFSEventStreamCreateFlagWatchRoot | kFSEventStreamCreateFlagNoDefer);
	CFRelease(paths);
	return s;
}

static dispatch_queue_t golib_vfs_queue_create(void) {
	return dispatch_queue_create("golib.vfs.watch", DISPATCH_QUEUE_SERIAL);
}

static void golib_vfs_queue_release(dispatch_queue_t q) {
	dispatch_release(q);
}

static void golib_vfs_noop(void *ctx) {}

// golib_vfs_drain waits out a callback still running on the serial queue: an empty task queued
// behind it runs only once it has returned. Never called from the queue itself.
static void golib_vfs_drain(dispatch_queue_t q) {
	dispatch_sync_f(q, NULL, golib_vfs_noop);
}

// golib_vfs_getpath is fcntl(F_GETPATH), which cgo cannot call directly (fcntl is variadic).
static int golib_vfs_getpath(int fd, char *buf) {
	return fcntl(fd, F_GETPATH, buf);
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"runtime/cgo"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/vfs"
)

var _ vfs.Watcher = (*FS)(nil)

const (
	// fseLatency is how long FSEvents coalesces before a callback: quick enough to feel immediate,
	// long enough to merge a burst.
	fseLatency = 0.05
	// fseQueueBoundDefault is how many records wait for a stalled consumer before the watch drops them and
	// reports one OpOverflow{""} instead — about 1 MB, enough for a burst outside skipped directories.
	fseQueueBoundDefault = 16384
)

// fseQueueBound is fseQueueBoundDefault; tests lower it to stall a consumer cheaply.
var fseQueueBound atomic.Int64

func init() { fseQueueBound.Store(fseQueueBoundDefault) }

// fseFault, when set by this package's tests, is consulted before the stream's "create" and "start";
// a non-nil return makes that step fail. Production code never sets it.
var fseFault atomic.Pointer[func(op string) error]

// fseHandles counts the watches registered in the cgo handle table, so tests can prove a failed or
// ended watch leaves none behind.
var fseHandles atomic.Int64

func fseFailing(op string) error {
	if h := fseFault.Load(); h != nil {
		return (*h)(op)
	}
	return nil
}

// Watch streams change events under dir from FSEvents until ctx ends or the FS closes; the channel
// then closes. One stream covers the whole tree — no descriptor per directory, so no ceiling on how
// many directories a root may hold. With [vfs.Recursive] every subdirectory is covered, including ones
// created or moved in later; a directory that appears also yields an OpOverflow for its subtree, since
// a populated directory moved in reports only itself.
//
// FSEvents' flags accumulate per path, so presence is decided by an lstat when the event is handled:
// a write to a recently created file may arrive as OpCreate, which consumers treat like OpWrite. Two
// writes within the coalescing window (50 ms) are one event. A dropped-events report from FSEvents,
// or a consumer that falls more than 16 384 records behind, yields an OpOverflow; the watched
// directory itself deleted or moved yields a final OpOverflow{""} before the close.
//
// With [vfs.SkipDirs], nothing under a skipped directory is reported, and its own appearance is never
// an OpOverflow. FSEvents does not see changes made by another machine to a network volume.
//
// Built only with cgo: a macOS build without it has no Watch, and callers take [vfs.Poll].
func (f *FS) Watch(ctx context.Context, dir string, opts ...vfs.WatchOption) (<-chan vfs.Event, error) {
	cfg := vfs.ResolveWatch(opts)
	if err := f.enter(ctx, "watch", dir, false); err != nil {
		return nil, err
	}
	defer f.leave()
	st, err := f.lstat(dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, &fs.PathError{Op: "watch", Path: dir, Err: errNotDir}
	}
	real, err := f.canonical(dir)
	if err != nil {
		return nil, &fs.PathError{Op: "watch", Path: dir, Err: err}
	}
	w := &fseWatch{
		f:   f,
		out: make(chan vfs.Event),
		m: fseMapper{realDir: real, dir: dir, recursive: cfg.Recursive, cfg: cfg,
			stat: func(rel string) presence { return f.presence(rel) }},
		wake: make(chan struct{}, 1),
	}
	if err := w.start(real); err != nil {
		return nil, &fs.PathError{Op: "watch", Path: dir, Err: err}
	}
	go w.run(ctx)
	return w.out, nil
}

// canonical is dir's path as the kernel names it — /private/var, not /var; the volume's own case —
// taken with F_GETPATH from the directory the jail opened, so no pathname is resolved again after the
// jail checked it — a directory swapped for a symlink in between cannot move the watch outside.
func (f *FS) canonical(dir string) (string, error) {
	d, err := f.root.Open(dir)
	if err != nil {
		return "", err
	}
	defer d.Close()
	buf := make([]byte, C.MAXPATHLEN)
	if r, err := C.golib_vfs_getpath(C.int(d.Fd()), (*C.char)(unsafe.Pointer(&buf[0]))); r == -1 {
		return "", fmt.Errorf("F_GETPATH: %w", err)
	}
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0]))), nil
}

// presence lstats rel through the jail.
func (f *FS) presence(rel string) presence {
	st, err := f.lstat(rel)
	switch {
	case err == nil && st.IsDir():
		return presentDir
	case err == nil:
		return presentFile
	case errors.Is(err, fs.ErrNotExist):
		return absent
	}
	return unknown
}

// fseWatch is one Watch call: one FSEvents stream on a private serial queue. The callback appends to
// pending under mu and signals wake; run maps and sends.
type fseWatch struct {
	f      *FS
	m      fseMapper
	out    chan vfs.Event
	handle cgo.Handle
	stream C.FSEventStreamRef
	queue  C.dispatch_queue_t

	mu         sync.Mutex
	pending    []fsevent
	overflowed bool // pending was dropped for a stalled consumer: report one OpOverflow{""}
	wake       chan struct{}
}

// start creates, schedules and starts the stream. On failure it unwinds everything it made, in
// reverse order, so nothing native leaks and nothing stays in the handle table.
func (w *fseWatch) start(real string) error {
	w.handle = cgo.NewHandle(w)
	fseHandles.Add(1)
	path := C.CString(real)
	defer C.free(unsafe.Pointer(path))
	if err := fseFailing("create"); err == nil {
		w.stream = C.golib_vfs_stream_create(path, C.uintptr_t(w.handle), C.double(fseLatency))
	}
	if w.stream == nil {
		w.deleteHandle()
		return errors.New("FSEventStreamCreate failed")
	}
	w.queue = C.golib_vfs_queue_create()
	C.FSEventStreamSetDispatchQueue(w.stream, w.queue)
	started := fseFailing("start") == nil && C.FSEventStreamStart(w.stream) != 0
	if !started {
		// scheduled but never started: no callback can have run
		C.FSEventStreamInvalidate(w.stream)
		C.FSEventStreamRelease(w.stream)
		C.golib_vfs_queue_release(w.queue)
		w.deleteHandle()
		return errors.New("FSEventStreamStart failed")
	}
	return nil
}

func (w *fseWatch) deleteHandle() {
	w.handle.Delete()
	fseHandles.Add(-1)
}

//export golibVFSFSEvents
func golibVFSFSEvents(info C.uintptr_t, n C.size_t, paths **C.char, flags *C.FSEventStreamEventFlags) {
	w := cgo.Handle(info).Value().(*fseWatch)
	ps := unsafe.Slice(paths, int(n))
	fl := unsafe.Slice(flags, int(n))
	batch := make([]fsevent, len(ps))
	for i := range ps {
		batch[i] = fsevent{path: C.GoString(ps[i]), flags: uint32(fl[i])}
	}
	w.mu.Lock()
	if !w.overflowed && int64(len(w.pending)+len(batch)) <= fseQueueBound.Load() {
		w.pending = append(w.pending, batch...)
	} else {
		w.pending, w.overflowed = nil, true // the consumer re-reads everything: a later record adds nothing
	}
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// take returns what the callback queued, and whether some was dropped.
func (w *fseWatch) take() ([]fsevent, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	batch, over := w.pending, w.overflowed
	w.pending, w.overflowed = nil, false
	return batch, over
}

func (w *fseWatch) run(ctx context.Context) {
	defer w.release()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.f.done:
			return
		case <-w.wake:
		}
		batch, over := w.take()
		var events []vfs.Event
		if over {
			events = append(events, vfs.Event{Path: "", Op: vfs.OpOverflow})
		}
		mapped, end := w.m.mapBatch(batch)
		events = append(events, mapped...)
		for _, ev := range events {
			select {
			case w.out <- ev:
			case <-ctx.Done():
				return
			case <-w.f.done:
				return
			}
		}
		if end {
			logger.Debug(w.f.log, logger.Fields{"op": "vfs/local: watch root changed", "dir": w.m.dir})
			return
		}
	}
}

// release stops the stream and frees it, in the order that makes deleting the handle safe: once
// Invalidate has unscheduled the stream and an empty task has drained the serial queue, no callback
// is running or can run. Then the channel closes.
func (w *fseWatch) release() {
	C.FSEventStreamStop(w.stream)
	C.FSEventStreamInvalidate(w.stream)
	C.golib_vfs_drain(w.queue)
	C.FSEventStreamRelease(w.stream)
	C.golib_vfs_queue_release(w.queue)
	w.deleteHandle()
	close(w.out)
}
