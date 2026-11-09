//go:build windows

package local

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/vfs"
)

// winNotifyFilter is what a watch asks ReadDirectoryChangesW for: names appearing, going and renamed
// (files and directories), and a file's content changing (its size or its write time).
const winNotifyFilter = windows.FILE_NOTIFY_CHANGE_FILE_NAME | windows.FILE_NOTIFY_CHANGE_DIR_NAME |
	windows.FILE_NOTIFY_CHANGE_SIZE | windows.FILE_NOTIFY_CHANGE_LAST_WRITE

// winNotifyBuffer is one read's buffer: the kernel keeps changes made between reads in a buffer of
// this size, and reports an overflow when they outgrow it. 64 KiB is the most a network share takes.
const winNotifyBuffer = 64 << 10

// winRootCheck is how often a watch with nothing to report asks whether its directory is still
// where it was watched: moved away, a directory reports nothing about itself. A test shortens it.
var winRootCheck = time.Second

// winWatches counts the watches whose handles are open, so tests can prove a failed or ended watch
// leaves none behind.
var winWatches atomic.Int64

// Watch streams change events under dir from ReadDirectoryChangesW until ctx ends or the FS closes;
// the channel then closes. One handle covers the whole tree, so there is no ceiling on how many
// directories a root may hold. With [vfs.Recursive] every subdirectory is covered, including ones
// created or moved in later; a directory that appears also yields an OpOverflow for its subtree, since
// a populated directory moved in reports only itself.
//
// Presence is decided by an lstat when an event is handled, so a name that went and came back is
// an OpCreate. A consumer that falls behind until the kernel's buffer overflows gets an
// OpOverflow{""}. The watched directory deleted, or moved or replaced (noticed within a second),
// yields a final OpOverflow{""} before the close. With [vfs.SkipDirs], nothing under a skipped
// directory is reported and its appearance is never an OpOverflow; the kernel still watches it, so
// its changes count toward the buffer.
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
	w, err := f.newWinWatch(dir, cfg)
	if err != nil {
		return nil, &fs.PathError{Op: "watch", Path: dir, Err: err}
	}
	// the first read is issued before Watch returns: the kernel records nothing for a handle until
	// then, so a change made as soon as Watch returns would otherwise be lost
	if err := w.issue(); err != nil {
		w.closeHandles()
		return nil, &fs.PathError{Op: "watch", Path: dir, Err: err}
	}
	go w.run(ctx)
	return w.out, nil
}

// winWatch is one Watch call: one overlapped directory handle, one read in flight at a time.
type winWatch struct {
	f      *FS
	m      winMapper
	out    chan vfs.Event
	h      windows.Handle // the watched directory, opened for listing and overlapped I/O
	volume uint32         // h's volume and file index: the directory the watch is of
	index  uint64
	ov     windows.Overlapped // the read in flight's; ov.HEvent is signalled when it completes
	buf    []byte

	mu      sync.Mutex // orders issuing a read against cancelling it: no read starts after stop
	stopped bool

	checked time.Time // when rootHere last ran: a busy watch checks as often as an idle one
}

// newWinWatch opens dir for the watch. dir is opened through the root, so the jail resolves it, and
// that handle is opened again relative to itself (NtCreateFile with an empty name, as ReOpenFile
// does) for listing and overlapped I/O: no path is looked up after the jail's, so a directory swapped
// for a junction in between cannot move the watch outside.
func (f *FS) newWinWatch(dir string, cfg vfs.WatchConfig) (*winWatch, error) {
	d, err := f.root.Open(dir)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	name, err := windows.NewNTUnicodeString("")
	if err != nil {
		return nil, err
	}
	oa := &windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(d.Fd()), ObjectName: name}
	oa.Length = uint32(unsafe.Sizeof(*oa))
	var h windows.Handle
	var iosb windows.IO_STATUS_BLOCK
	if err := windows.NtCreateFile(&h, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, oa, &iosb, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN,
		windows.FILE_DIRECTORY_FILE, 0, 0); err != nil {
		return nil, fmt.Errorf("reopening the directory for a watch: %w", err)
	}
	var hi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &hi); err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("reading the watched directory's identity: %w", err)
	}
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("creating the watch's event: %w", err)
	}
	winWatches.Add(1)
	return &winWatch{
		checked: time.Now(),
		f:       f,
		m:       winMapper{dir: dir, recursive: cfg.Recursive, cfg: cfg, stat: func(rel string) presence { return f.presence(rel) }},
		out:     make(chan vfs.Event),
		h:       h, volume: hi.VolumeSerialNumber, index: uint64(hi.FileIndexHigh)<<32 | uint64(hi.FileIndexLow),
		ov:  windows.Overlapped{HEvent: ev},
		buf: make([]byte, winNotifyBuffer),
	}, nil
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

