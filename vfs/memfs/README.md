# vfs/memfs

An in-memory `vfs.FS` for tests and for code that wants the vfs contract without a disk. It implements
every capability — `ConditionalWriter`, `ExclusiveCreator`, `NoReplaceRenamer`, `Watcher`, `Copier` —
and since no other writer can exist, every condition is exact.

```go
fsys := memfs.New()
```

Every mutation happens under one lock, so renames and no-replace commits are atomic. Versions are a
counter; each mutation gives the touched entries (and the parent directory) a new one, the way a disk
moves a directory's mtime. Watch events are queued, never dropped, and arrive in mutation order.
`memfs.WithClock` replaces `time.Now` for deterministic modification times.
