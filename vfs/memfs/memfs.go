package memfs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/vfs"
)

var (
	errClosed   = errs.Sentinel(errs.ErrClosed, "memfs: closed")
	errIsDir    = errs.Sentinel(errs.ErrInvalidArgument, "memfs: is a directory")
	errNotDir   = errs.Sentinel(errs.ErrInvalidArgument, "memfs: not a directory")
	errNotEmpty = errs.Sentinel(errs.ErrPrecondition, "memfs: directory not empty")
)

// FS is an in-memory [vfs.FS]. It implements every capability — [vfs.ConditionalWriter],
// [vfs.ExclusiveCreator], [vfs.NoReplaceRenamer], [vfs.Watcher] and [vfs.Copier] — and, with no other
// writer possible, every condition is exact. The zero value is not usable; call [New].
type FS struct {
	mu       sync.Mutex
	nodes    map[string]*node // "." is the root directory
	seq      uint64           // version counter, shared by all nodes
	closed   bool
	watchers map[*watcher]struct{}
	now      func() time.Time
}

type node struct {
	dir  bool
	data []byte
	perm fs.FileMode
	mod  time.Time
	ver  uint64
}

// Option configures [New].
type Option func(*FS)

// WithClock replaces time.Now as the source of modification times (tests).
func WithClock(now func() time.Time) Option { return func(f *FS) { f.now = now } }

// New returns an empty filesystem containing only its root directory.
func New(opts ...Option) *FS {
	f := &FS{nodes: map[string]*node{}, watchers: map[*watcher]struct{}{}, now: time.Now}
	for _, o := range opts {
		o(f)
	}
	f.seq++
	f.nodes["."] = &node{dir: true, perm: 0o755, mod: f.now(), ver: f.seq}
	return f
}

var (
	_ vfs.FS                = (*FS)(nil)
	_ vfs.ConditionalWriter = (*FS)(nil)
	_ vfs.ExclusiveCreator  = (*FS)(nil)
	_ vfs.NoReplaceRenamer  = (*FS)(nil)
	_ vfs.Watcher           = (*FS)(nil)
	_ vfs.Copier            = (*FS)(nil)
)

// cond is the condition a mutation commits under: none, the target's version, or its absence.
type cond struct {
	ifVersion bool
	want      vfs.Version
	exclusive bool
}

func pathErr(op, name string, err error) error { return &fs.PathError{Op: op, Path: name, Err: err} }

// begin checks ctx, name and closed state, and takes the lock. The caller unlocks.
func (f *FS) begin(ctx context.Context, op, name string, mutable bool) error {
	if err := ctx.Err(); err != nil {
		return pathErr(op, name, err)
	}
	check := vfs.CheckName
	if mutable {
		check = vfs.CheckMutable
	}
	if err := check(op, name); err != nil {
		return err
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return pathErr(op, name, errClosed)
	}
	return nil
}

func (f *FS) info(name string, n *node) vfs.FileInfo {
	fi := vfs.FileInfo{
		Path:    name,
		Name:    baseOf(name),
		ModTime: n.mod,
		Mode:    n.mode(),
		Version: vfs.Version(fmt.Sprintf("m%d", n.ver)),
	}
	if !n.dir {
		fi.Size = int64(len(n.data))
	}
	return fi
}

func (n *node) mode() fs.FileMode {
	if n.dir {
		return fs.ModeDir | n.perm
	}
	return n.perm
}

func baseOf(name string) string {
	if name == "." {
		return "."
	}
	_, b := vfs.Split(name)
	return b
}

// touch gives n a new version and modification time.
func (f *FS) touch(n *node) {
	f.seq++
	n.ver = f.seq
	n.mod = f.now()
}

// Stat returns the entry at name.
func (f *FS) Stat(ctx context.Context, name string) (vfs.FileInfo, error) {
	if err := f.begin(ctx, "stat", name, false); err != nil {
		return vfs.FileInfo{}, err
	}
	defer f.mu.Unlock()
	n, ok := f.nodes[name]
	if !ok {
		return vfs.FileInfo{}, pathErr("stat", name, vfs.ErrNotExist)
	}
	return f.info(name, n), nil
}

