package gui

import (
	"os"
	"strings"
	"testing"
)

// verifiedGio is the Gio release the backend's input-method handling was verified against. It
// relies on Gio behaviour no API states:
//   - text input opens only for key.SoftKeyboardCmd, and closes on every focus change;
//   - a composition is cancelled when the snippet or selection the app reports differs from
//     the input method's, so key.SnippetEvent must be answered and key.SelectionEvent mirrored;
//   - committed text and keys reach the app in the order the input method sent them.
//
// A new Gio can change any of these silently. Before moving this constant, read Gio's diff of
// io/input/key.go and app/{ime,window,os_wayland,os_macos}.go, then type Korean with a Hangul
// input method on Wayland and on macOS: every composing stage stays visible, a syllable is
// never hidden under the next, and Enter after Hangul lands after the last syllable.
const verifiedGio = "v0.10.3"

// The module requires exactly the Gio release the input-method handling was verified against.
func TestGioIsTheVersionTheIMEContractWasVerifiedAgainst(t *testing.T) {
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(mod), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "gioui.org" || len(f) >= 3 && f[0] == "require" && f[1] == "gioui.org" {
			v := f[1]
			if f[0] == "require" {
				v = f[2]
			}
			if v != verifiedGio {
				t.Fatalf("go.mod requires gioui.org %s, but the input-method handling was verified against %s. "+
					"Re-verify it as verifiedGio's comment says, then move verifiedGio.", v, verifiedGio)
			}
			return
		}
	}
	t.Fatal("go.mod does not require gioui.org")
}
