// Package vfs is a filesystem interface with swappable drivers: one surface to read, write, watch and
// move files whether they live on local disk ([github.com/yongjohnlee80/golib/vfs/local]), in memory
// ([github.com/yongjohnlee80/golib/vfs/memfs]), or — later — in a GCS bucket or behind SFTP.
//
// # Names
//
// Every name is relative to the driver's root and follows [io/fs.ValidPath]: slash-separated, unrooted,
// no "." or ".." elements, no empty elements. "." alone names the root. An invalid name fails with
// [ErrInvalidName] before any I/O, which rules out every lexical escape ("../x", "/etc", "a/../../x").
// Escapes that exist only on disk — a symlink inside the root pointing outside it — are refused by the
// driver (the local driver resolves every name through [os.Root]).
//
// Errors are [*io/fs.PathError] values wrapping this package's sentinels or the driver's own error, as
// the standard library does.
//
// # Entries, not targets
//
// A [FileInfo] describes the directory entry itself: a symlink is reported with fs.ModeSymlink, never as
// its target. Stat, ReadDir and [Walk] all report entries this way, so the Version they return is the
// one conditional mutations compare, Remove and Rename act on the link itself, and Walk never enters a
// link. Path resolution follows links that stay inside the root.
//
// # Atomic writes and the commit point
//
// [FS.WriteFile] replaces a file atomically: a reader sees the old content or the new, never a mix. An
// error before the commit point leaves the old state intact. An error after it (the parent directory
// could not be synced, or the final Stat failed) is a [*CommitError]: this call's change took effect,
// and a caller must Stat before retrying. [errors.Is](err, [ErrCommitted]) tells the two apart.
//
// # Versions
//
// [FileInfo.Version] is an opaque token that changes on every mutation made through the driver. For
// changes made by other processes it is best-effort on local disk (a metadata heuristic) and exact on
// backends that version objects.
//
// # Capabilities
//
// [FS] holds only what every driver answers. Each optional ability is its own interface, found by type
// assertion:
//
//   - [ConditionalWriter] — WriteFileIf / RemoveIf against a Version; a stale one is a [*ConflictError]
//   - [ExclusiveCreator] — CreateExclusive, an atomic create-if-absent
//   - [NoReplaceRenamer] — RenameNoReplace, an atomic rename that refuses to replace
//   - [Watcher] — change events; [Poll] gives any driver an observational stream by comparing versions
//   - [Copier] — a server-side copy
//
// A driver never emulates an ability with weaker semantics; a capability the platform refuses at
// runtime returns [github.com/yongjohnlee80/golib/errs.ErrUnsupported].
//
// Conditions are exact among callers of one driver instance. Whether they are also exact against other
// writers — other processes editing the files directly — is stated by each driver. Where they are not,
// a foreign write can land between the check and the commit and be overwritten; watching can only
// detect that on a best-effort basis, never prevent it.
package vfs
