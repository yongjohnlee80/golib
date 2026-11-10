package paths

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The OS differences these cells cover: the separator, drive letters and UNC shares, case, trailing
// dots and spaces, device names, 8.3 short names, alternate data streams, and ~\. Each rule runs
// with both values of win, on every OS.

func TestParseLogical(t *testing.T) {
	for _, s := range []string{".", "a", "a/b.md", "a b/c:d"} {
		l, err := ParseLogical(s)
		if err != nil || l.String() != s {
			t.Errorf("ParseLogical(%q) = %q, %v", s, l, err)
		}
	}
	for _, s := range []string{"", "/a", "a/", "a//b", "../a", "a/../b", "a/./b", `a\b`, "\xff"} {
		if l, err := ParseLogical(s); !errors.Is(err, ErrInvalidLogical) {
			t.Errorf("ParseLogical(%q) = %q, %v; want ErrInvalidLogical", s, l, err)
		}
	}
	var zero Logical
	if zero.String() != "." || !zero.IsRoot() {
		t.Errorf("the zero Logical is %q", zero)
	}
	child, err := zero.Join("a", "b")
	if err != nil || child.String() != "a/b" {
		t.Errorf("Join from the root: %q, %v", child, err)
	}
	if _, err := child.Join(".."); err == nil {
		t.Error("Join of .. was accepted")
	}
	var wire Logical
	if err := wire.UnmarshalText([]byte("../x")); err == nil {
		t.Error("an invalid path came in over the wire")
	}
	if err := wire.UnmarshalText([]byte("a/b")); err != nil || wire != child {
		t.Errorf("UnmarshalText: %v, %v", wire, err)
	}
}

