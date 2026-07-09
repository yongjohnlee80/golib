package term

import (
	"bytes"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
)

// This file maps parser actions onto the core tui event set (ADR-0002 §2.5,
// §2.7): legacy CSI/SS3 key decoding with the xterm modifier encoding, kitty
// CSI u keys, SGR mouse, bracketed-paste framing, mode-2048 in-band resize
// reports, and terminal focus. Probe replies (DECRPM, DA1, kitty query,
// OSC 10/11 colors, XTGETTCAP) are classified here and routed to the probe
// callback — they are never user events (ADR-0002 §2.6).

// probeKind classifies one capability-probe reply (ADR-0002 §2.6).
type probeKind uint8

const (
	prDECRPM   probeKind = iota // DECRPM: CSI ? mode ; value $ y
	prDA1                       // the DA1 fence: CSI ? ... c
	prKitty                     // kitty query reply: CSI ? flags u
	prOSCColor                  // OSC 10/11 default color report
	prTermcap                   // XTGETTCAP reply (RGB / Smulx)
)

type probeReply struct {
	kind        probeKind
	mode, value int // prDECRPM
	osc         int // prOSCColor: 10 or 11
	color       tui.ProbedColor
	rgb, smulx  bool // prTermcap
}

// decoder turns a byte stream into tui events. It is synchronous and
// goroutine-free: feedBytes drives the parser and invokes the callbacks
// inline, which keeps the whole decode path table-testable (ADR-0002 §5.2).
type decoder struct {
	p     parser
	emit  func(tui.Event)
	probe func(probeReply)

	// onFocusIn, when set, fires after a terminal focus-in event — the
	// backend uses it for the opportunistic size re-check (ADR-0002 §2.8).
	onFocusIn func()

	ss3 bool // ESC O seen; next byte is the SS3 final

	// Bracketed-paste capture (ADR-0002 §2.7). While pasting, bytes bypass
	// the parser entirely and are captured literally until the exact
	// ESC [ 2 0 1 ~ terminator — a CSI 200~ opener inside an unterminated
	// paste therefore lands in the text as literal bytes.
	pasting    bool
	pasteBuf   []byte
	pasteMatch int // bytes of pasteEnd matched so far
}

var pasteEnd = []byte("\x1b[201~")

// feedBytes decodes one read chunk. Sequences may split across chunks at any
// byte boundary.
func (d *decoder) feedBytes(p []byte) {
	for i := 0; i < len(p); i++ {
		if d.pasting {
			d.pasteByte(p[i])
			continue
		}
		d.p.feed(p[i], d.handle)
	}
}

// finish flushes decoder state at stream end (Stop or reader failure): an
// unterminated paste is delivered rather than dropped (ADR-0002 §2.7), and a
// pending lone ESC is delivered as the Escape key.
func (d *decoder) finish() {
	if d.pasting {
		d.pasteBuf = append(d.pasteBuf, pasteEnd[:d.pasteMatch]...)
		d.pasteMatch = 0
		d.finishPaste()
	}
	if d.awaitingEsc() {
		d.resolveEsc()
	}
}

// awaitingEsc reports whether the stream ends in an ambiguous ESC that the
// legacy disambiguation timeout should resolve (ADR-0002 §2.5).
func (d *decoder) awaitingEsc() bool {
	return d.ss3 || d.p.state == sEscape
}

// resolveEsc resolves a pending lone ESC (or lone ESC O) as key input. It is
// invoked by the backend's esc-disambiguation timer — only when kitty mode is
// inactive — and by finish.
func (d *decoder) resolveEsc() {
	if d.ss3 {
		d.ss3 = false
		d.emit(tui.KeyEvent{Code: 'O', Mods: tui.ModAlt})
		return
	}
	if d.p.state == sEscape {
		d.p.reset()
		d.emit(tui.KeyEvent{Code: tui.KeyEscape})
	}
}

// --- bracketed paste ---

func (d *decoder) pasteByte(b byte) {
	if b == pasteEnd[d.pasteMatch] {
		d.pasteMatch++
		if d.pasteMatch == len(pasteEnd) {
			d.finishPaste()
		}
		return
	}
	if d.pasteMatch > 0 {
		// Partial terminator turned out to be literal text.
		d.pasteBuf = append(d.pasteBuf, pasteEnd[:d.pasteMatch]...)
		d.pasteMatch = 0
		if b == pasteEnd[0] {
			d.pasteMatch = 1
			return
		}
	}
	d.pasteBuf = append(d.pasteBuf, b)
}

func (d *decoder) finishPaste() {
	text := d.pasteBuf
	text = bytes.ReplaceAll(text, []byte("\r\n"), []byte("\n"))
	text = bytes.ReplaceAll(text, []byte("\r"), []byte("\n"))
	ev := tui.PasteEvent{Text: string(text)}
	d.pasteBuf = d.pasteBuf[:0]
	d.pasteMatch = 0
	d.pasting = false
	d.emit(ev)
}

