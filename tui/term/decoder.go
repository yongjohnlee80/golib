package term

import (
	"bytes"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/internal/vtparse"
	"github.com/yongjohnlee80/golib/tui/internal/xterm"
)

// This file maps parser actions onto the core tui event set: legacy CSI/SS3
// key decoding with the xterm modifier encoding, kitty
// CSI u keys, SGR mouse, bracketed-paste framing, mode-2048 in-band resize
// reports, and terminal focus. Probe replies (DECRPM, DA1, kitty query,
// OSC 10/11 colors, XTGETTCAP) are classified here and routed to the probe
// callback — they are never user events.

// probeKind classifies one capability-probe reply.
type probeKind uint8

const (
	prDECRPM   probeKind = iota // DECRPM: CSI ? mode ; value $ y
	prDA1                       // the DA1 fence: CSI ? ... c
	prKitty                     // kitty query reply: CSI ? flags u
	prOSCColor                  // OSC 10/11 default color report
	prTermcap                   // XTGETTCAP reply (RGB / Smulx)
	prGraphics                  // kitty graphics reply to the probe's query (i=31): OK or an error
)

type probeReply struct {
	kind        probeKind
	mode, value int // prDECRPM
	osc         int // prOSCColor: 10 or 11
	color       tui.ProbedColor
	rgb, smulx  bool // prTermcap
	graphicsOK  bool // prGraphics
}

// decoder turns a byte stream into tui events. It is synchronous and
// goroutine-free: feedBytes drives the parser and invokes the callbacks
// inline, which keeps the whole decode path table-testable.
type decoder struct {
	p     vtparse.Parser
	emit  func(tui.Event)
	probe func(probeReply)

	// onFocusIn, when set, fires after a terminal focus-in event — the
	// backend uses it for the opportunistic size re-check.
	onFocusIn func()

	ss3 bool // ESC O seen; next byte is the SS3 final

	// optionFold turns the characters Option+letter types on a US Mac keyboard back into
	// Alt+letter (WithOptionFold).
	optionFold bool

	// Bracketed-paste capture. While pasting, bytes bypass
	// the parser entirely and are captured literally until the exact
	// ESC [ 2 0 1 ~ terminator — a CSI 200~ opener inside an unterminated
	// paste therefore lands in the text as literal bytes.
	pasting    bool
	pasteBuf   []byte
	pasteMatch int // bytes of pasteEnd matched so far
}

var pasteEnd = []byte(xterm.PasteEnd)

// feedBytes decodes one read chunk. Sequences may split across chunks at any
// byte boundary.
func (d *decoder) feedBytes(p []byte) {
	for i := 0; i < len(p); i++ {
		if d.pasting {
			d.pasteByte(p[i])
			continue
		}
		d.p.Feed(p[i], d.handle)
	}
}

