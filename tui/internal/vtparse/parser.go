package vtparse

import "unicode/utf8"

// This file implements the DEC ANSI parser: a byte-at-a-time,
// incremental implementation of Paul Flo Williams' state machine
// (https://vt100.net/emu/dec_ansi_parser) — ground, escape,
// escape-intermediate, the four CSI states, osc-string, the five DCS states,
// and sos-pm-apc-string — with byte-class transitions, ESC-from-anywhere
// restart, and CAN/SUB abort. It is a pure state machine with no I/O:
// Feed(b, emit) consumes one byte and emits zero or more actions, so
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
//
// The deviations above serve the key decoder (an input stream). A parser
// with Output set reads what a program writes to a terminal instead, and
// keeps vt100.net's behaviour where the two differ:
//
//   - DEL in ground is ignored.
//   - ESC ESC restarts the escape sequence without emitting anything.
//   - A byte >= 0x80 in the escape state abandons the sequence and is
//     dropped (there is no Alt+<rune> on an output stream).
//   - In ground, the C1 controls U+0080–U+009F (UTF-8 encoded) act as
//     their 7-bit equivalents ESC + (r - 0x40): U+009B starts a CSI,
//     U+009D an OSC, and so on. Inside an OSC, DCS or APC string, U+009C
//     (ST) ends it, as ESC \ does.
//   - A CSI or DCS parameter holds up to 8 sub-parameters rather than 4, so
//     SGR's longest colon form, 38:2:cs:r:g:b, keeps its colour.

// Parser limits: parameter storage allows 32 params x 8 sub-params,
// saturating — excess is ignored but the sequence is still consumed. Input
// keeps 4 sub-params (the key decoder's widest form needs 3, and kitty's
// associated text must not grow); output takes all 8, for SGR's
// 38:2:cs:r:g:b. String payloads (OSC/DCS) are capped to bound memory.
const (
	maxParams      = 32
	maxSubparams   = 8
	inputSubparams = 4
	maxParamValue  = 65535
	maxStringData  = 4096
)

// Kind says which fields of an Action are meaningful.
type Kind uint8

const (
	Print   Kind = iota // Rune (Alt = ESC-prefixed rune)
	Execute             // Byte: C0 control byte (plus DEL, see above)
	Esc                 // Inter, Final
	CSI                 // Priv, Params, Inter, Final
	OSC                 // Data
	DCS                 // Priv, Params, Inter, Final, Data
	APC                 // Data: an application program command's string (kitty graphics replies)
)

// Action is one parser output. The Data slice aliases parser-owned storage
// and is valid only for the duration of the emit call; consumers that retain
// it must copy.
type Action struct {
	Kind   Kind
	Rune   rune
	Alt    bool
	Byte   byte
	Priv   byte
	Final  byte
	Inter  string
	Params []Param
	Data   []byte
}

// Param is one CSI/DCS parameter with its ':'-separated sub-parameters.
// A part of -1 marks an empty (defaulted) position.
type Param struct{ Parts []int }

// Param returns parameter i's primary value, or def when absent/empty.
func (a *Action) Param(i, def int) int {
	if i >= len(a.Params) || len(a.Params[i].Parts) == 0 || a.Params[i].Parts[0] < 0 {
		return def
	}
	return a.Params[i].Parts[0]
}

// Sub returns sub-parameter j of parameter i, or def when absent/empty.
func (a *Action) Sub(i, j, def int) int {
	if i >= len(a.Params) || j >= len(a.Params[i].Parts) || a.Params[i].Parts[j] < 0 {
		return def
	}
	return a.Params[i].Parts[j]
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
	sAPC
	sUTF8
)

// Parser is the state machine. The zero value is an input-stream parser
// in the ground state.
type Parser struct {
	// Output selects output-stream behaviour (see the package comment).
	// Reset keeps it.
	Output bool

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

	// c2 holds a 0xC2 met inside a string on output, until the next byte
	// says whether it began U+009C (ST).
	c2 bool
}

// Reset returns the parser to ground, dropping any in-flight sequence.
func (p *Parser) Reset() {
	p.state = sGround
	p.u8n = 0
	p.c2 = false
}

// InEscape reports whether the last byte fed left a bare ESC pending: the
// stream so far ends in ESC, which on input may be the Escape key itself.
func (p *Parser) InEscape() bool {
	return p.state == sEscape
}

