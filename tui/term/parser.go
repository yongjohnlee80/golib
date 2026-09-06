package term

import "unicode/utf8"

// This file implements the DEC ANSI parser: a byte-at-a-time,
// incremental implementation of Paul Flo Williams' state machine
// (https://vt100.net/emu/dec_ansi_parser) — ground, escape,
// escape-intermediate, the four CSI states, osc-string, the five DCS states,
// and sos-pm-apc-string — with byte-class transitions, ESC-from-anywhere
// restart, and CAN/SUB abort. It is a pure state machine with no I/O:
// feed(b, emit) consumes one byte and emits zero or more actions, so
// sequences split across arbitrary read boundaries decode identically to
// contiguous input.
//
// Extensions over the 1990s model, per modern practice:
//
//   - ':' is accepted as a sub-parameter separator inside CSI/DCS params
//     (kitty keys, SGR colon-form truecolor in DECRQSS replies).
//   - Ground-state bytes >= 0x80 are assembled as UTF-8 and emitted as
//     rune prints; single-byte C1 controls are NOT interpreted (the input
//     stream is UTF-8, where 0x80–0x9F are continuation bytes).
//   - Escape-state bytes >= 0x80 begin a UTF-8 rune with the alt flag set
//     (meta-sends-escape terminals prefixing a multibyte character).
//   - DEL (0x7F) in ground is emitted as an execute action instead of being
//     ignored: it is the Backspace key on modern terminals and must reach
//     the key decoder.
//   - CAN/SUB abort the in-flight sequence AND emit an execute action:
//     0x18/0x1A are Ctrl+X / Ctrl+Z on an input stream.

// Parser limits: parameter storage allows 32 params x 4
// sub-params, saturating — excess is ignored but the sequence is still
// consumed. String payloads (OSC/DCS) are capped to bound memory.
const (
	maxParams     = 32
	maxSubparams  = 4
	maxParamValue = 65535
	maxStringData = 4096
)

type actionKind uint8

const (
	actPrint   actionKind = iota // r (alt = ESC-prefixed rune)
	actExecute                   // b: C0 control byte (plus DEL, see above)
	actEsc                       // inter, final
	actCSI                       // priv, params, inter, final
	actOSC                       // data
	actDCS                       // priv, params, inter, final, data
)

// action is one parser output. The data slice aliases parser-owned storage
// and is valid only for the duration of the emit call; consumers that retain
// it must copy.
type action struct {
	kind   actionKind
	r      rune
	alt    bool
	b      byte
	priv   byte
	final  byte
	inter  string
	params []csiParam
	data   []byte
}

// csiParam is one CSI/DCS parameter with its ':'-separated sub-parameters.
// A part of -1 marks an empty (defaulted) position.
type csiParam struct{ parts []int }

// param returns parameter i's primary value, or def when absent/empty.
func (a *action) param(i, def int) int {
	if i >= len(a.params) || len(a.params[i].parts) == 0 || a.params[i].parts[0] < 0 {
		return def
	}
	return a.params[i].parts[0]
}

// sub returns sub-parameter j of parameter i, or def when absent/empty.
func (a *action) sub(i, j, def int) int {
	if i >= len(a.params) || j >= len(a.params[i].parts) || a.params[i].parts[j] < 0 {
		return def
	}
	return a.params[i].parts[j]
}

type pState uint8

const (
	sGround pState = iota
	sEscape
	sEscInter
	sCSIEntry
	sCSIParam
	sCSIInter
	sCSIIgnore
	sOSC
	sDCSEntry
	sDCSParam
	sDCSInter
	sDCSPass
	sDCSIgnore
	sSOSPMAPC
	sUTF8
)

type parser struct {
	state pState

	inter []byte
	priv  byte

	params   [maxParams][maxSubparams]int
	hasVal   [maxParams][maxSubparams]bool
	subN     [maxParams]uint8
	iParam   int
	iSub     int
	sawParam bool
	pDiscard bool // param count saturated: consume, ignore
	sDiscard bool // sub-param count saturated for the current param

	final byte // DCS hook final byte
	data  []byte

	u8     [utf8.UTFMax]byte
	u8n    int
	u8need int
	u8alt  bool
}

