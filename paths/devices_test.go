package paths

import (
	"errors"
	"testing"
)

// devices are names Windows opens as a device rather than a file, in any case, with any extension
// and with trailing dots or spaces; nearDevices are names close to them that are files.
var (
	devices = []string{"CON", "con", "Con", "PRN", "AUX", "NUL", "nul.txt", "CON.tar.gz", "NUL.", "nul .md", "nul..",
		"NUL  ", "COM0", "com9", "LPT1", "lpt9.log", "COM\u00b9", "com\u00b2", "LPT\u00b3.txt", "conin$", "CONOUT$.x"}
	nearDevices = []string{"CONSOLE", "nul_", "COM10", "LPT", "COM", "conx", "anul", "nul-1", "LPT\u2074", "COM\u00b90", " nul"}
)

// TestWindowsDeviceNames: WindowsDevice names exactly Windows' devices; their near names are files.
func TestWindowsDeviceNames(t *testing.T) {
	for _, d := range devices {
		if !WindowsDevice(d) {
			t.Errorf("WindowsDevice(%q) = false, want true", d)
		}
	}
	for _, n := range nearDevices {
		if WindowsDevice(n) {
			t.Errorf("WindowsDevice(%q) = true, want false: a file", n)
		}
	}
}

// TestADeviceInAPathIsRefusedUnderWindowsRulesAlone: under Windows' rules a path holding a device
// element names no file: it is never within a root, and Rel, URL and Under refuse it (ErrRefused),
// on a drive and on a share, as its last element or a folder. Under Unix's rules the same names are
// files. Near names are files under both.
func TestADeviceInAPathIsRefusedUnderWindowsRulesAlone(t *testing.T) {
	for _, d := range devices {
		for _, p := range []string{`C:\a\` + d, `C:\a\` + d + `\x.md`, `\\srv\share\a\` + d} {
			root := `C:\a`
			if p[:2] == `\\` {
				root = `\\srv\share\a`
			}
			if within(root, p, true) {
				t.Errorf("windows: %q is within %q", p, root)
			}
			if _, err := rel(root, p, true); !errors.Is(err, ErrRefused) {
				t.Errorf("windows: rel(%q, %q) = %v, want ErrRefused", root, p, err)
			}
			if _, err := toURL(p, true); !errors.Is(err, ErrRefused) {
				t.Errorf("windows: toURL(%q) = %v, want ErrRefused", p, err)
			}
			if _, err := under(p, Logical{}, true); !errors.Is(err, ErrRefused) {
				t.Errorf("windows: under(%q) = %v, want ErrRefused", p, err)
			}
		}
		if l, err := rel("/a", "/a/"+d, false); err != nil || l.s != d {
			t.Errorf("unix: rel(/a, /a/%s) = %q, %v; want the file %q", d, l.s, err, d)
		}
		if _, err := toURL("/a/"+d, false); err != nil {
			t.Errorf("unix: toURL(/a/%s) = %v, want a file's URL", d, err)
		}
	}
	for _, n := range nearDevices {
		if !within(`C:\a`, `C:\a\`+n, true) || !within("/a", "/a/"+n, false) {
			t.Errorf("%q, a file, is not within its folder under both rules", n)
		}
		if _, err := toURL(`C:\a\`+n, true); err != nil {
			t.Errorf("windows: toURL of the file %q = %v", n, err)
		}
	}
}
