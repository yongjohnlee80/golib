# vfs

One filesystem interface, swappable drivers: read, write, move and watch files the same way whether
they live on local disk ([`vfs/local`](local/README.md)), in memory ([`vfs/memfs`](memfs/README.md)),
or later in a GCS bucket or behind SFTP.

```go
fsys, err := local.New("/srv/kb")        // *local.FS; hold it as vfs.FS
defer fsys.Close()

fi, err := fsys.WriteFile(ctx, "notes/a.md", strings.NewReader(body)) // atomic replace
rc, err := fsys.Open(ctx, "notes/a.md", 0)                           // stream from an offset
for fi, err := range vfs.Walk(ctx, fsys, ".") { … }                  // the whole tree, parents first
```

## The core and its capabilities

`vfs.FS` holds only what every driver can answer: `Stat`, `ReadDir`, `Open`, `WriteFile`,
`MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `Close`. Everything optional is **its own interface**,
found by type assertion — the pattern `dao` uses for dialect capabilities:

| Interface | Methods | What it adds |
|---|---|---|
| `ConditionalWriter` | `WriteFileIf`, `RemoveIf` | commit only if the entry's `Version` is still the one you read |
| `ExclusiveCreator` | `CreateExclusive` | atomic create-if-absent — the file appears whole or not at all |
| `NoReplaceRenamer` | `RenameNoReplace` | atomic rename that refuses to replace |
| `Watcher` | `Watch` | change events; `vfs.Poll` gives any driver an observational fallback |
| `Copier` | `Copy` | server-side copy |

```go
if c, ok := fsys.(vfs.ConditionalWriter); ok {
	_, err := c.WriteFileIf(ctx, "notes/a.md", r, seen.Version)
	var ce *vfs.ConflictError
	if errors.As(err, &ce) {
		// someone else saved first; ce.Current is what is there now
	}
}
```

There is no `Supports*()` predicate beside the interfaces — a predicate and an interface can disagree.
A capability the platform refuses at runtime (a filesystem without an atomic no-replace rename)
returns `errs.ErrUnsupported`; nothing is emulated with weaker semantics.

| Driver | ConditionalWriter | ExclusiveCreator | NoReplaceRenamer | Watcher | Copier |
|---|---|---|---|---|---|
| `local` (Linux) | ✓ | ✓ | ✓ | ✓ inotify | – |
| `local` (macOS) | ✓ | ✓ | ✓ | – (`Poll`) | – |
| `memfs` | ✓ | ✓ | ✓ | ✓ | ✓ |

## Contracts

- **Names** follow `io/fs.ValidPath` and are relative to the driver's root; `"."` is the root. An
  invalid name — `../x`, `/etc`, `a//b` — fails with `ErrInvalidName` before any I/O.
- **Atomic writes and the commit point.** `WriteFile` replaces a file atomically. An error before the
  commit leaves the old state; an error after it (the parent directory could not be synced, the final
  Stat failed) is a `*CommitError` — `errors.Is(err, vfs.ErrCommitted)` — meaning *this call's change
  took effect*. Stat before retrying.
- **Entries, not targets.** A `FileInfo` describes the directory entry itself: a symlink is reported
  with `fs.ModeSymlink`, never as its target. So the `Version` from `Stat`, `ReadDir` or `Walk` is the
  one the conditions compare, and `Walk` never enters a symlink. Path resolution still follows links
  that stay inside the root.
- **Versions** are opaque tokens that change on every mutation made through the driver. For changes by
  other processes they are best-effort on local disk (a metadata heuristic) and exact on backends that
  version objects.
- **Conditions** are exact among callers of one driver instance. Whether they are exact against other
  writers is stated by each driver; where not, a foreign write can land between check and commit.

## Errors

`*fs.PathError` values wrapping `ErrNotExist` / `ErrExist` (the `io/fs` sentinels), `ErrConflict`
(a `*ConflictError` carrying the current state), `ErrInvalidName`, `ErrCommitted` (a `*CommitError`),
and `golib/errs` kinds (`ErrUnsupported`, `ErrClosed`, `ErrInvalidArgument`).

## Testing a driver

[`vfs/vfstest`](vfstest/README.md) is the conformance suite: `vfstest.TestFS(t, newFS)` runs the core
cells for every driver and one cell group per capability the driver implements.