// reset returns the parser to ground, dropping any in-flight sequence.
func (p *parser) reset() {
	p.state = sGround
	p.u8n = 0
}

// feed consumes one byte, emitting zero or more actions.
func (p *parser) feed(b byte, emit func(*action)) {
	// "Anywhere" transitions (vt100.net): CAN/SUB abort, ESC restarts.
	switch b {
	case 0x18, 0x1A:
		// CAN/SUB abort any in-flight sequence without dispatch — and are
		// still keys on an input stream (Ctrl+X / Ctrl+Z).
		p.state = sGround
		p.u8n = 0
		emit(&action{kind: actExecute, b: b})
		return
	case 0x1B:
		switch p.state {
		case sOSC:
			// xterm practice: OSC is dispatched when the ESC of its ESC \
			// terminator arrives (the trailing '\' dispatches as a harmless
			// actEsc the decoder ignores).
			p.dispatchOSC(emit)
		case sDCSPass:
			p.dispatchDCS(emit)
		case sEscape:
			// ESC ESC: the pending ESC was a real Escape key; deliver it.
			emit(&action{kind: actExecute, b: 0x1B})
		}
		p.enterEscape()
		return
	}

	switch p.state {
	case sGround:
		p.ground(b, emit)
	case sUTF8:
		p.utf8Byte(b, emit)
	case sEscape:
		p.escape(b, emit)
	case sEscInter:
		p.escInter(b, emit)
	case sCSIEntry:
		p.csiEntry(b, emit)
	case sCSIParam:
		p.csiParam(b, emit)
	case sCSIInter:
		p.csiInter(b, emit)
	case sCSIIgnore:
		p.csiIgnore(b, emit)
	case sOSC:
		p.osc(b, emit)
	case sDCSEntry:
		p.dcsEntry(b)
	case sDCSParam:
		p.dcsParam(b)
	case sDCSInter:
		p.dcsInter(b)
	case sDCSPass:
		p.dcsPass(b)
	case sDCSIgnore, sSOSPMAPC:
		// Consumed without effect until ESC / CAN / SUB (handled above).
	}
}

func (p *parser) ground(b byte, emit func(*action)) {
	switch {
	case b <= 0x1F:
		emit(&action{kind: actExecute, b: b})
	case b <= 0x7E:
		emit(&action{kind: actPrint, r: rune(b)})
	case b == 0x7F:
		// Deviation from vt100.net (which ignores DEL): Backspace key.
		emit(&action{kind: actExecute, b: b})
	default:
		p.startUTF8(b, false)
	}
}

func (p *parser) startUTF8(b byte, alt bool) {
	var need int
	switch {
	case b&0xE0 == 0xC0:
		need = 2
	case b&0xF0 == 0xE0:
		need = 3
	case b&0xF8 == 0xF0:
		need = 4
	default:
		return // stray continuation / invalid lead byte: dropped
	}
	p.u8[0] = b
	p.u8n = 1
	p.u8need = need
	p.u8alt = alt
	p.state = sUTF8
}

func (p *parser) utf8Byte(b byte, emit func(*action)) {
	if b&0xC0 != 0x80 {
		// Invalid continuation: drop the partial rune, reprocess b in ground.
		p.state = sGround
		p.u8n = 0
		p.feed(b, emit)
		return
	}
	p.u8[p.u8n] = b
	p.u8n++
	if p.u8n < p.u8need {
		return
	}
	r, _ := utf8.DecodeRune(p.u8[:p.u8n])
	p.state = sGround
	p.u8n = 0
	emit(&action{kind: actPrint, r: r, alt: p.u8alt})
}

func (p *parser) enterEscape() {
	p.state = sEscape
	p.inter = p.inter[:0]
	p.u8n = 0
}

