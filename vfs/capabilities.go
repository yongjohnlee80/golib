package vfs

import (
	"context"
	"io"
)

// Capability Discovery Pattern:
// A driver advertises optional functionality by implementing the corresponding interface.
// Consumers discover supported features dynamically via Go type assertion:
//
//	if cw, ok := fsys.(vfs.ConditionalWriter); ok {
//		info, err = cw.WriteFileIf(ctx, name, r, seen)
//	}
//
// Semantics and Invariants:
// There are deliberately no boolean Supports*() methods beside these interfaces to prevent interface-state
// drift. If a platform refuses a capability at runtime (such as atomic no-replace renames on an unsupported
// filesystem), the driver returns an error wrapping [github.com/yongjohnlee80/golib/errs.ErrUnsupported]
// rather than degrading to a non-atomic or weaker emulation.

// ConditionalWriter is implemented by drivers that support optimistic concurrency control
// conditioned upon the target's current [Version].
//
// Atomicity:
// For all callers of a driver instance, the version comparison and subsequent mutation are executed
// as a single atomic operation.
type ConditionalWriter interface {
	// WriteFileIf atomically executes [FS.WriteFile] if and only if the current version of name matches want.
	// If the current version differs, or if name no longer exists, it returns a [*ConflictError]
	// carrying the target's current state.
	WriteFileIf(ctx context.Context, name string, r io.Reader, want Version, opts ...WriteOption) (FileInfo, error)

	// RemoveIf atomically executes [FS.Remove] if and only if the current version of name matches want.
	// If the current version differs, or if name no longer exists, it returns a [*ConflictError].
	RemoveIf(ctx context.Context, name string, want Version) error
}

// ExclusiveCreator is implemented by drivers supporting atomic create-if-absent semantics.
type ExclusiveCreator interface {
	// CreateExclusive writes r's data to name only if nothing currently exists at name.
	// The file appears completely or not at all. If an entry already exists at name, it returns
	// a [*ConflictError] containing the existing entry's metadata.
	CreateExclusive(ctx context.Context, name string, r io.Reader, opts ...WriteOption) (FileInfo, error)
}

// NoReplaceRenamer is implemented by drivers supporting atomic rename that refuses to overwrite targets.
type NoReplaceRenamer interface {
	// RenameNoReplace moves from to to (file or directory) only if no entry exists at to.
	// If an entry already exists at to, it returns a [*ConflictError] containing the destination's metadata.
	RenameNoReplace(ctx context.Context, from, to string) error
}

// Watcher is implemented by drivers capable of streaming real-time filesystem change events.
// Callers discover Watcher by type assertion and fall back to [Poll] when it is not implemented:
//
//	events, err := func() (<-chan vfs.Event, error) {
//		if w, ok := fsys.(vfs.Watcher); ok {
//			return w.Watch(ctx, "docs", vfs.Recursive())
//		}
//		return vfs.Poll(ctx, fsys, "docs", 2*time.Second, vfs.Recursive())
//	}()
type Watcher interface {
	// Watch streams mutation events under dir until ctx is cancelled, closing the returned channel upon termination.
	//
	// Overflow Handling:
	// If event queues overflow or platform events are lost, [OpOverflow] is emitted for the affected
	// subtree (or empty path "" for the entire watch tree), signaling that the consumer must resynchronize.
	Watch(ctx context.Context, dir string, opts ...WatchOption) (<-chan Event, error)
}

// Copier is implemented by drivers with native, server-side file copy mechanisms.
// Drivers without native copy do not emulate it; callers must stream using [FS.Open] and [FS.WriteFile].
type Copier interface {
	// Copy clones the file at from to to, replacing any existing file at to, and returns the new entry's metadata.
	Copy(ctx context.Context, from, to string, opts ...WriteOption) (FileInfo, error)
}

// Op is the kind of an [Event].
type Op uint8

const (
	// OpCreate: an entry appeared at Path — created, or moved or renamed into place (an editor's
	// atomic save arrives as a create). Consumers usually treat it like OpWrite.
	OpCreate Op = iota + 1
	// OpWrite: the file at Path changed.
	OpWrite
	// OpRemove: the entry at Path disappeared — deleted, or moved or renamed away. A rename is an
	// OpRemove of the old name and an OpCreate of the new one.
	OpRemove
	// OpOverflow: events were lost for Path's subtree ("" for everything watched); re-read it.
	OpOverflow
)

func (o Op) String() string {
	switch o {
	case OpCreate:
		return "create"
	case OpWrite:
		return "write"
	case OpRemove:
		return "remove"
	case OpOverflow:
		return "overflow"
	}
	return "unknown"
}

// Event is one change under a watched directory.
type Event struct {
	Path string // root-relative, slash-separated
	Op   Op
}