// --- action dispatch ---

func (d *decoder) handle(a *action) {
	switch a.kind {
	case actPrint:
		if d.ss3 {
			d.ss3 = false
			if a.r <= 0x7F {
				if ev, ok := ss3Key(byte(a.r)); ok {
					d.emit(ev)
				}
			}
			return
		}
		mods := tui.Mods(0)
		text := string(a.r)
		if a.alt {
			mods = tui.ModAlt
			text = ""
		}
		d.emit(tui.KeyEvent{Code: a.r, Mods: mods, Text: text})
	case actExecute:
		d.ss3 = false
		if ev, ok := ctrlKey(a.b); ok {
			d.emit(ev)
		}
	case actEsc:
		d.escDispatch(a)
	case actCSI:
		d.csiDispatch(a)
	case actOSC:
		d.oscDispatch(a)
	case actDCS:
		d.dcsDispatch(a)
	}
}

// ctrlKey decodes a C0 control byte (plus DEL) into its legacy key meaning.
// The known-unfixable legacy collisions — Ctrl+I = Tab, Ctrl+M = Enter,
// Ctrl+[ = ESC, no key release — are inherent to this encoding (ADR-0002
// §2.5) and are exactly what kitty mode removes.
func ctrlKey(b byte) (tui.KeyEvent, bool) {
	switch b {
	case 0x0D:
		return tui.KeyEvent{Code: tui.KeyEnter}, true
	case 0x09:
		return tui.KeyEvent{Code: tui.KeyTab}, true
	case 0x7F:
		return tui.KeyEvent{Code: tui.KeyBackspace}, true
	case 0x1B:
		return tui.KeyEvent{Code: tui.KeyEscape}, true
	case 0x00:
		return tui.KeyEvent{Code: ' ', Mods: tui.ModCtrl}, true
	}
	switch {
	case b >= 0x01 && b <= 0x1A: // Ctrl+A .. Ctrl+Z (byte & 0x1F)
		return tui.KeyEvent{Code: rune('a' + b - 1), Mods: tui.ModCtrl}, true
	case b >= 0x1C && b <= 0x1F: // Ctrl+\ Ctrl+] Ctrl+^ Ctrl+_
		return tui.KeyEvent{Code: rune(b | 0x40), Mods: tui.ModCtrl}, true
	}
	return tui.KeyEvent{}, false
}

func (d *decoder) escDispatch(a *action) {
	if a.inter != "" {
		return // charset designations etc. — not input
	}
	switch a.final {
	case 'O':
		d.ss3 = true // SS3: the next byte is the key final
	case '\\':
		// ST tail after an OSC/DCS dispatch — not input.
	default:
		// Meta-sends-escape: ESC <char> is Alt+<char>.
		d.emit(tui.KeyEvent{Code: rune(a.final), Mods: tui.ModAlt})
	}
}

func ss3Key(final byte) (tui.KeyEvent, bool) {
	var code rune
	switch final {
	case 'A':
		code = tui.KeyUp
	case 'B':
		code = tui.KeyDown
	case 'C':
		code = tui.KeyRight
	case 'D':
		code = tui.KeyLeft
	case 'H':
		code = tui.KeyHome
	case 'F':
		code = tui.KeyEnd
	case 'P':
		code = tui.KeyF1
	case 'Q':
		code = tui.KeyF2
	case 'R':
		code = tui.KeyF3
	case 'S':
		code = tui.KeyF4
	case 'M':
		code = tui.KeyEnter // keypad Enter
	default:
		return tui.KeyEvent{}, false
	}
	return tui.KeyEvent{Code: code}, true
}

// modsEvent decodes the xterm/kitty modifier parameter at index idx —
// m = 1 + bitmask(shift=1, alt=2, ctrl=4, super=8, ...), identical bit
// positions to tui.Mods — plus kitty's event-type sub-parameter
// (1 press, 2 repeat, 3 release).
func modsEvent(a *action, idx int) (tui.Mods, tui.KeyKind) {
	m := a.param(idx, 1)
	if m < 1 {
		m = 1
	}
	mods := tui.Mods(m - 1)
	kind := tui.KeyPress
	switch a.sub(idx, 1, 1) {
	case 2:
		kind = tui.KeyRepeat
	case 3:
		kind = tui.KeyRelease
	}
	return mods, kind
}