func (p *parser) escape(b byte, emit func(*action)) {
	switch {
	case b <= 0x1F:
		emit(&action{kind: actExecute, b: b})
	case b <= 0x2F:
		p.inter = append(p.inter, b)
		p.state = sEscInter
	case b == 'P':
		p.enterDCS()
	case b == 'X', b == '^', b == '_':
		p.state = sSOSPMAPC
	case b == '[':
		p.enterCSI()
	case b == ']':
		p.enterOSC()
	case b <= 0x7E:
		p.state = sGround
		emit(&action{kind: actEsc, final: b, inter: string(p.inter)})
	case b == 0x7F:
		// ignore
	default:
		// Extension: meta-sends-escape with a multibyte char (Alt+<rune>).
		p.startUTF8(b, true)
	}
}

func (p *parser) escInter(b byte, emit func(*action)) {
	switch {
	case b <= 0x1F:
		emit(&action{kind: actExecute, b: b})
	case b <= 0x2F:
		p.inter = append(p.inter, b)
	case b <= 0x7E:
		p.state = sGround
		emit(&action{kind: actEsc, final: b, inter: string(p.inter)})
	default:
		// 0x7F and >= 0x80: ignore
	}
}

func (p *parser) clearSeq() {
	p.inter = p.inter[:0]
	p.priv = 0
	p.params = [maxParams][maxSubparams]int{}
	p.hasVal = [maxParams][maxSubparams]bool{}
	p.subN = [maxParams]uint8{}
	p.iParam = 0
	p.iSub = 0
	p.sawParam = false
	p.pDiscard = false
	p.sDiscard = false
}

func (p *parser) enterCSI() {
	p.clearSeq()
	p.state = sCSIEntry
}

func (p *parser) csiEntry(b byte, emit func(*action)) {
	switch {
	case b <= 0x1F:
		emit(&action{kind: actExecute, b: b})
	case b <= 0x2F:
		p.inter = append(p.inter, b)
		p.state = sCSIInter
	case b >= '0' && b <= '9':
		p.paramDigit(b)
		p.state = sCSIParam
	case b == ':':
		p.paramSub()
		p.state = sCSIParam
	case b == ';':
		p.paramSep()
		p.state = sCSIParam
	case b >= 0x3C && b <= 0x3F:
		p.priv = b
		p.state = sCSIParam
	case b <= 0x7E:
		p.dispatchCSI(b, emit)
	default:
		// 0x7F and >= 0x80: ignore
	}
}

func (p *parser) csiParam(b byte, emit func(*action)) {
	switch {
	case b <= 0x1F:
		emit(&action{kind: actExecute, b: b})
	case b <= 0x2F:
		p.inter = append(p.inter, b)
		p.state = sCSIInter
	case b >= '0' && b <= '9':
		p.paramDigit(b)
	case b == ':':
		p.paramSub()
	case b == ';':
		p.paramSep()
	case b >= 0x3C && b <= 0x3F:
		p.state = sCSIIgnore // private marker mid-params: malformed
	case b <= 0x7E:
		p.dispatchCSI(b, emit)
	default:
		// ignore
	}
}

func (p *parser) csiInter(b byte, emit func(*action)) {
	switch {
	case b <= 0x1F:
		emit(&action{kind: actExecute, b: b})
	case b <= 0x2F:
		p.inter = append(p.inter, b)
	case b <= 0x3F:
		p.state = sCSIIgnore // params after intermediates: malformed
	case b <= 0x7E:
		p.dispatchCSI(b, emit)
	default:
		// ignore
	}
}

func (p *parser) csiIgnore(b byte, emit func(*action)) {
	switch {
	case b <= 0x1F:
		emit(&action{kind: actExecute, b: b})
	case b >= 0x40 && b <= 0x7E:
		p.state = sGround // consumed, no dispatch
	default:
		// ignore
	}
}

func (p *parser) paramDigit(b byte) {
	p.sawParam = true
	if p.pDiscard || p.sDiscard {
		return
	}
	v := p.params[p.iParam][p.iSub]*10 + int(b-'0')
	if v > maxParamValue {
		v = maxParamValue
	}
	p.params[p.iParam][p.iSub] = v
	p.hasVal[p.iParam][p.iSub] = true
}

func (p *parser) paramSub() {
	p.sawParam = true
	if p.pDiscard || p.sDiscard {
		return
	}
	if p.iSub+1 >= maxSubparams {
		p.sDiscard = true
		return
	}
	p.iSub++
}

