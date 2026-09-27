//go:build linux

package local

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/vfs"
)

var _ vfs.Watcher = (*FS)(nil)

var errNotDir = errs.Sentinel(errs.ErrInvalidArgument, "local: not a directory")

const watchMask = unix.IN_CREATE | unix.IN_CLOSE_WRITE | unix.IN_DELETE | unix.IN_MOVED_FROM |
	unix.IN_MOVED_TO | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_ATTRIB | unix.IN_ONLYDIR |
	unix.IN_EXCL_UNLINK

// Watch streams change events under dir with inotify until ctx ends or the FS closes; the channel
// then closes. With [vfs.Recursive] every subdirectory is watched, including ones created or moved in
// later — each of those also yields an OpOverflow for its subtree, since entries made before its watch
// landed are unknowable. A kernel queue overflow or an exhausted watch limit yields OpOverflow rather
// than a silent gap. If dir itself is deleted or moved, a final OpOverflow{Path: ""} precedes the close.
//
// Watches attach to the directory the root resolved (opened through os.Root, then watched via its fd's
// /proc/self/fd link), so no pathname is re-traversed after the jail checked it. The consumer must keep
// reading: a stalled consumer stalls the reader, and the kernel queue then overflows.
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
	in, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, &fs.PathError{Op: "watch", Path: dir, Err: err}
	}
	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		unix.Close(in)
		return nil, &fs.PathError{Op: "watch", Path: dir, Err: err}
	}
	w := &watch{
		f: f, in: in, wakeR: pipe[0], wakeW: pipe[1], dir: dir, recursive: cfg.Recursive,
		paths: map[int]string{}, wds: map[string]int{}, moved: map[uint32]string{},
		out: make(chan vfs.Event), stop: make(chan struct{}),
	}
	if _, err := w.addDir(dir); err != nil {
		w.release()
		return nil, &fs.PathError{Op: "watch", Path: dir, Err: err}
	}
	if cfg.Recursive {
		w.addChildren(dir)
	}
	w.wg.Add(1)
	go w.waker(ctx)
	go w.run(ctx)
	return w.out, nil
}

// watch is one Watch call: one inotify instance, one watch descriptor per directory. Its maps are
// touched only by Watch before run starts and by run after.
type watch struct {
	f            *FS
	in           int // inotify fd
	wakeR, wakeW int // a pipe the waker writes to end run's poll
	dir          string
	recursive    bool

	paths map[int]string    // wd → root-relative directory
	wds   map[string]int    // directory → wd
	moved map[uint32]string // IN_MOVED_FROM cookie → old directory path, awaiting its IN_MOVED_TO
	queue []vfs.Event       // events waiting to be sent

	out  chan vfs.Event
	stop chan struct{} // closed when run exits, so the waker never writes a closed fd
	wg   sync.WaitGroup
}

// addDir attaches a watch to the directory the jail resolves for rel. The fd only has to live across
// the add: the watch is on the inode.
func (w *watch) addDir(rel string) (int, error) {
	d, err := w.f.root.Open(rel)
	if err != nil {
		return -1, err
	}
	defer d.Close()
	st, err := d.Stat()
	if err != nil {
		return -1, err
	}
	if !st.IsDir() {
		return -1, errNotDir
	}
	wd, err := unix.InotifyAddWatch(w.in, fmt.Sprintf("/proc/self/fd/%d", d.Fd()), watchMask)
	if err != nil {
		return -1, err
	}
	if old, ok := w.paths[wd]; ok && old != rel {
		delete(w.wds, old) // the same inode reached by another path (a link swapped in); last one wins
	}
	w.paths[wd] = rel
	w.wds[rel] = wd
	return wd, nil
}

// addChildren watches every real subdirectory under rel (symlinks are not entered). An exhausted watch
// limit queues an OpOverflow for the subtree it could not cover; entries that vanished are skipped.
func (w *watch) addChildren(rel string) {
	d, err := w.f.root.Open(rel)
	if err != nil {
		return
	}
	entries, err := d.ReadDir(-1)
	d.Close()
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || vfs.IsTemp(e.Name()) { // DirEntry.IsDir is false for a symlink
			continue
		}
		sub := vfs.Join(rel, e.Name())
		if _, err := w.addDir(sub); err != nil {
			if errors.Is(err, unix.ENOSPC) {
				w.queue = append(w.queue, vfs.Event{Path: sub, Op: vfs.OpOverflow})
			}
			continue
		}
		w.addChildren(sub)
	}
}

// dropTree removes the watches for rel and everything under it.
func (w *watch) dropTree(rel string) {
	for p, wd := range w.wds {
		if p == rel || strings.HasPrefix(p, rel+"/") {
			_, _ = unix.InotifyRmWatch(w.in, uint32(wd))
			delete(w.wds, p)
			delete(w.paths, wd)
		}
	}
}

// moveTree rewrites the paths of rel's subtree after a move within the watch.
func (w *watch) moveTree(from, to string) {
	for p, wd := range w.wds {
		if p == from || strings.HasPrefix(p, from+"/") {
			np := to + strings.TrimPrefix(p, from)
			delete(w.wds, p)
			w.wds[np] = wd
			w.paths[wd] = np
		}
	}
}

// waker ends run's poll when ctx ends or the FS closes.
func (w *watch) waker(ctx context.Context) {
	defer w.wg.Done()
	select {
	case <-ctx.Done():
	case <-w.f.done:
	case <-w.stop:
		return
	}
	_, _ = unix.Write(w.wakeW, []byte{0})
}

