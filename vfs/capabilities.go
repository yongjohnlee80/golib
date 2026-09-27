package vfs

import (
	"context"
	"io"
)

// A driver declares a capability by implementing its interface; callers find it by type assertion:
//
//	if c, ok := fsys.(vfs.ConditionalWriter); ok {
//		info, err = c.WriteFileIf(ctx, name, r, seen)
//	}
//
// There is deliberately no Supports*() predicate beside the interfaces: a predicate and an interface
// can disagree. What a capability promises beyond its methods — whether a condition is exact against
// writers outside this driver instance — is part of each driver's documented contract. A capability
// the platform refuses at runtime (a filesystem without an atomic no-replace rename) returns an error
// wrapping github.com/yongjohnlee80/golib/errs.ErrUnsupported, never a weaker emulation.

// ConditionalWriter is implemented by drivers that can make a mutation conditional on the target's
// current [Version]. The check and the commit are one step for every caller of the driver instance.
type ConditionalWriter interface {
	// WriteFileIf is [FS.WriteFile] that commits only if name's current version is want. A mismatch,
	// or a name that no longer exists, fails with a [*ConflictError] carrying the current state.
	WriteFileIf(ctx context.Context, name string, r io.Reader, want Version, opts ...WriteOption) (FileInfo, error)

	// RemoveIf is [FS.Remove] that removes only if name's current version is want.
	RemoveIf(ctx context.Context, name string, want Version) error
}

// ExclusiveCreator is implemented by drivers with an atomic create-if-absent.
type ExclusiveCreator interface {
	// CreateExclusive writes r's content to name only if nothing exists there. The file appears whole
	// or not at all. An existing entry fails with a [*ConflictError] carrying it.
	CreateExclusive(ctx context.Context, name string, r io.Reader, opts ...WriteOption) (FileInfo, error)
}

// NoReplaceRenamer is implemented by drivers with an atomic rename that refuses to replace.
type NoReplaceRenamer interface {
	// RenameNoReplace moves from to to — a file or a directory — only if nothing exists at to. An
	// existing entry fails with a [*ConflictError] carrying it.
	RenameNoReplace(ctx context.Context, from, to string) error
}

// Watcher is implemented by drivers that can deliver change events. Callers find it by type assertion
// and fall back to [Poll] when it is absent:
//
//	events, err := func() (<-chan vfs.Event, error) {
//		if w, ok := fsys.(vfs.Watcher); ok {
//			return w.Watch(ctx, "docs", vfs.Recursive())
//		}
//		return vfs.Poll(ctx, fsys, "docs", 2*time.Second, vfs.Recursive())
//	}()
type Watcher interface {
	// Watch streams events for entries under dir until ctx ends; the channel then closes. OpOverflow
	// means events were lost for Event.Path's subtree ("" for the whole watch) and the consumer must
	// re-read it: nothing is dropped silently.
	Watch(ctx context.Context, dir string, opts ...WatchOption) (<-chan Event, error)
}

// Copier is implemented by drivers with a server-side copy. Drivers without one do not emulate it;
// callers Open and WriteFile.
type Copier interface {
	// Copy copies the file at from to to, replacing an existing file, and returns the new entry.
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