// rootDue is rootHere when winRootCheck has passed since the last check, else true. A directory
// moved away keeps reporting the changes made in it, so a check made only when nothing happens
// would never come while it stays busy.
func (w *winWatch) rootDue() bool {
	if time.Since(w.checked) < winRootCheck {
		return true
	}
	return w.rootHere()
}

// rootHere is whether the watched directory is still the one at dir: deleted (its name gone, or
// pending its deletion), moved, or replaced, it is not.
func (w *winWatch) rootHere() bool {
	w.checked = time.Now()
	fi, err := w.f.lstat(w.m.dir)
	if err != nil {
		return false
	}
	wi, ok := fi.(winInfo)
	return ok && wi.ok && fi.IsDir() && wi.volume == w.volume && wi.index == w.index
}

func (w *winWatch) run(ctx context.Context) {
	quit, cancelled := make(chan struct{}), make(chan struct{})
	go func() { // ends the read in flight when the watch ends, from outside or from run itself
		defer close(cancelled)
		select {
		case <-ctx.Done():
		case <-w.f.done:
		case <-quit:
		}
		w.mu.Lock()
		w.stopped = true
		_ = windows.CancelIoEx(w.h, &w.ov)
		w.mu.Unlock()
	}()
	defer func() {
		close(quit)
		<-cancelled // no CancelIoEx can reach h once it is closed, and maybe reused
		w.closeHandles()
		close(w.out)
	}()
	send := func(evs ...vfs.Event) bool {
		for _, ev := range evs {
			select {
			case w.out <- ev:
			case <-ctx.Done():
				return false
			case <-w.f.done:
				return false
			}
		}
		return true
	}
	end := func(why string) {
		logger.Debug(w.f.log, logger.Fields{"op": "vfs/local: watch root changed", "dir": w.m.dir, "why": why})
		send(vfs.Event{Path: "", Op: vfs.OpOverflow})
	}
	for {
		n, err := w.wait()
		var evs []vfs.Event
		switch {
		case errors.Is(err, errWatchStopped) || errors.Is(err, windows.ERROR_OPERATION_ABORTED):
			return
		case errors.Is(err, errRootGone):
			end("moved or replaced")
			return
		case errors.Is(err, windows.ERROR_NOTIFY_ENUM_DIR) || (err == nil && n == 0):
			if !w.rootHere() {
				end("gone")
				return
			}
			evs = []vfs.Event{{Path: "", Op: vfs.OpOverflow}} // the kernel's buffer overflowed
		case err != nil: // the directory went (a read on one pending its deletion is refused), or the handle failed
			end(err.Error())
			return
		case !w.rootDue(): // checked before mapping: records of a moved directory name what is at dir now
			end("moved or replaced")
			return
		default:
			evs = w.m.mapBatch(parseNotify(w.buf[:n]))
		}
		if !send(evs...) {
			return
		}
		// changes made meanwhile are kept by the kernel for this next read
		if err := w.issue(); err != nil {
			if !errors.Is(err, errWatchStopped) {
				end(err.Error())
			}
			return
		}
	}
}

// closeHandles closes the watch's handles; the watch is then no longer counted.
func (w *winWatch) closeHandles() {
	windows.CloseHandle(w.ov.HEvent)
	windows.CloseHandle(w.h)
	winWatches.Add(-1)
}

var (
	errWatchStopped = errors.New("the watch ended")
	errRootGone     = errors.New("the watched directory is no longer there")
)

// issue starts one ReadDirectoryChangesW, unless the watch has been stopped: under mu, so a stop
// either comes first, and no read starts, or comes after and cancels it.
func (w *winWatch) issue() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return errWatchStopped
	}
	_ = windows.ResetEvent(w.ov.HEvent)
	return windows.ReadDirectoryChanges(w.h, &w.buf[0], uint32(len(w.buf)), w.m.recursive, winNotifyFilter, nil, &w.ov, 0)
}

// wait waits for the read in flight, checking within winRootCheck of the last check that the
// watched directory is still there; it returns how many bytes of records the read left in buf.
func (w *winWatch) wait() (uint32, error) {
	for {
		// waits out what is left until the check is due, not a fresh interval: the check comes
		// within winRootCheck of the last, busy or idle
		left := max(winRootCheck-time.Since(w.checked), time.Millisecond)
		ev, err := windows.WaitForSingleObject(w.ov.HEvent, uint32(left/time.Millisecond))
		if err != nil || ev != uint32(windows.WAIT_TIMEOUT) {
			break
		}
		if !w.rootDue() {
			_ = windows.CancelIoEx(w.h, &w.ov)
			var n uint32
			_ = windows.GetOverlappedResult(w.h, &w.ov, &n, true) // the kernel is done with buf
			return 0, errRootGone
		}
	}
	var n uint32
	err := windows.GetOverlappedResult(w.h, &w.ov, &n, true)
	return n, err
}
