package paths

import "errors"

var (
	// ErrInvalidLogical is a string that is not a logical path.
	ErrInvalidLogical = errors.New("paths: not a logical path")
	// ErrNotAbsolute is a path that had to be absolute and isn't, in this OS's form.
	ErrNotAbsolute = errors.New("paths: not an absolute path")
	// ErrOutside is a path that is not below the root it was asked about.
	ErrOutside = errors.New("paths: outside the root")
	// ErrRefused is a logical element this OS would read as something else.
	ErrRefused = errors.New("paths: an element this OS would read as another")
	// ErrNoHome is a ~ with no home folder to expand it to.
	ErrNoHome = errors.New("paths: ~ with no home folder")
	// ErrNotFileURL is a string that is not a file: URL this OS can name.
	ErrNotFileURL = errors.New("paths: not a file URL for this OS")
	// ErrNotCanonical is an element [Encode] would not have written.
	ErrNotCanonical = errors.New("paths: not an encoded name")
	// ErrNotReversible is the hashed form of a name too long for one element: it names the file, but
	// the name cannot be recovered from it.
	ErrNotReversible = errors.New("paths: a hashed name cannot be reversed")
)
