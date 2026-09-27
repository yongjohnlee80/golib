package vfs

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/yongjohnlee80/golib/errs"
)

var (
	// ErrNotExist is io/fs.ErrNotExist: the named entry does not exist.
	ErrNotExist = fs.ErrNotExist

	// ErrExist is io/fs.ErrExist: the named entry already exists.
	ErrExist = fs.ErrExist

	// ErrConflict means a condition failed: a stale version ([ConditionalWriter]) or an existing target
	// ([ExclusiveCreator], [NoReplaceRenamer]). The error is a [*ConflictError] carrying the current state.
	ErrConflict = errs.Sentinel(errs.ErrPrecondition, "vfs: version conflict")

	// ErrInvalidName means a name is not a valid path under the root (see [io/fs.ValidPath]). Every
	// lexical escape — "..", an absolute name — fails with it before any I/O.
	ErrInvalidName = errs.Sentinel(errs.ErrInvalidArgument, "vfs: invalid name")

	// ErrCommitted marks an error that happened after the commit point: this call's change took
	// effect. It does not promise the bytes still occupy the name — another writer may have replaced
	// them since. The error is a [*CommitError].
	ErrCommitted = errors.New("vfs: committed; a follow-up step failed")
)

// ConflictError is returned when a write condition fails. errors.Is(err, ErrConflict) is true.
type ConflictError struct {
	Path    string   // the name the condition applied to
	Want    Version  // the version the caller required; "" for an exclusive create or no-replace rename
	Current FileInfo // the entry as it is now; the zero FileInfo when it does not exist
}

func (e *ConflictError) Error() string {
	if e.Want == "" {
		return fmt.Sprintf("vfs: %s: already exists (%v)", e.Path, ErrConflict)
	}
	if e.Current.Path == "" {
		return fmt.Sprintf("vfs: %s: want version %s, no longer exists (%v)", e.Path, e.Want, ErrConflict)
	}
	return fmt.Sprintf("vfs: %s: want version %s, have %s (%v)", e.Path, e.Want, e.Current.Version, ErrConflict)
}

// Unwrap reports ErrConflict.
func (e *ConflictError) Unwrap() error { return ErrConflict }

// CommitError is a failure after the commit point: this call's change took effect, then a follow-up
// step (syncing the parent directory, cleaning up, the final Stat) failed. errors.Is(err, ErrCommitted)
// is true, and so is errors.Is(err, Err).
type CommitError struct {
	Path string   // the name that was changed
	Info FileInfo // the new entry; the zero FileInfo when the failing step was the final Stat
	Err  error    // the follow-up step's error
}

func (e *CommitError) Error() string {
	return fmt.Sprintf("vfs: %s: committed, then: %v", e.Path, e.Err)
}

// Unwrap reports ErrCommitted and the follow-up step's error.
func (e *CommitError) Unwrap() []error { return []error{ErrCommitted, e.Err} }