func (d *decoder) csiDispatch(a *action) {
	switch a.priv {
	case '?':
		d.privReply(a)
		return
	case '<':
		if a.final == 'M' || a.final == 'm' {
			d.mouse(a)
		}
		return
	case 0:
		// fall through to the public dispatch below
	default:
		return // '>' / '=' prefixed — not input we decode
	}
	if a.inter != "" {
		return
	}
	switch a.final {
	case 'A', 'B', 'C', 'D', 'H', 'F':
		mods, kind := modsEvent(a, 1)
		d.emit(tui.KeyEvent{Kind: kind, Code: navKey(a.final), Mods: mods})
	case 'P', 'Q', 'R', 'S':
		// Modified F1–F4: CSI 1 ; m {P,Q,R,S}. A bare final (no params)
		// is not key input (CSI R is also the CPR report shape).
		if a.param(0, 0) == 1 && len(a.params) >= 2 {
			mods, kind := modsEvent(a, 1)
			d.emit(tui.KeyEvent{Kind: kind, Code: tui.KeyF1 + rune(a.final-'P'), Mods: mods})
		}
	case 'Z': // back-tab
		mods, kind := modsEvent(a, 1)
		d.emit(tui.KeyEvent{Kind: kind, Code: tui.KeyTab, Mods: mods | tui.ModShift})
	case '~':
		d.tildeKey(a)
	case 'u':
		d.kittyKey(a)
	case 't':
		// Mode 2048 in-band resize report: CSI 48 ; rows ; cols ; hpx ; wpx t
		if a.param(0, 0) == 48 {
			rows, cols := a.param(1, 0), a.param(2, 0)
			if rows > 0 && cols > 0 {
				d.emit(tui.ResizeEvent{W: cols, H: rows})
			}
		}
	case 'I':
		d.emit(tui.FocusEvent{Gained: true, Terminal: true})
		if d.onFocusIn != nil {
			d.onFocusIn()
		}
	case 'O':
		d.emit(tui.FocusEvent{Gained: false, Terminal: true})
	case 'M':
		// Legacy X10 mouse encoding — never enabled (SGR is mandatory,
		// ADR-0002 §2.6); ignore rather than misparse the 3 raw bytes.
	}
}

func navKey(final byte) rune {
	switch final {
	case 'A':
		return tui.KeyUp
	case 'B':
		return tui.KeyDown
	case 'C':
		return tui.KeyRight
	case 'D':
		return tui.KeyLeft
	case 'H':
		return tui.KeyHome
	default: // 'F'
		return tui.KeyEnd
	}
}

// tildeKeys maps the legacy CSI k ~ keycodes
// (https://invisible-island.net/xterm/ctlseqs/ctlseqs.html).
var tildeKeys = map[int]rune{
	1:  tui.KeyHome,
	2:  tui.KeyInsert,
	3:  tui.KeyDelete,
	4:  tui.KeyEnd,
	5:  tui.KeyPageUp,
	6:  tui.KeyPageDown,
	7:  tui.KeyHome,
	8:  tui.KeyEnd,
	11: tui.KeyF1,
	12: tui.KeyF2,
	13: tui.KeyF3,
	14: tui.KeyF4,
	15: tui.KeyF5,
	17: tui.KeyF6,
	18: tui.KeyF7,
	19: tui.KeyF8,
	20: tui.KeyF9,
	21: tui.KeyF10,
	23: tui.KeyF11,
	24: tui.KeyF12,
}

func (d *decoder) tildeKey(a *action) {
	code := a.param(0, 0)
	switch code {
	case 200: // bracketed paste opener: capture until ESC [ 2 0 1 ~
		d.pasting = true
		d.pasteBuf = d.pasteBuf[:0]
		d.pasteMatch = 0
		return
	case 201: // stray terminator outside a paste: drop
		return
	}
	key, ok := tildeKeys[code]
	if !ok {
		return
	}
	mods, kind := modsEvent(a, 1)
	d.emit(tui.KeyEvent{Kind: kind, Code: key, Mods: mods})
}

// kittyKey decodes CSI unicode-key-code:shifted:base ; mods:event ; text u
// (https://sw.kovidgoyal.net/kitty/keyboard-protocol/). Functional keys use
// the kitty PUA assignments, which tui/keys.go adopts verbatim — the decode
// is identity.
func (d *decoder) kittyKey(a *action) {
	code := a.param(0, -1)
	if code < 0 {
		return
	}
	shifted := a.sub(0, 1, 0)
	base := a.sub(0, 2, 0)
	mods, kind := modsEvent(a, 1)
	var text strings.Builder
	if len(a.params) >= 3 {
		for j := range a.params[2].parts {
			if v := a.sub(2, j, 0); v > 0 {
				text.WriteRune(rune(v))
			}
		}
	}
	d.emit(tui.KeyEvent{
		Kind:    kind,
		Code:    rune(code),
		Base:    rune(base),
		Shifted: rune(shifted),
		Mods:    mods,
		Text:    text.String(),
	})
}

