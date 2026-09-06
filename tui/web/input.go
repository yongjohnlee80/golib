package web

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/tui"
)

// KeyReport is a faithful description of one browser `keydown`, as reported by
// the client.
//
// The client reports; the SERVER decides. Putting the resolution order here
// rather than in the served JavaScript means the rules of are testable
// against the real [tui.Event] structs, and a client cannot talk the backend
// into emitting an event shape the table does not allow.
//
// The one thing the client must decide by itself is `preventDefault`, which has
// to happen synchronously in the browser and cannot wait for a round trip. Its
// tables are generated from the same Go values used here, so the two cannot
// drift apart.
type KeyReport struct {
	// Key is KeyboardEvent.key verbatim.
	Key string

	// Repeat is KeyboardEvent.repeat.
	Repeat bool

	// Modifier flags, as reported.
	Ctrl, Alt, Shift, Meta bool

	// AltGraph is getModifierState("AltGraph"). It is the ONLY automatic
	// AltGraph signal: see [decoder.decodeKey].
	AltGraph bool

	// Composing is KeyboardEvent.isComposing.
	Composing bool
}

// MouseReport is one reported pointer action, in CELL coordinates.
type MouseReport struct {
	// Kind is "down", "up", "move" or "wheel".
	Kind string

	// Button is the browser's button index for down/up (0 left, 1 middle,
	// 2 right); ignored for move.
	Button int

	// Dir is the wheel direction for Kind "wheel": "up", "down", "left",
	// "right". Wheels are quantized to discrete steps by the client, because
	// [tui.MouseEvent] has no delta field to put a magnitude in.
	Dir string

	// X and Y are cell coordinates.
	X, Y int

	// Modifier flags.
	Ctrl, Alt, Shift, Meta bool
}

// namedKeys maps KeyboardEvent.key values to tui key constants (rule 3).
//
// Order matters only in the sense that this map is consulted BEFORE the
// modified-printable rule: "Enter" with Ctrl held is Ctrl+Enter, not a
// modified printable, and a table lookup is what makes that unambiguous.
var namedKeys = map[string]rune{
	"Enter":      tui.KeyEnter,
	"Tab":        tui.KeyTab,
	"Escape":     tui.KeyEscape,
	"Backspace":  tui.KeyBackspace,
	"Delete":     tui.KeyDelete,
	"Insert":     tui.KeyInsert,
	"ArrowUp":    tui.KeyUp,
	"ArrowDown":  tui.KeyDown,
	"ArrowLeft":  tui.KeyLeft,
	"ArrowRight": tui.KeyRight,
	"Home":       tui.KeyHome,
	"End":        tui.KeyEnd,
	"PageUp":     tui.KeyPageUp,
	"PageDown":   tui.KeyPageDown,
	"F1":         tui.KeyF1,
	"F2":         tui.KeyF2,
	"F3":         tui.KeyF3,
	"F4":         tui.KeyF4,
	"F5":         tui.KeyF5,
	"F6":         tui.KeyF6,
	"F7":         tui.KeyF7,
	"F8":         tui.KeyF8,
	"F9":         tui.KeyF9,
	"F10":        tui.KeyF10,
	"F11":        tui.KeyF11,
	"F12":        tui.KeyF12,
}