func (p *parser) paramSep() {
	p.sawParam = true
	if p.pDiscard {
		return
	}
	p.sDiscard = false
	p.subN[p.iParam] = uint8(p.iSub + 1)
	if p.iParam+1 >= maxParams {
		p.pDiscard = true
		return
	}
	p.iParam++
	p.iSub = 0
}

func (p *parser) buildParams() []csiParam {
	if !p.sawParam {
		return nil
	}
	if !p.pDiscard {
		p.subN[p.iParam] = uint8(p.iSub + 1)
	}
	n := p.iParam + 1
	out := make([]csiParam, n)
	for i := range n {
		m := int(p.subN[i])
		if m == 0 {
			m = 1
		}
		parts := make([]int, m)
		for j := range m {
			if p.hasVal[i][j] {
				parts[j] = p.params[i][j]
			} else {
				parts[j] = -1
			}
		}
		out[i] = csiParam{parts: parts}
	}
	return out
}

func (p *parser) dispatchCSI(final byte, emit func(*action)) {
	p.state = sGround
	emit(&action{
		kind:   actCSI,
		priv:   p.priv,
		inter:  string(p.inter),
		params: p.buildParams(),
		final:  final,
	})
}

func (p *parser) enterOSC() {
	p.data = p.data[:0]
	p.state = sOSC
}

func (p *parser) osc(b byte, emit func(*action)) {
	switch {
	case b == 0x07: // BEL terminator (xterm extension)
		p.dispatchOSC(emit)
		p.state = sGround
	case b <= 0x1F:
		// ignore other C0 inside OSC
	default:
		if len(p.data) < maxStringData {
			p.data = append(p.data, b)
		}
	}
}

func (p *parser) dispatchOSC(emit func(*action)) {
	emit(&action{kind: actOSC, data: p.data})
}

func (p *parser) enterDCS() {
	p.clearSeq()
	p.data = p.data[:0]
	p.final = 0
	p.state = sDCSEntry
}

func (p *parser) dcsEntry(b byte) {
	switch {
	case b <= 0x1F:
		// ignore
	case b <= 0x2F:
		p.inter = append(p.inter, b)
		p.state = sDCSInter
	case b >= '0' && b <= '9':
		p.paramDigit(b)
		p.state = sDCSParam
	case b == ':':
		p.paramSub()
		p.state = sDCSParam
	case b == ';':
		p.paramSep()
		p.state = sDCSParam
	case b >= 0x3C && b <= 0x3F:
		p.priv = b
		p.state = sDCSParam
	case b <= 0x7E:
		p.dcsHook(b)
	default:
		// ignore
	}
}

func (p *parser) dcsParam(b byte) {
	switch {
	case b <= 0x1F:
		// ignore
	case b <= 0x2F:
		p.inter = append(p.inter, b)
		p.state = sDCSInter
	case b >= '0' && b <= '9':
		p.paramDigit(b)
	case b == ':':
		p.paramSub()
	case b == ';':
		p.paramSep()
	case b >= 0x3C && b <= 0x3F:
		p.state = sDCSIgnore
	case b <= 0x7E:
		p.dcsHook(b)
	default:
		// ignore
	}
}

func (p *parser) dcsInter(b byte) {
	switch {
	case b <= 0x1F:
		// ignore
	case b <= 0x2F:
		p.inter = append(p.inter, b)
	case b <= 0x3F:
		p.state = sDCSIgnore
	case b <= 0x7E:
		p.dcsHook(b)
	default:
		// ignore
	}
}

func (p *parser) dcsHook(final byte) {
	p.final = final
	p.data = p.data[:0]
	p.state = sDCSPass
}

func (p *parser) dcsPass(b byte) {
	if b == 0x7F {
		return
	}
	if len(p.data) < maxStringData {
		p.data = append(p.data, b)
	}
}

func (p *parser) dispatchDCS(emit func(*action)) {
	emit(&action{
		kind:   actDCS,
		priv:   p.priv,
		inter:  string(p.inter),
		params: p.buildParams(),
		final:  p.final,
		data:   p.data,
	})
}