// mouse decodes an SGR mouse report: CSI < b ; x ; y M/m with button bits per
// xterm — 0–2 button, +4 shift, +8 meta, +16 ctrl, +32 motion, +64 wheel
// (ADR-0002 §2.7). Coordinates arrive 1-based and are emitted 0-based.
func (d *decoder) mouse(a *action) {
	b0 := a.param(0, 0)
	x := a.param(1, 1) - 1
	y := a.param(2, 1) - 1
	var mods tui.Mods
	if b0&4 != 0 {
		mods |= tui.ModShift
	}
	if b0&8 != 0 {
		mods |= tui.ModAlt
	}
	if b0&16 != 0 {
		mods |= tui.ModCtrl
	}
	switch {
	case b0&64 != 0:
		if a.final != 'M' {
			return // wheel has no release
		}
		d.emit(tui.MouseEvent{
			Kind:   tui.MouseWheel,
			Button: tui.WheelUp + tui.MouseButton(b0&3),
			X:      x, Y: y, Mods: mods,
		})
	case b0&32 != 0:
		d.emit(tui.MouseEvent{
			Kind:   tui.MouseMotion,
			Button: mouseButton(b0 & 3),
			X:      x, Y: y, Mods: mods,
		})
	default:
		kind := tui.MousePress
		if a.final == 'm' {
			kind = tui.MouseRelease
		}
		d.emit(tui.MouseEvent{
			Kind:   kind,
			Button: mouseButton(b0 & 3),
			X:      x, Y: y, Mods: mods,
		})
	}
}

func mouseButton(b int) tui.MouseButton {
	switch b {
	case 0:
		return tui.MouseLeft
	case 1:
		return tui.MouseMiddle
	case 2:
		return tui.MouseRight
	default:
		return tui.MouseNone
	}
}

// --- probe replies ---

func (d *decoder) privReply(a *action) {
	if d.probe == nil {
		return
	}
	switch {
	case a.final == 'c' && a.inter == "":
		d.probe(probeReply{kind: prDA1})
	case a.final == 'u' && a.inter == "":
		d.probe(probeReply{kind: prKitty})
	case a.final == 'y' && a.inter == "$":
		mode := a.param(0, -1)
		if mode >= 0 {
			d.probe(probeReply{kind: prDECRPM, mode: mode, value: a.param(1, 0)})
		}
	}
}

func (d *decoder) oscDispatch(a *action) {
	if d.probe == nil {
		return
	}
	i := bytes.IndexByte(a.data, ';')
	if i < 0 {
		return
	}
	ps, err := strconv.Atoi(string(a.data[:i]))
	if err != nil || (ps != 10 && ps != 11) {
		return
	}
	c, ok := parseOSCColor(string(a.data[i+1:]))
	if !ok {
		return
	}
	d.probe(probeReply{kind: prOSCColor, osc: ps, color: c})
}

// parseOSCColor parses an OSC 10/11 color report: the X11 rgb:/rgba: spec
// with 1–4 hex digits per component, or a #RRGGBB literal.
func parseOSCColor(s string) (tui.ProbedColor, bool) {
	if h, ok := strings.CutPrefix(s, "#"); ok && len(h) == 6 {
		v, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return tui.ProbedColor{}, false
		}
		return tui.ProbedColor{
			R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), Known: true,
		}, true
	}
	body, ok := strings.CutPrefix(s, "rgb:")
	if !ok {
		if body, ok = strings.CutPrefix(s, "rgba:"); !ok {
			return tui.ProbedColor{}, false
		}
	}
	parts := strings.Split(body, "/")
	if len(parts) < 3 {
		return tui.ProbedColor{}, false
	}
	var comp [3]uint8
	for i := range 3 {
		p := parts[i]
		if len(p) == 0 || len(p) > 4 {
			return tui.ProbedColor{}, false
		}
		v, err := strconv.ParseUint(p, 16, 32)
		if err != nil {
			return tui.ProbedColor{}, false
		}
		maxV := uint64(1)<<(4*len(p)) - 1
		comp[i] = uint8(v * 255 / maxV)
	}
	return tui.ProbedColor{R: comp[0], G: comp[1], B: comp[2], Known: true}, true
}

func (d *decoder) dcsDispatch(a *action) {
	if d.probe == nil {
		return
	}
	// XTGETTCAP reply: DCS 1 + r key=value [; key=value] ST (keys hex-coded);
	// DCS 0 + r ST reports failure.
	if a.final != 'r' || a.inter != "+" || a.param(0, 0) != 1 {
		return
	}
	var r probeReply
	r.kind = prTermcap
	for kv := range strings.SplitSeq(string(a.data), ";") {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		decoded, err := hex.DecodeString(name)
		if err != nil {
			continue
		}
		switch string(decoded) {
		case "RGB":
			r.rgb = true
		case "Smulx":
			r.smulx = true
		}
	}
	if r.rgb || r.smulx {
		d.probe(r)
	}
}
