package files

import (
	"io/fs"
	"path"
	"strings"
)

// The paths of Windows' local disk as an fs.FS sees them, which [LocalFiles] reads there: an fs path
// begins with its drive ("C:/Users/me"), "." is the list of drives, and a rooted path, what a person
// and a host read, is Windows' own ("C:\Users\me"). Plain string work, so it builds and is tested
// on every OS; only listing the drives and opening a path are Windows' (drivesfs_windows.go).

// winSep is Windows' separator. A path of the drives is written with it, and may be typed with it
// or with a slash: this file is the one place the files widgets know it.
const winSep = `\`

// driveSlashed is a typed path of the drives with Windows' separators made slashes.
func driveSlashed(typed string) string { return strings.ReplaceAll(typed, winSep, "/") }

// driveAbsolute reports whether a typed path of the drives is rooted: it names a drive, or starts
// at a separator (the current drive's root, which driveFSPath then refuses as naming none).
func driveAbsolute(typed string) bool {
	p := driveSlashed(typed)
	return driveOf(p) != "" || strings.HasPrefix(p, "/")
}

// driveOf is p's drive, upper-cased with its colon ("C:"), when p, slash-separated, begins with one:
// a letter, a colon, then nothing or a slash. "" otherwise.
func driveOf(p string) string {
	if len(p) < 2 || p[1] != ':' || (len(p) > 2 && p[2] != '/') {
		return ""
	}
	c := p[0] | 0x20 // lower-cased, if a letter
	if c < 'a' || c > 'z' {
		return ""
	}
	return strings.ToUpper(p[:1]) + ":"
}

// driveRooted writes an fs path of the drives as Windows does: "." (the list of drives) → "",
// "C:" → "C:\", "C:/Users/me" → "C:\Users\me".
func driveRooted(p string) string {
	if p == "." || p == "" {
		return ""
	}
	vol, rest, _ := strings.Cut(p, "/")
	return vol + winSep + strings.ReplaceAll(rest, "/", winSep)
}

// driveFSPath reads a Windows path back into an fs path of the drives ("C:\Users\..\me" →
// "C:/me"), reporting false for one that names no drive (a relative path, \\server\share, \rooted)
// or holds what Windows would read as more than a name: a colon (an alternate data stream) past
// the drive. The empty path is the list of drives.
func driveFSPath(rooted string) (string, bool) {
	p := driveSlashed(rooted)
	if p == "" {
		return ".", true
	}
	vol := driveOf(p)
	if vol == "" {
		return "", false
	}
	rest := path.Clean("/" + p[2:])[1:]
	if rest == "" {
		return vol, true
	}
	if strings.Contains(rest, ":") {
		return "", false
	}
	q := vol + "/" + rest
	return q, fs.ValidPath(q)
}

// drivePath is an fs path of the drives as the OS opens it: "C:" → "C:\", "C:/Users" →
// "C:\Users"; false for "." and for a path driveFSPath would refuse.
func drivePath(name string) (string, bool) {
	if name == "." || !fs.ValidPath(name) || strings.Contains(name, winSep) {
		return "", false
	}
	if q, ok := driveFSPath(name); !ok || q != name {
		return "", false
	}
	return driveRooted(name), true
}