// finish flushes decoder state at stream end (Stop or reader failure): an
// unterminated paste is delivered rather than dropped, and a
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
// legacy disambiguation timeout should resolve.
func (d *decoder) awaitingEsc() bool {
	return d.ss3 || d.p.InEscape()
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
	if d.p.InEscape() {
		d.p.Reset()
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

func (d *decoder) handle(a *vtparse.Action) {
	switch a.Kind {
	case vtparse.Print:
		if d.ss3 {
			d.ss3 = false
			if a.Rune <= 0x7F {
				if ev, ok := ss3Key(byte(a.Rune)); ok {
					d.emit(ev)
				}
			}
			return
		}
		mods := tui.Mods(0)
		text := string(a.Rune)
		if a.Alt {
			mods = tui.ModAlt
			text = ""
		}
		d.key(tui.KeyEvent{Code: a.Rune, Mods: mods, Text: text})
	case vtparse.Execute:
		d.ss3 = false
		if ev, ok := ctrlKey(a.Byte); ok {
			d.emit(ev)
		}
	case vtparse.Esc:
		d.escDispatch(a)
	case vtparse.CSI:
		d.csiDispatch(a)
	case vtparse.OSC:
		d.oscDispatch(a)
	case vtparse.DCS:
		d.dcsDispatch(a)
	case vtparse.APC:
		d.apcDispatch(a)
	}
}

// graphicsProbeID is the image id of the probe's kitty graphics query.
const graphicsProbeID = "31"

// apcDispatch reads a kitty graphics reply to the probe's query: APC G i=31 ; OK ST, or an error
// message in place of OK. Any other APC is not a key and is dropped.
func (d *decoder) apcDispatch(a *vtparse.Action) {
	if d.probe == nil || len(a.Data) == 0 || a.Data[0] != 'G' {
		return
	}
	ctrl, msg, ok := strings.Cut(string(a.Data[1:]), ";")
	if !ok {
		return
	}
	for kv := range strings.SplitSeq(ctrl, ",") {
		if kv == "i="+graphicsProbeID {
			d.probe(probeReply{kind: prGraphics, graphicsOK: msg == "OK"})
			return
		}
	}
}

// ctrlKey decodes a C0 control byte (plus DEL) into its legacy key meaning.
// The known-unfixable legacy collisions — Ctrl+I = Tab, Ctrl+M = Enter,
// Ctrl+[ = ESC, no key release — are inherent to this encoding and are
// exactly what kitty mode removes.
func ctrlKey(b byte) (tui.KeyEvent, bool) {
	if k, ok := xterm.C0Key(b); ok {
		return tui.KeyEvent{Code: k}, true
	}
	if k, ok := xterm.CtrlKey(b); ok {
		return tui.KeyEvent{Code: k, Mods: tui.ModCtrl}, true
	}
	return tui.KeyEvent{}, false
}

func (d *decoder) escDispatch(a *vtparse.Action) {
	if a.Inter != "" {
		return // charset designations etc. — not input
	}
	switch a.Final {
	case 'O':
		d.ss3 = true // SS3: the next byte is the key final
	case '\\':
		// ST tail after an OSC/DCS dispatch — not input.
	default:
		// Meta-sends-escape: ESC <char> is Alt+<char>.
		d.emit(tui.KeyEvent{Code: rune(a.Final), Mods: tui.ModAlt})
	}
}

func ss3Key(final byte) (tui.KeyEvent, bool) {
	if final == xterm.SS3KeypadEnter {
		return tui.KeyEvent{Code: tui.KeyEnter}, true
	}
	k, ok := xterm.LetterKey(final)
	return tui.KeyEvent{Code: k}, ok
}

// modsEvent decodes the xterm/kitty modifier parameter at index idx —
// m = 1 + bitmask(shift=1, alt=2, ctrl=4, super=8, ...), identical bit
// positions to tui.Mods — plus kitty's event-type sub-parameter
// (1 press, 2 repeat, 3 release).
func modsEvent(a *vtparse.Action, idx int) (tui.Mods, tui.KeyKind) {
	mods := xterm.ParamMods(a.Param(idx, 1))
	kind := tui.KeyPress
	switch a.Sub(idx, 1, 1) {
	case 2:
		kind = tui.KeyRepeat
	case 3:
		kind = tui.KeyRelease
	}
	return mods, kind
}

func (d *decoder) csiDispatch(a *vtparse.Action) {
	switch a.Priv {
	case '?':
		d.privReply(a)
		return
	case '<':
		if a.Final == 'M' || a.Final == 'm' {
			d.mouse(a)
		}
		return
	case 0:
		// fall through to the public dispatch below
	default:
		return // '>' / '=' prefixed — not input we decode
	}
	if a.Inter != "" {
		return
	}
	switch a.Final {
	case 'A', 'B', 'C', 'D', 'H', 'F':
		mods, kind := modsEvent(a, 1)
		k, _ := xterm.LetterKey(a.Final)
		d.emit(tui.KeyEvent{Kind: kind, Code: k, Mods: mods})
	case 'P', 'Q', 'R', 'S':
		// Modified F1–F4: CSI 1 ; m {P,Q,R,S}. A bare final (no params)
		// is not key input (CSI R is also the CPR report shape).
		if a.Param(0, 0) == 1 && len(a.Params) >= 2 {
			mods, kind := modsEvent(a, 1)
			k, _ := xterm.LetterKey(a.Final)
			d.emit(tui.KeyEvent{Kind: kind, Code: k, Mods: mods})
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
		if a.Param(0, 0) == 48 {
			rows, cols := a.Param(1, 0), a.Param(2, 0)
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
		// Legacy X10 mouse encoding — never enabled (SGR is mandatory); ignore
		// rather than misparse the 3 raw bytes.
	}
}

func (d *decoder) tildeKey(a *vtparse.Action) {
	code := a.Param(0, 0)
	switch code {
	case xterm.PasteStartCode: // bracketed paste opener: capture until ESC [ 2 0 1 ~
		d.pasting = true
		d.pasteBuf = d.pasteBuf[:0]
		d.pasteMatch = 0
		return
	case xterm.PasteEndCode: // stray terminator outside a paste: drop
		return
	}
	key, ok := xterm.TildeKey(code)
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
func (d *decoder) kittyKey(a *vtparse.Action) {
	code := a.Param(0, -1)
	if code < 0 {
		return
	}
	shifted := a.Sub(0, 1, 0)
	base := a.Sub(0, 2, 0)
	mods, kind := modsEvent(a, 1)
	var text strings.Builder
	if len(a.Params) >= 3 {
		for j := range a.Params[2].Parts {
			if v := a.Sub(2, j, 0); v > 0 {
				text.WriteRune(rune(v))
			}
		}
	}
	d.key(tui.KeyEvent{
		Kind:    kind,
		Code:    rune(code),
		Base:    rune(base),
		Shifted: rune(shifted),
		Mods:    mods,
		Text:    text.String(),
	})
}

// key emits a key that may carry typed text, folding an Option-composed character when asked.
func (d *decoder) key(ev tui.KeyEvent) {
	if d.optionFold {
		ev = foldOption(ev)
	}
	d.emit(ev)
}

// mouse decodes an SGR mouse report: CSI < b ; x ; y M/m with button bits per
// xterm — 0–2 button, +4 shift, +8 meta, +16 ctrl, +32 motion, +64 wheel.
// Coordinates arrive 1-based and are emitted 0-based.
func (d *decoder) mouse(a *vtparse.Action) {
	b0 := a.Param(0, 0)
	x := a.Param(1, 1) - 1
	y := a.Param(2, 1) - 1
	mods := xterm.MouseMods(b0)
	switch {
	case b0&xterm.MouseWheelBit != 0:
		if a.Final != 'M' {
			return // wheel has no release
		}
		d.emit(tui.MouseEvent{
			Kind:   tui.MouseWheel,
			Button: xterm.WheelOf(b0),
			X:      x, Y: y, Mods: mods,
		})
	case b0&xterm.MouseMotionBit != 0:
		d.emit(tui.MouseEvent{
			Kind:   tui.MouseMotion,
			Button: mouseButton(b0),
			X:      x, Y: y, Mods: mods,
		})
	default:
		kind := tui.MousePress
		if a.Final == 'm' {
			kind = tui.MouseRelease
		}
		d.emit(tui.MouseEvent{
			Kind:   kind,
			Button: mouseButton(b0),
			X:      x, Y: y, Mods: mods,
		})
	}
}

// mouseButton is the button a report's low two bits name; none for 3.
func mouseButton(b int) tui.MouseButton {
	btn, _ := xterm.MouseButtonOf(b & 3)
	return btn
}

// --- probe replies ---

func (d *decoder) privReply(a *vtparse.Action) {
	if d.probe == nil {
		return
	}
	switch {
	case a.Final == 'c' && a.Inter == "":
		d.probe(probeReply{kind: prDA1})
	case a.Final == 'u' && a.Inter == "":
		d.probe(probeReply{kind: prKitty})
	case a.Final == 'y' && a.Inter == "$":
		mode := a.Param(0, -1)
		if mode >= 0 {
			d.probe(probeReply{kind: prDECRPM, mode: mode, value: a.Param(1, 0)})
		}
	}
}

func (d *decoder) oscDispatch(a *vtparse.Action) {
	if d.probe == nil {
		return
	}
	i := bytes.IndexByte(a.Data, ';')
	if i < 0 {
		return
	}
	ps, err := strconv.Atoi(string(a.Data[:i]))
	if err != nil || (ps != 10 && ps != 11) {
		return
	}
	c, ok := parseOSCColor(string(a.Data[i+1:]))
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

func (d *decoder) dcsDispatch(a *vtparse.Action) {
	if d.probe == nil {
		return
	}
	// XTGETTCAP reply: DCS 1 + r key=value [; key=value] ST (keys hex-coded);
	// DCS 0 + r ST reports failure.
	if a.Final != 'r' || a.Inter != "+" || a.Param(0, 0) != 1 {
		return
	}
	var r probeReply
	r.kind = prTermcap
	for kv := range strings.SplitSeq(string(a.Data), ";") {
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