// NamedKeys returns the keydown values this backend forwards. The client uses it
// to decide preventDefault, so both sides read one table.
func NamedKeys() []string {
	out := make([]string, 0, len(namedKeys))
	for k := range namedKeys {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// reservedRule is one browser-owned chord, in a form the client can evaluate.
//
// A DATA table rather than Go control flow, because the client has to make the
// same decision synchronously and the only way two implementations stay in
// agreement is for there to be one implementation. The rules are injected into
// the served page and the client walks them; nothing about them is written twice.
type reservedRule struct {
	// Key matches KeyboardEvent.key. Compared case-insensitively when Lower is
	// set, exactly otherwise.
	Key string `json:"key"`

	// Lower means compare against the lower-cased key.
	Lower bool `json:"lower,omitempty"`

	// Need names the modifier the chord requires: "" (none), "ctrl", "meta",
	// "cmdOrCtrl".
	Need string `json:"need,omitempty"`
}

// reservedRules are the chords the BROWSER keeps (rule 1).
//
// Stealing these is how a web terminal becomes hostile: a user who cannot open a
// tab, reload, or reach devtools has lost control of their own browser. The
// README lists what an App therefore cannot see.
var reservedRules = []reservedRule{
	{Key: "F5"},
	{Key: "F11"},
	{Key: "F12"},
	{Key: "Tab", Need: "ctrl"}, // Ctrl+Tab switches tabs
	{Key: "t", Lower: true, Need: "cmdOrCtrl"}, // new tab
	{Key: "n", Lower: true, Need: "cmdOrCtrl"}, // new window
	{Key: "w", Lower: true, Need: "cmdOrCtrl"}, // close tab
	{Key: "l", Lower: true, Need: "cmdOrCtrl"}, // address bar
	{Key: "r", Lower: true, Need: "cmdOrCtrl"}, // reload
	{Key: "q", Lower: true, Need: "meta"},      // Cmd+Q quits the browser
}

// ReservedRules returns the browser-owned chord table, for injection into the
// client. Copied, so a caller cannot alter what the server enforces.
func ReservedRules() []reservedRule { return slices.Clone(reservedRules) }

// matches evaluates one rule against a report.
func (r reservedRule) matches(k KeyReport) bool {
	key := k.Key
	if r.Lower {
		key = strings.ToLower(key)
	}
	if key != r.Key {
		return false
	}
	switch r.Need {
	case "":
		return true
	case "ctrl":
		return k.Ctrl
	case "meta":
		return k.Meta
	case "cmdOrCtrl":
		return k.Ctrl || k.Meta
	}
	return false
}

// reservedShortcut reports whether a chord belongs to the BROWSER and must not
// be forwarded or preventDefault'ed (rule 1).
func reservedShortcut(r KeyReport) bool {
	for _, rule := range reservedRules {
		if rule.matches(r) {
			return true
		}
	}
	return false
}

// ReservedShortcut is the exported form, for tests and callers.
func ReservedShortcut(r KeyReport) bool { return reservedShortcut(r) }

// decoder applies 's resolution order.
type decoder struct {
	// treatCtrlAltAsAltGraph enables the Ctrl+Alt heuristic on browsers that do
	// not report AltGraph. OFF by default: inferring AltGraph from Ctrl+Alt
	// would silently swallow every legitimate Ctrl+Alt chord, so it is an opt-in
	// with the trade-off stated in the README.
	treatCtrlAltAsAltGraph bool
}

// mods maps the reported modifier flags.
//
// metaKey maps to ModSuper, NOT ModMeta. tui.Mods is in kitty order, where super
// is the Cmd/Windows key and meta is the historical Meta (usually Alt). The
// obvious-looking metaKey→ModMeta mapping would silently break every Cmd chord
// on macOS.
func mods(ctrl, alt, shift, meta bool) tui.Mods {
	var m tui.Mods
	if shift {
		m |= tui.ModShift
	}
	if alt {
		m |= tui.ModAlt
	}
	if ctrl {
		m |= tui.ModCtrl
	}
	if meta {
		m |= tui.ModSuper
	}
	return m
}

// decodeKey applies the normative resolution order; the FIRST matching rule
// wins. ok=false means "emit nothing", which is a decision, not a gap.
func (d *decoder) decodeKey(r KeyReport) (tui.Event, bool) {
	// Rule 1: reserved browser shortcuts are the browser's.
	if reservedShortcut(r) {
		return nil, false
	}

	// Rule 2: composition or AltGraph is never a chord — the character arrives
	// through the text path. This rule must precede the named-key and
	// modified-printable rules, because on layouts where AltGraph reports as
	// Ctrl+Alt we would otherwise emit a COMMAND instead of the character the
	// user typed.
	if r.Composing || r.AltGraph {
		return nil, false
	}
	if d.treatCtrlAltAsAltGraph && r.Ctrl && r.Alt {
		return nil, false
	}

	m := mods(r.Ctrl, r.Alt, r.Shift, r.Meta)
	kind := tui.KeyPress
	if r.Repeat {
		kind = tui.KeyRepeat
	}

	// Rule 3: named keys.
	if code, ok := namedKeys[r.Key]; ok {
		return tui.KeyEvent{Kind: kind, Code: code, Mods: m}, true
	}

	// Rule 4 (text) is not reachable from a keydown: text arrives as a separate
	// host-value delta, so there is nothing to do here.

	// Rule 5: modified printable. Only with a command modifier — an unmodified
	// printable is text and belongs to the text path, or it would be emitted
	// twice.
	if r.Ctrl || r.Alt || r.Meta {
		if code, ok := singleScalar(r.Key); ok {
			return tui.KeyEvent{Kind: kind, Code: code, Mods: m}, true
		}
		// Dead, Unidentified, or not a single scalar: KeyEvent.Code holds ONE
		// rune, so there is nothing faithful to put in it. Dropped rather than
		// approximated; any resulting character still reaches the app through
		// the text path.
		return nil, false
	}

	// Rule 6: anything else is dropped explicitly. Never a phantom key.
	return nil, false
}

// singleScalar returns the lower-cased codepoint of a key value that is exactly
// one Unicode scalar.
//
// Lower-casing gives a component one thing to match: Ctrl+C and Ctrl+Shift+C
// both arrive as 'c', with Shift in Mods where it belongs.
//
// [unicode.ToLower] is Go's SIMPLE case mapping — one rune in, one rune out,
// always. Unicode's FULL mappings can expand (Turkish İ lower-cases to i plus a
// combining dot above), but neither strings nor unicode applies them, so there
// is no expansion case here and no guard pretending to handle one. What actually
// protects Code, which holds a single rune, is the multi-scalar rejection above.
func singleScalar(key string) (rune, bool) {
	if utf8.RuneCountInString(key) != 1 {
		return 0, false
	}
	r, size := utf8.DecodeRuneInString(key)
	if r == utf8.RuneError && size <= 1 {
		// Invalid UTF-8, not a real U+FFFD the client sent.
		return 0, false
	}
	return unicode.ToLower(r), true
}

// decodeText turns a drained capture-element value into key events.
//
// Emission is PER RUNE, matching tui/term's actPrint, which emits
// KeyEvent{Code: r, Text: string(r)} for each rune rather than one event per
// grapheme cluster. A multi-codepoint emoji therefore arrives as several events,
// exactly as it would over a terminal — a component must not be able to tell the
// two backends apart.
//
// Base and Shifted stay 0. The browser cannot supply them: KeyboardEvent.code is
// a PHYSICAL key identifier ("KeyA" is a position, not a base rune) and the DOM
// exposes no base-layout or shifted codepoint. In tui/term they are populated
// only on the kitty CSI path, and 0 is exactly the shape a non-kitty terminal
// produces.
func decodeText(s string) []tui.Event {
	if s == "" {
		return nil
	}
	out := make([]tui.Event, 0, utf8.RuneCountInString(s))
	for _, r := range s {
		if r == utf8.RuneError {
			// Invalid UTF-8 from a client is dropped rather than forwarded as a
			// replacement character the user never typed.
			continue
		}
		out = append(out, tui.KeyEvent{Kind: tui.KeyPress, Code: r, Text: string(r)})
	}
	return out
}

// decodePaste normalizes clipboard text. CR and CRLF become \n so a component
// sees one line ending regardless of the client's platform.
func decodePaste(s string) tui.Event {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return tui.PasteEvent{Text: s}
}

// decodeMouse maps a pointer report. ok=false means the report was unusable.
func decodeMouse(r MouseReport) (tui.Event, bool) {
	m := mods(r.Ctrl, r.Alt, r.Shift, r.Meta)
	switch r.Kind {
	case "down":
		btn, ok := button(r.Button)
		if !ok {
			return nil, false
		}
		return tui.MouseEvent{Kind: tui.MousePress, Button: btn, X: r.X, Y: r.Y, Mods: m}, true
	case "up":
		btn, ok := button(r.Button)
		if !ok {
			return nil, false
		}
		return tui.MouseEvent{Kind: tui.MouseRelease, Button: btn, X: r.X, Y: r.Y, Mods: m}, true
	case "move":
		return tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseNone, X: r.X, Y: r.Y, Mods: m}, true
	case "wheel":
		btn, ok := wheelButton(r.Dir)
		if !ok {
			return nil, false
		}
		return tui.MouseEvent{Kind: tui.MouseWheel, Button: btn, X: r.X, Y: r.Y, Mods: m}, true
	}
	return nil, false
}

// button maps a browser button index. An unknown index is refused rather than
// mapped to MouseLeft, which would turn a stray back/forward button into a click.
func button(i int) (tui.MouseButton, bool) {
	switch i {
	case 0:
		return tui.MouseLeft, true
	case 1:
		return tui.MouseMiddle, true
	case 2:
		return tui.MouseRight, true
	}
	return tui.MouseNone, false
}

func wheelButton(dir string) (tui.MouseButton, bool) {
	switch dir {
	case "up":
		return tui.WheelUp, true
	case "down":
		return tui.WheelDown, true
	case "left":
		return tui.WheelLeft, true
	case "right":
		return tui.WheelRight, true
	}
	return tui.MouseNone, false
}

// decodeFocus maps window focus/blur. Terminal is true: this is terminal-level
// focus, not component focus, which the App's focus manager owns.
func decodeFocus(gained bool) tui.Event {
	return tui.FocusEvent{Gained: gained, Terminal: true}
}