// Feed consumes one byte, emitting zero or more actions.
func (p *Parser) Feed(b byte, emit func(*Action)) {
	// "Anywhere" transitions (vt100.net): CAN/SUB abort, ESC restarts.
	switch b {
	case 0x18, 0x1A:
		// CAN/SUB abort any in-flight sequence without dispatch — and are
		// still keys on an input stream (Ctrl+X / Ctrl+Z).
		p.state = sGround
		p.u8n = 0
		p.c2 = false
		emit(&Action{Kind: Execute, Byte: b})
		return
	case 0x1B:
		if p.c2 {
			p.c2 = false
			p.stringByte(0xC2, emit) // it was data, not the start of ST
		}
		switch p.state {
		case sOSC:
			// xterm practice: OSC is dispatched when the ESC of its ESC \
			// terminator arrives (the trailing '\' dispatches as a harmless
			// Esc the decoder ignores).
			p.dispatchOSC(emit)
		case sDCSPass:
			p.dispatchDCS(emit)
		case sAPC:
			emit(&Action{Kind: APC, Data: p.data})
		case sEscape:
			// ESC ESC on input: the pending ESC was a real Escape key;
			// deliver it. On output the first ESC is simply abandoned.
			if !p.Output {
				emit(&Action{Kind: Execute, Byte: 0x1B})
			}
		}
		p.enterEscape()
		return
	}

	if p.Output && p.inString() {
		if p.c2 {
			p.c2 = false
			if b == 0x9C {
				p.endString(emit)
				return
			}
			p.stringByte(0xC2, emit)
		}
		if b == 0xC2 {
			p.c2 = true
			return
		}
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
		p.csiParamState(b, emit)
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
	case sAPC:
		if b != 0x7F && len(p.data) < maxStringData {
			p.data = append(p.data, b)
		}
	}
}

// inString reports whether the parser is collecting a string's payload.
func (p *Parser) inString() bool {
	switch p.state {
	case sOSC, sDCSPass, sAPC, sDCSIgnore, sSOSPMAPC:
		return true
	}
	return false
}

// stringByte is one payload byte of the string being collected.
func (p *Parser) stringByte(b byte, emit func(*Action)) {
	switch p.state {
	case sOSC:
		p.osc(b, emit)
	case sDCSPass:
		p.dcsPass(b)
	case sAPC:
		if len(p.data) < maxStringData {
			p.data = append(p.data, b)
		}
	}
}

// endString ends the string being collected at an ST, dispatching it.
func (p *Parser) endString(emit func(*Action)) {
	switch p.state {
	case sOSC:
		p.dispatchOSC(emit)
	case sDCSPass:
		p.dispatchDCS(emit)
	case sAPC:
		emit(&Action{Kind: APC, Data: p.data})
	}
	p.state = sGround
}

func (p *Parser) ground(b byte, emit func(*Action)) {
	switch {
	case b <= 0x1F:
		emit(&Action{Kind: Execute, Byte: b})
	case b <= 0x7E:
		emit(&Action{Kind: Print, Rune: rune(b)})
	case b == 0x7F:
		// Deviation from vt100.net (which ignores DEL): Backspace key.
		if !p.Output {
			emit(&Action{Kind: Execute, Byte: b})
		}
	default:
		p.startUTF8(b, false)
	}
}