func TestWithin(t *testing.T) {
	for _, c := range []struct {
		win     bool
		root, p string
		want    bool
	}{
		{false, "/r", "/r", true},
		{false, "/r", "/r/a/b", true},
		{false, "/r", "/rx", false},
		{false, "/r", "/r/../x", false},
		{false, "/r", "/R/a", false}, // Unix is case-sensitive
		{false, "/r", "r/a", false},
		{false, "/", "/a", true},
		{true, `C:\r`, `C:\r\a`, true},
		{true, `C:\r`, `c:/R/a`, true}, // either separator, case-blind volume and elements
		{true, `C:\r`, `C:\rx`, false},
		{true, `C:\r`, `C:\r\..\x`, false},
		{true, `C:\r`, `C:\r\a\..\b`, true},
		{true, `C:\r`, `D:\r\a`, false},
		{true, `C:\r\foo`, `C:\r\foo.\a`, true}, // Windows drops a trailing dot
		{true, `C:\r\foo`, `C:\r\foo \a`, true}, // and a trailing space
		{true, `C:\r\a.b`, `C:\r\a.b\c`, true},
		{true, `C:\r`, `C:\r\a:stream`, false}, // an alternate data stream
		{true, `C:\r`, `C:r\a`, false},         // relative to the drive's current folder
		{true, `C:\r`, `\r\a`, false},          // rooted, on no named drive
		{true, `C:\r`, `\\?\C:\r\a`, false},    // verbatim forms are not its plain spelling
		{true, `\\s\share`, `\\S\SHARE\a`, true},
		{true, `\\s\share\r`, `\\s\other\r\a`, false},
		{true, `\\s\share`, `\\s`, false},
		{true, `C:\`, `C:\a`, true},
		{true, `C:\`, `C:\..\..\a`, true}, // .. stops at the volume
	} {
		if got := within(c.root, c.p, c.win); got != c.want {
			t.Errorf("within(%q, %q, win=%v) = %v, want %v", c.root, c.p, c.win, got, c.want)
		}
	}
}

func TestRelAndUnder(t *testing.T) {
	for _, c := range []struct {
		win        bool
		root, p, l string
	}{
		{false, "/r", "/r", "."},
		{false, "/r", "/r/a/b.md", "a/b.md"},
		{true, `C:\r`, `C:\r`, "."},
		{true, `C:\r`, `c:\R\a\b.md`, "a/b.md"},
		{true, `\\s\share\r`, `\\s\share\r\a`, "a"},
	} {
		got, err := rel(c.root, c.p, c.win)
		if err != nil || got.String() != c.l {
			t.Errorf("rel(%q, %q, win=%v) = %q, %v; want %q", c.root, c.p, c.win, got, err, c.l)
		}
	}
	if _, err := rel("/r", "/x", false); !errors.Is(err, ErrOutside) {
		t.Errorf("rel outside: %v", err)
	}
	if _, err := rel(`C:\r`, `D:\r\a`, true); !errors.Is(err, ErrOutside) {
		t.Errorf("rel on another drive: %v", err)
	}
	if _, err := rel("/r", `/r/a\b`, false); !errors.Is(err, ErrInvalidLogical) {
		t.Errorf("a Unix name holding a backslash has no logical form: %v", err)
	}
	must := func(s string) Logical {
		l, err := ParseLogical(s)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	for _, c := range []struct {
		win           bool
		root, l, want string
	}{
		{false, "/r", ".", "/r"},
		{false, "/r/", "a/b", "/r/a/b"},
		{true, `C:\r`, "a/b.md", `C:\r\a\b.md`},
		{true, `C:\`, "a", `C:\a`},
		{true, `\\s\share`, "a", `\\s\share\a`},
		{false, `/r\`, "a", `/r\/a`}, // on Unix a backslash is part of the folder's name
		{false, "/r//", "a", "/r/a"},
		{true, `C:\r\`, "a", `C:\r\a`},
		{true, "C:/r/", "a", `C:/r\a`},
		{false, "/r", "con", "/r/con"}, // Unix reserves none of these
		{false, "/r", "a./b ", "/r/a./b "},
		{false, "/r", "PROGRA~1", "/r/PROGRA~1"},
	} {
		got, err := under(c.root, must(c.l), c.win)
		if err != nil || got != c.want {
			t.Errorf("under(%q, %q, win=%v) = %q, %v; want %q", c.root, c.l, c.win, got, err, c.want)
		}
	}
	for _, e := range []string{"a.", "a ", "b/a.", "con", "NUL.txt", "sub/aux", "com1", "a:b", "a<b", "a|b", `a"b`, "a?", "a*", "PROGRA~1", "report~12.md"} {
		if got, err := under(`C:\r`, must(e), true); !errors.Is(err, ErrRefused) {
			t.Errorf("under of %q on Windows = %q, %v; want ErrRefused", e, got, err)
		}
	}
	for _, e := range []string{"console", "a.b", "a~b", "toolong~1.md", "x~1.jpeg", "~1"} {
		if _, err := under(`C:\r`, must(e), true); err != nil {
			t.Errorf("under of %q on Windows: %v", e, err)
		}
	}
	for _, root := range []string{"r", `C:r`} {
		if _, err := under(root, must("a"), true); !errors.Is(err, ErrNotAbsolute) {
			t.Errorf("under a relative root %q: %v", root, err)
		}
	}
}

func TestExpand(t *testing.T) {
	for _, c := range []struct {
		win           bool
		s, home, want string
	}{
		{false, "~", "/h", "/h"},
		{false, "~/x/y", "/h", "/h/x/y"},
		{false, `~\x`, "/h", `~\x`}, // a backslash is part of a Unix name
		{false, "~user/x", "/h", "~user/x"},
		{false, "~/x", `/h\`, `/h\/x`}, // on Unix a backslash is part of the home's name
		{false, "~/x", "/h/", "/h/x"},
		{false, "/a", "", "/a"},
		{true, "~", `C:\h`, `C:\h`},
		{true, `~\x\y`, `C:\h`, `C:\h\x\y`},
		{true, "~/x/y", `C:\h\`, `C:\h\x\y`},
		{true, `C:\a`, "", `C:\a`},
	} {
		if got, err := expand(c.s, c.home, c.win); err != nil || got != c.want {
			t.Errorf("expand(%q, %q, win=%v) = %q, %v; want %q", c.s, c.home, c.win, got, err, c.want)
		}
	}
	for _, s := range []string{"~", "~/x"} {
		if _, err := expand(s, "", false); !errors.Is(err, ErrNoHome) {
			t.Errorf("expand(%q) with no home: %v", s, err)
		}
	}
}

func TestFileURLs(t *testing.T) {
	for _, c := range []struct {
		win    bool
		p, url string
	}{
		{false, "/home/u/a b.md", "file:///home/u/a%20b.md"},
		{false, "/", "file:///"},
		{true, `C:\Users\a b.md`, "file:///C:/Users/a%20b.md"},
		{true, `C:\`, "file:///C:/"},
		{true, `\\server\share\a.md`, "file://server/share/a.md"},
	} {
		got, err := toURL(c.p, c.win)
		if err != nil || got != c.url {
			t.Errorf("toURL(%q, win=%v) = %q, %v; want %q", c.p, c.win, got, err, c.url)
		}
		back, err := fromURL(c.url, c.win)
		if err != nil || back != c.p {
			t.Errorf("fromURL(%q, win=%v) = %q, %v; want %q", c.url, c.win, back, err, c.p)
		}
	}
	if got, err := fromURL("file://localhost/a", false); err != nil || got != "/a" {
		t.Errorf("localhost: %q, %v", got, err)
	}
	if got, err := fromURL("file://server/share", true); err != nil || got != `\\server\share` {
		t.Errorf("a bare share: %q, %v", got, err)
	}
	if got, err := fromURL("file:///C:", true); err != nil || got != `C:\` {
		t.Errorf("a bare drive: %q, %v", got, err)
	}
	for _, c := range []struct {
		win bool
		u   string
	}{
		{false, "http://x/a"}, {false, "file://server/a"}, {false, "file:///a?q=1"}, {false, "file:///a#f"},
		{false, "file:///a%2Fb"}, {false, "file:///a%00b"},
		{true, "file:///a/b"}, {true, "file:///C:/a%5Cb"}, {true, "file:///CC:/a"},
		{true, "file://server"}, {true, "file://server/"}, // a host with no share is no Windows path
		{true, "file://server/share:x"}, // nor is a stream on one
	} {
		if got, err := fromURL(c.u, c.win); !errors.Is(err, ErrNotFileURL) {
			t.Errorf("fromURL(%q, win=%v) = %q, %v; want ErrNotFileURL", c.u, c.win, got, err)
		}
	}
	for _, p := range []struct {
		win bool
		s   string
	}{{false, "a/b"}, {true, `C:a`}, {true, `\a`}} {
		if _, err := toURL(p.s, p.win); !errors.Is(err, ErrNotAbsolute) {
			t.Errorf("toURL(%q) of a relative path: %v", p.s, err)
		}
	}
}

// On the OS this runs on: a logical path below a real folder is a real file, and the OS path,
// its URL and its place below the root all come back to each other.
func TestARealPathRoundTrips(t *testing.T) {
	root := OS(t.TempDir())
	if !IsAbs(root) {
		t.Fatalf("the temporary folder %q is not absolute", root)
	}
	if err := os.MkdirAll(string(Join(root, "sub")), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := ParseLogical("sub/a b.md")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Under(root, l)
	if err != nil {
		t.Fatal(err)
	}
	if p != OS(filepath.Join(string(root), "sub", "a b.md")) {
		t.Errorf("Under = %q", p)
	}
	if err := os.WriteFile(string(p), []byte("x"), 0o600); err != nil {
		t.Fatalf("the OS refused %q: %v", p, err)
	}
	if !Within(root, p) {
		t.Errorf("Within(%q, %q) is false", root, p)
	}
	if back, err := Rel(root, p); err != nil || back != l {
		t.Errorf("Rel = %q, %v; want %q", back, err, l)
	}
	u, err := URL(p)
	if err != nil {
		t.Fatal(err)
	}
	if back, err := FromURL(u); err != nil || !Within(p, back) || !Within(back, p) {
		t.Errorf("FromURL(%q) = %q, %v; want %q", u, back, err, p)
	}
	home, err := Expand("~"+Sep()+"x", string(root))
	if err != nil || home != Join(root, "x") {
		t.Errorf("Expand = %q, %v", home, err)
	}
	if len(Sep()) != 1 || Sep()[0] != os.PathSeparator {
		t.Errorf("Sep() = %q", Sep())
	}
}
