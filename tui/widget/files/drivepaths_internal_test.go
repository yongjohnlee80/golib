package files

import (
	"testing"
	"testing/fstest"
)

// Windows' local disk, as the file widgets write and read its paths: built and run on every OS,
// since it is string work alone.
func TestWindowsDrivePathsRoundTrip(t *testing.T) {
	src := FileSource{drives: true, Home: `C:\Users\me`}
	for _, c := range []struct{ fsPath, rooted string }{
		{".", ""},
		{"C:", `C:\`},
		{"C:/Users/me", `C:\Users\me`},
		{"D:/data/a b.txt", `D:\data\a b.txt`},
	} {
		if got := src.Rooted(c.fsPath); got != c.rooted {
			t.Errorf("Rooted(%q) = %q, want %q", c.fsPath, got, c.rooted)
		}
		if got, ok := src.fsPath(c.rooted); !ok || got != c.fsPath {
			t.Errorf("fsPath(%q) = %q, %v; want %q", c.rooted, got, ok, c.fsPath)
		}
	}
}

// A Windows path is read as Windows reads it: either separator, either case of drive letter, . and
// .. resolved; a path naming no drive, or with a colon past it, is outside the source.
func TestWindowsDrivePathsReadAsWindowsDoes(t *testing.T) {
	src := FileSource{drives: true}
	for _, c := range []struct {
		rooted, want string
		ok           bool
	}{
		{`c:\Users\me\`, "C:/Users/me", true},
		{"C:/Users/me/../you", "C:/Users/you", true},
		{`C:\..\..`, "C:", true},
		{"C:", "C:", true},
		{`C:\a\stream.txt:hidden`, "", false},
		{`Users\me`, "", false},
		{`\\server\share\x`, "", false},
		{`\rooted`, "", false},
		{`1:\x`, "", false},
		{`CC:\x`, "", false},
	} {
		got, ok := src.fsPath(c.rooted)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("fsPath(%q) = %q, %v; want %q, %v", c.rooted, got, ok, c.want, c.ok)
		}
	}
}

// What a person types is rooted when it names a drive or starts with a separator; anything else is
// relative to the folder listed. A source with a Root keeps its own rule.
func TestWindowsDrivesTellARootedTypedPath(t *testing.T) {
	src := FileSource{drives: true}
	for typed, want := range map[string]bool{`C:\x`: true, "d:/x": true, "C:": true, `\x`: true, "/x": true, "x": false, `x\y`: false, "CC:/x": false} {
		if got := src.absolute(typed); got != want {
			t.Errorf("absolute(%q) = %v, want %v", typed, got, want)
		}
	}
	if !src.local() {
		t.Error("Windows' local disk is not local")
	}
	sftp := FileSource{Root: "sftp://host/"}
	if !sftp.absolute("sftp://host/x") || !sftp.absolute("/x") || sftp.absolute("x") || sftp.local() {
		t.Error("a source with a Root lost its own rule")
	}
}

// drivePath, the path Windows opens, is given only for an fs path the source would itself write.
func TestWindowsDrivePathIsOnlyForTheSourcesOwnPaths(t *testing.T) {
	for name, want := range map[string]string{"C:": `C:\`, "C:/Users/me": `C:\Users\me`} {
		if got, ok := drivePath(name); !ok || got != want {
			t.Errorf("drivePath(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
	for _, name := range []string{".", "c:/x", "C:/a/../b", `C:/a\b`, "C:/a:b", "Users", "C:/"} {
		if got, ok := drivePath(name); ok {
			t.Errorf("drivePath(%q) = %q; want it refused", name, got)
		}
	}
}

// A path typed on Windows' local disk is read with either separator, relative to the folder listed
// or rooted at a drive, and must land on a drive; elsewhere a backslash is part of a name.
func TestWindowsDrivesReadATypedPath(t *testing.T) {
	src := FileSource{drives: true}
	for _, c := range []struct {
		dir, typed, want string
		ok               bool
	}{
		{"C:/base", `sub\file.txt`, "C:/base/sub/file.txt", true},
		{"C:/base", `..\peer`, "C:/peer", true},
		{"C:/base", `..\..`, ".", true},
		{"C:/base", `D:\x`, "D:/x", true},
		{"C:/base", "sub/x", "C:/base/sub/x", true},
		{"C:/base", `a:b`, "", false},
		{".", "notadrive", "", false},
	} {
		got, ok := src.typedPath(c.dir, c.typed)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("typedPath(%q, %q) = %q, %v; want %q, %v", c.dir, c.typed, got, ok, c.want, c.ok)
		}
	}
	unix := FileSource{Root: "/"}
	if got, ok := unix.typedPath("home", `a\b`); !ok || got != `home/a\b` {
		t.Errorf("a source with a Root read a backslash as a separator: %q, %v", got, ok)
	}
}

// ~\ in a path field on Windows' local disk is the source's Home, as ~/ is.
func TestWindowsDrivesTildeTakesEitherSeparator(t *testing.T) {
	src := FileSource{drives: true, Home: `C:\Users\me`}
	for _, typed := range []string{`~\notes`, "~/notes"} {
		if p, _, ok := (&pathField{}).resolve(src, "D:/elsewhere", typed); !ok || p != "C:/Users/me/notes" {
			t.Errorf("resolve(%q) = %q, %v; want C:/Users/me/notes", typed, p, ok)
		}
	}
}

// The save view's own entry, Selected, reads a name typed on Windows' local disk with either
// separator, relative to the folder listed or rooted, and refuses one that lands on no drive. The
// source is a MapFS standing for the drives, so the cell runs on every OS.
func TestASaveViewNamesAWindowsPathTypedEitherWay(t *testing.T) {
	src := FileSource{drives: true, FS: fstest.MapFS{"C:/base/x.md": {}}}
	v := NewFileSaveView(WithFileViewSource(src))
	v.listing.list.dir = "C:/base"
	for typed, want := range map[string]string{
		`sub\file.txt`: `C:\base\sub\file.txt`,
		`..\peer.txt`:  `C:\peer.txt`,
		`D:\x\y.md`:    `D:\x\y.md`,
		"sub/z.md":     `C:\base\sub\z.md`,
		`a:b`:          "",
	} {
		v.SetName(typed)
		if got := v.Selected(); got != want {
			t.Errorf("Selected after the name %q = %q, want %q", typed, got, want)
		}
	}
}

// A path of the drives holding an element Windows opens as a device (NUL, con.txt, COM1, in any
// case, with an extension or trailing dots or spaces) names no file: driveFSPath and drivePath
// refuse it, as its last element or a folder. A near name (CONSOLE, nul_, COM10, LPT) is a file.
func TestADrivePathHoldingADeviceIsRefused(t *testing.T) {
	for _, d := range []string{"NUL", "nul.txt", "Con", "CON.tar.gz", "NUL.", "nul .md", "COM1", "lpt9", "COM\u00b9", "conin$"} {
		for _, rooted := range []string{`C:\a\` + d, `C:\a\` + d + `\x.md`, "C:/" + d} {
			if got, ok := driveFSPath(rooted); ok {
				t.Errorf("driveFSPath(%q) = %q, true; want refused: a device", rooted, got)
			}
		}
		if got, ok := drivePath("C:/a/" + d); ok {
			t.Errorf("drivePath(C:/a/%s) = %q, true; want refused: a device", d, got)
		}
	}
	for _, n := range []string{"CONSOLE", "nul_", "COM10", "LPT", "anul"} {
		if got, ok := driveFSPath(`C:\a\` + n); !ok || got != "C:/a/"+n {
			t.Errorf("driveFSPath(C:\\a\\%s) = %q, %v; want the file C:/a/%s", n, got, ok, n)
		}
		if got, ok := drivePath("C:/a/" + n); !ok || got != `C:\a\`+n {
			t.Errorf("drivePath(C:/a/%s) = %q, %v; want the file", n, got, ok)
		}
	}
}
