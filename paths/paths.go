package paths

import (
	"os"
	"path/filepath"
)

// OS is a path as the file system takes it, in the OS's own form. Its separator is [Sep]'s.
type OS string

// Sep is this OS's path separator.
func Sep() string { return string(os.PathSeparator) }

// Join is base with elem appended, by [filepath.Join]. elem are OS path elements: a name from data
// goes through [Encode] first, and a logical path through [Under].
func Join(base OS, elem ...string) OS {
	return OS(filepath.Join(append([]string{string(base)}, elem...)...))
}

// IsAbs reports whether p is absolute on this OS.
func IsAbs(p OS) bool { return filepath.IsAbs(string(p)) }

// Within reports whether p is root or below it, compared element by element, case-blind where the
// OS is. On Windows it normalizes what Windows does: either separator, "." and "..", and trailing
// dots and spaces. A relative path is within nothing.
//
// It is lexical, and it is not containment. It cannot see a symlink, a junction or an 8.3 short name,
// and a \\?\ path compares as different from its plain spelling. What a program may reach is decided
// where the file is opened, by a root such as [os.Root] or vfs/local.
func Within(root, p OS) bool { return within(string(root), string(p), windowsRules) }

// Rel is p's place below root, or [ErrOutside] when it is not [Within] it.
func Rel(root, p OS) (Logical, error) { return rel(string(root), string(p), windowsRules) }

// Under is l's OS path below root. root must be absolute ([ErrNotAbsolute]). On Windows, an element
// Windows would read as another is [ErrRefused]: a trailing dot or space, a reserved device name, a
// character it does not allow, or the shape of an 8.3 short name. A real file so named is unreachable
// through Under, and the error says why.
//
// Like [Within] it is lexical: a symlink or junction below root is for whoever opens the path.
func Under(root OS, l Logical) (OS, error) {
	p, err := under(string(root), l, windowsRules)
	return OS(p), err
}

// Expand is s with a leading ~ replaced by home: "~", "~/x" and, on Windows, "~\x". Anything else,
// "~user" included, is returned as it is. A ~ with an empty home is [ErrNoHome].
func Expand(s, home string) (OS, error) {
	p, err := expand(s, home, windowsRules)
	return OS(p), err
}

// URL is p's file: URL: file:///home/me, file:///C:/a%20b, file://server/share/x. p must be
// absolute ([ErrNotAbsolute]).
func URL(p OS) (string, error) { return toURL(string(p), windowsRules) }

// FromURL is the OS path a file: URL names, or [ErrNotFileURL]: for another scheme, a query or
// fragment, an encoded separator, or a form this OS has no path for (a host on Unix, no drive or
// share on Windows).
func FromURL(u string) (OS, error) {
	p, err := fromURL(u, windowsRules)
	return OS(p), err
}
