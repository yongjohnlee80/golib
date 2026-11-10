package paths

import (
	"fmt"
	"io/fs"
	"strings"
)

// LogicalSep is a [Logical] path's separator, on every OS.
const LogicalSep = "/"

// Logical is a place in a tree, a URL's path or a value on the wire: slash-separated, relative, with
// no "." or ".." element. It is a struct so that a plain conversion cannot make an unchecked one:
// [ParseLogical] is the boundary. The zero value is the root, ".".
type Logical struct {
	s string // "" for the root
}

// ParseLogical checks s as a logical path: valid by [io/fs.ValidPath], and with no backslash, which
// some OS would read as a separator. "." is the root.
func ParseLogical(s string) (Logical, error) {
	if !fs.ValidPath(s) || strings.Contains(s, `\`) {
		return Logical{}, fmt.Errorf("%w: %q", ErrInvalidLogical, s)
	}
	if s == "." {
		return Logical{}, nil
	}
	return Logical{s: s}, nil
}

// String is the path, "." for the root.
func (l Logical) String() string {
	if l.s == "" {
		return "."
	}
	return l.s
}

// IsRoot reports whether l is the root.
func (l Logical) IsRoot() bool { return l.s == "" }

// Join is l with elem appended, each of them checked as ParseLogical checks.
func (l Logical) Join(elem ...string) (Logical, error) {
	all := elem
	if l.s != "" {
		all = append([]string{l.s}, elem...)
	}
	return ParseLogical(strings.Join(all, LogicalSep))
}

// MarshalText is the path, for the wire.
func (l Logical) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

// UnmarshalText checks the wire's value as [ParseLogical] does.
func (l *Logical) UnmarshalText(b []byte) error {
	v, err := ParseLogical(string(b))
	if err != nil {
		return err
	}
	*l = v
	return nil
}