// ReadDir lists the directory at name, sorted by Name.
func (f *FS) ReadDir(ctx context.Context, name string) ([]vfs.FileInfo, error) {
	if err := f.begin(ctx, "readdir", name, false); err != nil {
		return nil, err
	}
	defer f.mu.Unlock()
	n, ok := f.nodes[name]
	if !ok {
		return nil, pathErr("readdir", name, vfs.ErrNotExist)
	}
	if !n.dir {
		return nil, pathErr("readdir", name, errNotDir)
	}
	var out []vfs.FileInfo
	for p, c := range f.nodes {
		if p != "." && parentOf(p) == name {
			out = append(out, f.info(p, c))
		}
	}
	slices.SortFunc(out, func(a, b vfs.FileInfo) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func parentOf(name string) string { d, _ := vfs.Split(name); return d }

// Open streams the file at name from offset. The content is a snapshot taken at Open.
func (f *FS) Open(ctx context.Context, name string, offset int64) (io.ReadCloser, error) {
	if err := f.begin(ctx, "open", name, false); err != nil {
		return nil, err
	}
	defer f.mu.Unlock()
	n, ok := f.nodes[name]
	if !ok {
		return nil, pathErr("open", name, vfs.ErrNotExist)
	}
	if n.dir {
		return nil, pathErr("open", name, errIsDir)
	}
	if offset < 0 {
		return nil, pathErr("open", name, errs.Wrap(errs.ErrInvalidArgument, "negative offset %d", offset))
	}
	data := n.data
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	return io.NopCloser(bytes.NewReader(bytes.Clone(data[offset:]))), nil
}

// check enforces c against the current entry at name (nil when absent).
func (f *FS) check(name string, c cond, cur *node) error {
	switch {
	case c.exclusive && cur != nil:
		return &vfs.ConflictError{Path: name, Current: f.info(name, cur)}
	case c.ifVersion && cur == nil:
		return &vfs.ConflictError{Path: name, Want: c.want}
	case c.ifVersion && f.info(name, cur).Version != c.want:
		return &vfs.ConflictError{Path: name, Want: c.want, Current: f.info(name, cur)}
	}
	return nil
}

// WriteFile replaces the file at name with r's content. r is read in full before the commit, so the
// replacement is atomic.
func (f *FS) WriteFile(ctx context.Context, name string, r io.Reader, opts ...vfs.WriteOption) (vfs.FileInfo, error) {
	return f.write(ctx, name, r, cond{}, opts)
}

// WriteFileIf is WriteFile that commits only if name's current version is want.
func (f *FS) WriteFileIf(ctx context.Context, name string, r io.Reader, want vfs.Version, opts ...vfs.WriteOption) (vfs.FileInfo, error) {
	return f.write(ctx, name, r, cond{ifVersion: true, want: want}, opts)
}

// CreateExclusive writes name only if nothing exists there.
func (f *FS) CreateExclusive(ctx context.Context, name string, r io.Reader, opts ...vfs.WriteOption) (vfs.FileInfo, error) {
	return f.write(ctx, name, r, cond{exclusive: true}, opts)
}

func (f *FS) write(ctx context.Context, name string, r io.Reader, c cond, opts []vfs.WriteOption) (vfs.FileInfo, error) {
	cfg := vfs.ResolveWrite(opts)
	if err := vfs.CheckMutable("write", name); err != nil {
		return vfs.FileInfo{}, err
	}
	data, err := io.ReadAll(ctxReader{ctx, r})
	if err != nil {
		return vfs.FileInfo{}, pathErr("write", name, err)
	}
	if err := f.begin(ctx, "write", name, true); err != nil {
		return vfs.FileInfo{}, err
	}
	defer f.mu.Unlock()
	parent, ok := f.nodes[parentOf(name)]
	if !ok {
		return vfs.FileInfo{}, pathErr("write", name, vfs.ErrNotExist)
	}
	if !parent.dir {
		return vfs.FileInfo{}, pathErr("write", name, errNotDir)
	}
	cur := f.nodes[name]
	if err := f.check(name, c, cur); err != nil {
		return vfs.FileInfo{}, err
	}
	if cur != nil && cur.dir {
		return vfs.FileInfo{}, pathErr("write", name, errIsDir)
	}
	op := vfs.OpWrite
	if cur == nil {
		perm := cfg.Perm
		if perm == 0 {
			perm = 0o644
		}
		cur = &node{perm: perm}
		f.nodes[name] = cur
		f.touch(parent)
		op = vfs.OpCreate
	}
	cur.data = data
	f.touch(cur)
	f.emit(name, op)
	return f.info(name, cur), nil
}

// MkdirAll creates name and any missing parents.
func (f *FS) MkdirAll(ctx context.Context, name string) error {
	if err := f.begin(ctx, "mkdir", name, false); err != nil {
		return err
	}
	defer f.mu.Unlock()
	if name == "." {
		return nil
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		p := strings.Join(parts[:i+1], "/")
		n, ok := f.nodes[p]
		if ok {
			if !n.dir {
				return pathErr("mkdir", p, errNotDir)
			}
			continue
		}
		f.nodes[p] = &node{dir: true, perm: 0o755}
		f.touch(f.nodes[p])
		f.touch(f.nodes[parentOf(p)])
		f.emit(p, vfs.OpCreate)
	}
	return nil
}

// Remove deletes the file or empty directory at name.
func (f *FS) Remove(ctx context.Context, name string) error { return f.remove(ctx, name, cond{}) }

// RemoveIf is Remove that removes only if name's current version is want.
func (f *FS) RemoveIf(ctx context.Context, name string, want vfs.Version) error {
	return f.remove(ctx, name, cond{ifVersion: true, want: want})
}

func (f *FS) remove(ctx context.Context, name string, c cond) error {
	if err := f.begin(ctx, "remove", name, true); err != nil {
		return err
	}
	defer f.mu.Unlock()
	cur, ok := f.nodes[name]
	if err := f.check(name, c, nodeOrNil(cur, ok)); err != nil {
		return err
	}
	if !ok {
		return pathErr("remove", name, vfs.ErrNotExist)
	}
	if cur.dir && f.hasChildren(name) {
		return pathErr("remove", name, errNotEmpty)
	}
	delete(f.nodes, name)
	f.touch(f.nodes[parentOf(name)])
	f.emit(name, vfs.OpRemove)
	return nil
}

func nodeOrNil(n *node, ok bool) *node {
	if !ok {
		return nil
	}
	return n
}

func (f *FS) hasChildren(dir string) bool {
	for p := range f.nodes {
		if p != "." && parentOf(p) == dir {
			return true
		}
	}
	return false
}

// under reports whether p is name or inside it.
func under(p, name string) bool { return p == name || strings.HasPrefix(p, name+"/") }

// RemoveAll deletes name and everything under it; a missing name is not an error.
func (f *FS) RemoveAll(ctx context.Context, name string) error {
	if err := f.begin(ctx, "removeall", name, true); err != nil {
		return err
	}
	defer f.mu.Unlock()
	if _, ok := f.nodes[name]; !ok {
		return nil
	}
	var gone []string
	for p := range f.nodes {
		if under(p, name) {
			gone = append(gone, p)
		}
	}
	slices.Sort(gone)
	for _, p := range slices.Backward(gone) {
		delete(f.nodes, p)
		f.emit(p, vfs.OpRemove)
	}
	f.touch(f.nodes[parentOf(name)])
	return nil
}

// Rename moves from to to, atomically, including whole directories.
func (f *FS) Rename(ctx context.Context, from, to string) error {
	return f.rename(ctx, from, to, cond{})
}

// RenameNoReplace is Rename that refuses to replace an existing entry at to.
func (f *FS) RenameNoReplace(ctx context.Context, from, to string) error {
	return f.rename(ctx, from, to, cond{exclusive: true})
}

func (f *FS) rename(ctx context.Context, from, to string, c cond) error {
	if err := vfs.CheckMutable("rename", from); err != nil {
		return err
	}
	if err := f.begin(ctx, "rename", to, true); err != nil {
		return err
	}
	defer f.mu.Unlock()
	src, ok := f.nodes[from]
	if !ok {
		return pathErr("rename", from, vfs.ErrNotExist)
	}
	if from == to {
		return f.check(to, c, src) // renameat2(RENAME_NOREPLACE) onto itself is EEXIST too
	}
	if src.dir && under(to, from) {
		return pathErr("rename", to, errs.Wrap(errs.ErrInvalidArgument, "cannot move %q into itself", from))
	}
	parent, ok := f.nodes[parentOf(to)]
	if !ok {
		return pathErr("rename", to, vfs.ErrNotExist)
	}
	if !parent.dir {
		return pathErr("rename", to, errNotDir)
	}
	dst := f.nodes[to]
	if err := f.check(to, c, dst); err != nil {
		return err
	}
	if dst != nil {
		switch {
		case dst.dir && !src.dir:
			return pathErr("rename", to, errIsDir)
		case !dst.dir && src.dir:
			return pathErr("rename", to, errNotDir)
		case dst.dir && f.hasChildren(to):
			return pathErr("rename", to, errNotEmpty)
		}
		delete(f.nodes, to)
		f.emit(to, vfs.OpRemove)
	}
	var moved []string
	for p := range f.nodes {
		if under(p, from) {
			moved = append(moved, p)
		}
	}
	slices.Sort(moved)
	for _, p := range moved {
		n := f.nodes[p]
		delete(f.nodes, p)
		np := to + strings.TrimPrefix(p, from)
		f.nodes[np] = n
		f.touch(n)
		f.emit(p, vfs.OpRemove)
		f.emit(np, vfs.OpCreate)
	}
	f.touch(f.nodes[parentOf(from)])
	f.touch(parent)
	return nil
}

// Copy copies the file at from to to, replacing an existing file.
func (f *FS) Copy(ctx context.Context, from, to string, opts ...vfs.WriteOption) (vfs.FileInfo, error) {
	if err := vfs.CheckName("copy", from); err != nil {
		return vfs.FileInfo{}, err
	}
	if err := f.begin(ctx, "copy", from, false); err != nil {
		return vfs.FileInfo{}, err
	}
	src, ok := f.nodes[from]
	var data []byte
	if ok {
		data = bytes.Clone(src.data)
	}
	f.mu.Unlock()
	if !ok {
		return vfs.FileInfo{}, pathErr("copy", from, vfs.ErrNotExist)
	}
	if src.dir {
		return vfs.FileInfo{}, pathErr("copy", from, errIsDir)
	}
	return f.WriteFile(ctx, to, bytes.NewReader(data), opts...)
}

// Close releases the filesystem and ends every watch.
func (f *FS) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	for w := range f.watchers {
		w.stop()
	}
	clear(f.watchers)
	return nil
}

// ctxReader aborts a read once ctx ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
