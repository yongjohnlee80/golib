package files

import "testing"

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