func (w *watch) run(ctx context.Context) {
	defer func() {
		close(w.stop)
		w.wg.Wait()
		w.release()
		close(w.out)
	}()
	buf := make([]byte, 64*(unix.SizeofInotifyEvent+unix.NAME_MAX+1))
	for {
		if !w.flush(ctx) {
			return
		}
		fds := []unix.PollFd{{Fd: int32(w.in), Events: unix.POLLIN}, {Fd: int32(w.wakeR), Events: unix.POLLIN}}
		if _, err := unix.Poll(fds, -1); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			logger.Error(w.f.log, err, logger.Fields{"op": "vfs/local: watch poll", "dir": w.dir})
			return
		}
		if fds[1].Revents != 0 {
			return
		}
		n, err := unix.Read(w.in, buf)
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				continue
			}
			logger.Error(w.f.log, err, logger.Fields{"op": "vfs/local: watch read", "dir": w.dir})
			return
		}
		if !w.parse(buf[:n]) {
			w.queue = append(w.queue, vfs.Event{Path: "", Op: vfs.OpOverflow})
			w.flush(ctx)
			return
		}
	}
}

// parse handles one read's worth of events. It returns false when the watched directory itself went.
func (w *watch) parse(b []byte) bool {
	for off := 0; off+unix.SizeofInotifyEvent <= len(b); {
		wd := int32(binary.NativeEndian.Uint32(b[off:]))
		mask := binary.NativeEndian.Uint32(b[off+4:])
		cookie := binary.NativeEndian.Uint32(b[off+8:])
		nlen := int(binary.NativeEndian.Uint32(b[off+12:]))
		name := strings.TrimRight(string(b[off+unix.SizeofInotifyEvent:off+unix.SizeofInotifyEvent+nlen]), "\x00")
		off += unix.SizeofInotifyEvent + nlen
		if !w.handle(int(wd), mask, cookie, name) {
			return false
		}
	}
	// moves whose IN_MOVED_TO did not follow went out of the watch: their watches go too
	for c, old := range w.moved {
		w.dropTree(old)
		delete(w.moved, c)
	}
	return true
}

func (w *watch) handle(wd int, mask, cookie uint32, name string) bool {
	if mask&unix.IN_Q_OVERFLOW != 0 {
		w.queue = append(w.queue, vfs.Event{Path: "", Op: vfs.OpOverflow})
		return true
	}
	dir, ok := w.paths[wd]
	if !ok {
		return true // a watch already dropped
	}
	if mask&(unix.IN_DELETE_SELF|unix.IN_MOVE_SELF) != 0 {
		return dir != w.dir // the watched directory itself: end the watch; a subdirectory: its parent reports it
	}
	if mask&unix.IN_IGNORED != 0 {
		delete(w.paths, wd)
		if w.wds[dir] == wd {
			delete(w.wds, dir)
		}
		return true
	}
	if name == "" || vfs.IsTemp(name) {
		return true
	}
	p := vfs.Join(dir, name)
	isDir := mask&unix.IN_ISDIR != 0
	switch {
	case mask&unix.IN_CREATE != 0:
		w.queue = append(w.queue, vfs.Event{Path: p, Op: vfs.OpCreate})
		if isDir && w.recursive {
			w.adopt(p)
		}
	case mask&unix.IN_MOVED_FROM != 0:
		w.queue = append(w.queue, vfs.Event{Path: p, Op: vfs.OpRemove})
		if isDir {
			w.moved[cookie] = p
		}
	case mask&unix.IN_MOVED_TO != 0:
		w.queue = append(w.queue, vfs.Event{Path: p, Op: vfs.OpCreate})
		if old, ok := w.moved[cookie]; ok {
			delete(w.moved, cookie)
			w.moveTree(old, p) // same inodes, same watches, new paths
		} else if isDir && w.recursive {
			w.adopt(p) // moved in from outside, possibly populated
		}
	case mask&unix.IN_DELETE != 0:
		w.queue = append(w.queue, vfs.Event{Path: p, Op: vfs.OpRemove})
	case mask&(unix.IN_CLOSE_WRITE|unix.IN_ATTRIB) != 0 && !isDir:
		w.queue = append(w.queue, vfs.Event{Path: p, Op: vfs.OpWrite})
	}
	return true
}

// adopt watches a directory that appeared under the watch, then reports its subtree as overflowed:
// whatever was created in it before the watches landed cannot be known.
func (w *watch) adopt(p string) {
	if _, err := w.addDir(p); err == nil {
		w.addChildren(p)
	}
	w.queue = append(w.queue, vfs.Event{Path: p, Op: vfs.OpOverflow})
}

// flush sends the queued events; it returns false once ctx ends or the FS closes.
func (w *watch) flush(ctx context.Context) bool {
	for len(w.queue) > 0 {
		select {
		case w.out <- w.queue[0]:
			w.queue = w.queue[1:]
		case <-ctx.Done():
			return false
		case <-w.f.done:
			return false
		}
	}
	w.queue = nil
	return true
}

// release removes every watch and closes the fds.
func (w *watch) release() {
	for wd := range w.paths {
		_, _ = unix.InotifyRmWatch(w.in, uint32(wd))
	}
	unix.Close(w.in)
	unix.Close(w.wakeR)
	unix.Close(w.wakeW)
}
