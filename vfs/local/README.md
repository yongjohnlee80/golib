# vfs/local

The `vfs.FS` driver for a directory on local disk. **Linux and macOS only** — its one commit path is
`golang.org/x/sys/unix` on a directory fd, and other platforms get no driver rather than a weaker one.

```go
fsys, err := local.New("/srv/kb", local.WithLogger(log))
```

## How it stays inside the root

Every name is resolved through `os.Root`, so `..`, absolute names and symlinks that point outside the
root cannot reach outside it. Writes go further: the parent directory is opened once through the root
and **pinned by fd**, and the temp file is created, committed and cleaned up relative to that fd with
base names only. A concurrent rename of the directory, or a symlink swapped in on its path, cannot
split the temp file from its commit or move either outside the root.

## Writes

1. condition check under a per-name lock (exact among callers of this `FS`);
2. `openat(O_CREAT|O_EXCL|O_NOFOLLOW)` a `.vfs-tmp-*` file in the pinned parent, copy, `fchmod`, `fsync`;
3. **commit** — one `renameat` (replace), or `renameat2(RENAME_NOREPLACE)` on Linux /
   `renameatx_np(RENAME_EXCL)` on macOS for `CreateExclusive` and `RenameNoReplace`;
4. `fsync` the parent. A failure from here on is a `*vfs.CommitError`.

A filesystem that refuses the no-replace flag makes those calls return `errs.ErrUnsupported`. The
kernel's EINVAL is ambiguous (it also means "a directory moved into itself", e.g. through a symlink
alias), so on EINVAL a probe in the destination directory decides which it was.
Temp files are hidden from `ReadDir`, `Walk` and `Watch`, and are invalid names for callers.

Conditions are **not** exact against other processes: a write by another program that lands between
the check and the commit is overwritten. Watching can detect that on a best-effort basis, never
prevent it.

## Versions

`dev:inode:size:mtime-ns:ctime-ns` of the entry's `lstat` — O(1), no content hash. An atomic replace
changes the inode; an in-place edit moves mtime and ctime, and ctime cannot be set by user tools.
Limits: the filesystem's timestamp granularity, inode reuse after delete + create, and a foreign write
racing the lstat. Hash content yourself when you need content identity.

## Watching (Linux)

`Watch` uses inotify through x/sys — one instance per call, one watch per directory. Each watch
attaches to the directory the root resolved (opened through `os.Root`, watched via its
`/proc/self/fd` link), never to a re-traversed pathname.

- `vfs.Recursive()` watches subdirectories created or moved in later; each also yields an
  `OpOverflow` for its subtree, since what was inside before its watch landed is unknowable.
- A move inside the watch is `OpRemove(old)` + `OpCreate(new)`, and later events use the new path.
- A kernel queue overflow is an `OpOverflow`.
- **No silent gaps.** A subdirectory that cannot be watched at setup (permissions, `max_user_watches`,
  the descriptor limit) makes `Watch` return that error — fall back to `vfs.Poll`. One that appears
  later and cannot be watched ends the watch. So does the watched directory itself being deleted or
  moved: a final `OpOverflow{Path: ""}`, then the channel closes. A directory that vanished before its
  watch landed is not a gap; its parent reports it.
- Keep reading: a stalled consumer stalls the reader, and the kernel queue then overflows.
- `vfs.SkipDirs(pred)`: a skipped directory gets no watch, nor does anything under it — which also
  keeps `node_modules` from spending `max_user_watches`. Its own appearance is an `OpCreate`, never an
  `OpOverflow`.

## Watching (macOS)

Built with cgo, `Watch` uses one FSEvents stream per call (ADR golib-vfs-0002): file-level events,
a private serial dispatch queue, 50 ms coalescing. One stream covers the whole tree, so there is no
descriptor per directory and no ceiling on how many a root holds.

- The stream watches the directory's canonical path, taken with `F_GETPATH` from the directory the
  root opened (`/private/var`, not `/var`); events are mapped back to root-relative paths.
- FSEvents' flags accumulate per path, so presence is decided by an `lstat` when the event is
  handled: a vanished path is `OpRemove`, a present one `OpCreate` or `OpWrite`. A write to a recently
  created file may arrive as `OpCreate` (treat it like `OpWrite`, as everywhere).
- A directory that appears is `OpCreate` + `OpOverflow` for its subtree (a populated directory moved in
  reports only itself), except one `vfs.SkipDirs` names: `OpCreate` alone, and nothing inside it.
- FSEvents' own dropped-events report, or a consumer more than 16 384 records behind, is an
  `OpOverflow`; the watched directory deleted or moved is a final `OpOverflow{Path: ""}`, then the
  channel closes. A stream that cannot start makes `Watch` return the error.
- Not seen: changes another machine makes to a network volume.

A macOS build **without cgo** (`CGO_ENABLED=0`) has no `Watch`: `*local.FS` does not implement
`vfs.Watcher` there, and callers take `vfs.Poll`.