func (p *Parser) startUTF8(b byte, alt bool) {
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

func (p *Parser) utf8Byte(b byte, emit func(*Action)) {
	if b&0xC0 != 0x80 {
		// Invalid continuation: drop the partial rune, reprocess b in ground.
		p.state = sGround
		p.u8n = 0
		p.Feed(b, emit)
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
	if p.Output && r >= 0x80 && r <= 0x9F {
		// A C1 control: the same as its 7-bit ESC form.
		p.enterEscape()
		p.escape(byte(r-0x40), emit)
		return
	}
	emit(&Action{Kind: Print, Rune: r, Alt: p.u8alt})
}

func (p *Parser) enterEscape() {
	p.state = sEscape
	p.inter = p.inter[:0]
	p.u8n = 0
}

func (p *Parser) escape(b byte, emit func(*Action)) {
	switch {
	case b <= 0x1F:
		emit(&Action{Kind: Execute, Byte: b})
	case b <= 0x2F:
		p.inter = append(p.inter, b)
		p.state = sEscInter
	case b == 'P':
		p.enterDCS()
	case b == 'X', b == '^':
		p.state = sSOSPMAPC
	case b == '_':
		p.data = p.data[:0]
		p.state = sAPC
	case b == '[':
		p.enterCSI()
	case b == ']':
		p.enterOSC()
	case b <= 0x7E:
		p.state = sGround
		emit(&Action{Kind: Esc, Final: b, Inter: string(p.inter)})
	case b == 0x7F:
		// ignore
	case p.Output:
		p.state = sGround
	default:
		// Extension: meta-sends-escape with a multibyte char (Alt+<rune>).
		p.startUTF8(b, true)
	}
}

func (p *Parser) escInter(b byte, emit func(*Action)) {
	switch {
	case b <= 0x1F:
		emit(&Action{Kind: Execute, Byte: b})
	case b <= 0x2F:
		p.inter = append(p.inter, b)
	case b <= 0x7E:
		p.state = sGround
		emit(&Action{Kind: Esc, Final: b, Inter: string(p.inter)})
	default:
		// 0x7F and >= 0x80: ignore
	}
}

func (p *Parser) clearSeq() {
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

func (p *Parser) enterCSI() {
	p.clearSeq()
	p.state = sCSIEntry
}

func (p *Parser) csiEntry(b byte, emit func(*Action)) {
	switch {
	case b <= 0x1F:
		emit(&Action{Kind: Execute, Byte: b})
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

func (p *Parser) csiParamState(b byte, emit func(*Action)) {
	switch {
	case b <= 0x1F:
		emit(&Action{Kind: Execute, Byte: b})
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

func (p *Parser) csiInter(b byte, emit func(*Action)) {
	switch {
	case b <= 0x1F:
		emit(&Action{Kind: Execute, Byte: b})
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

func (p *Parser) csiIgnore(b byte, emit func(*Action)) {
	switch {
	case b <= 0x1F:
		emit(&Action{Kind: Execute, Byte: b})
	case b >= 0x40 && b <= 0x7E:
		p.state = sGround // consumed, no dispatch
	default:
		// ignore
	}
}

func (p *Parser) paramDigit(b byte) {
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

func (p *Parser) paramSub() {
	p.sawParam = true
	if p.pDiscard || p.sDiscard {
		return
	}
	limit := inputSubparams
	if p.Output {
		limit = maxSubparams
	}
	if p.iSub+1 >= limit {
		p.sDiscard = true
		return
	}
	p.iSub++
}

func (p *Parser) paramSep() {
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

func (p *Parser) buildParams() []Param {
	if !p.sawParam {
		return nil
	}
	if !p.pDiscard {
		p.subN[p.iParam] = uint8(p.iSub + 1)
	}
	n := p.iParam + 1
	out := make([]Param, n)
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
		out[i] = Param{Parts: parts}
	}
	return out
}

func (p *Parser) dispatchCSI(final byte, emit func(*Action)) {
	p.state = sGround
	emit(&Action{
		Kind:   CSI,
		Priv:   p.priv,
		Inter:  string(p.inter),
		Params: p.buildParams(),
		Final:  final,
	})
}

func (p *Parser) enterOSC() {
	p.data = p.data[:0]
	p.state = sOSC
}

func (p *Parser) osc(b byte, emit func(*Action)) {
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

func (p *Parser) dispatchOSC(emit func(*Action)) {
	emit(&Action{Kind: OSC, Data: p.data})
}

func (p *Parser) enterDCS() {
	p.clearSeq()
	p.data = p.data[:0]
	p.final = 0
	p.state = sDCSEntry
}

func (p *Parser) dcsEntry(b byte) {
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

func (p *Parser) dcsParam(b byte) {
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

func (p *Parser) dcsInter(b byte) {
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

func (p *Parser) dcsHook(final byte) {
	p.final = final
	p.data = p.data[:0]
	p.state = sDCSPass
}

func (p *Parser) dcsPass(b byte) {
	if b == 0x7F {
		return
	}
	if len(p.data) < maxStringData {
		p.data = append(p.data, b)
	}
}

func (p *Parser) dispatchDCS(emit func(*Action)) {
	emit(&Action{
		Kind:   DCS,
		Priv:   p.priv,
		Inter:  string(p.inter),
		Params: p.buildParams(),
		Final:  p.final,
		Data:   p.data,
	})
}
