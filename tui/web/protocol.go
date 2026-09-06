package web

import "github.com/yongjohnlee80/golib/tui"

// The wire protocol. JSON over one WebSocket, client→server and server→client.
//
// JSON rather than a compact binary encoding because the payload is dominated by
// cell content that compresses well, the client is a browser where JSON is free,
// and a wire format nobody can read in devtools is a debugging cost paid on
// every future problem. §5 records the binary alternative.

// Client→server message types.
const (
	msgHello  = "hello"  // first message: credentials plus measurements
	msgKey    = "key"    // a reported keydown
	msgText   = "text"   // a drained capture-element value
	msgPaste  = "paste"  // clipboard text
	msgMouse  = "mouse"  // pointer action, in cell coordinates
	msgFocus  = "focus"  // window focus/blur
	msgResize = "resize" // measured size change
	msgAck    = "ack"    // frame acknowledgement
)

// Server→client message types.
const (
	msgFrame = "frame" // one atomic screen update
	msgReady = "ready" // the session is authenticated and running
	msgBye   = "bye"   // the server is closing the session
)

// clientMessage is one inbound message.
//
// One struct with optional fields rather than a discriminated union decoded in
// two passes: the payload is small, and a single decode means a malformed
// message cannot be half-applied.
type clientMessage struct {
	T string `json:"t"`

	// Hello. Credentials arrive in the FIRST MESSAGE, never in the URL — a URL
	// lands in browser history, in a Referer, and in every access log between
	// here and the client (§2.8).
	Ticket   string `json:"ticket,omitempty"`
	Session  string `json:"session,omitempty"`
	Identity string `json:"identity,omitempty"` // claimed principal, for sshkey
	Sig      string `json:"sig,omitempty"`      // SSHSIG armor, for sshkey
	Chal     string `json:"chal,omitempty"`     // challenge id, for sshkey

	// There are deliberately NO password fields here.
	//
	// Rev 11 made password a ticket minter rather than an attach mechanism, and
	// leaving the fields in place meant a custom client could still authenticate
	// a password directly over the WebSocket — the split existed in the prose and
	// not in the protocol. A password goes to the login route, which
	// returns a ticket; the ticket arrives above.

	// Measurements, on hello and resize.
	Cols    int     `json:"cols,omitempty"`
	Rows    int     `json:"rows,omitempty"`
	CellW   float64 `json:"cw,omitempty"`
	CellH   float64 `json:"ch,omitempty"`
	Pointer bool    `json:"pointer,omitempty"`
	Dark    bool    `json:"dark,omitempty"`
	FontOK  bool    `json:"fontok,omitempty"`

	// Key report.
	Key       string `json:"k,omitempty"`
	Repeat    bool   `json:"rep,omitempty"`
	Ctrl      bool   `json:"c,omitempty"`
	Alt       bool   `json:"a,omitempty"`
	Shift     bool   `json:"s,omitempty"`
	Meta      bool   `json:"m,omitempty"`
	AltGraph  bool   `json:"ag,omitempty"`
	Composing bool   `json:"ic,omitempty"`

	// Text and paste payload.
	Text string `json:"x,omitempty"`

	// Mouse report.
	Kind   string `json:"mk,omitempty"`
	Button int    `json:"btn,omitempty"`
	Dir    string `json:"dir,omitempty"`
	X      int    `json:"x2,omitempty"`
	Y      int    `json:"y2,omitempty"`

	// Focus.
	Gained bool `json:"g,omitempty"`

	// Ack.
	Rev uint64 `json:"rev,omitempty"`
}

// hello projects the measurement fields.
func (m clientMessage) hello() Hello {
	return Hello{
		Cols:          m.Cols,
		Rows:          m.Rows,
		Metrics:       Metrics{CellW: m.CellW, CellH: m.CellH},
		Pointer:       m.Pointer,
		PrefersDark:   m.Dark,
		FontAgreement: m.FontOK,
	}
}

// keyReport projects the key fields.
func (m clientMessage) keyReport() KeyReport {
	return KeyReport{
		Key:       m.Key,
		Repeat:    m.Repeat,
		Ctrl:      m.Ctrl,
		Alt:       m.Alt,
		Shift:     m.Shift,
		Meta:      m.Meta,
		AltGraph:  m.AltGraph,
		Composing: m.Composing,
	}
}

// mouseReport projects the pointer fields.
func (m clientMessage) mouseReport() MouseReport {
	return MouseReport{
		Kind:   m.Kind,
		Button: m.Button,
		Dir:    m.Dir,
		X:      m.X,
		Y:      m.Y,
		Ctrl:   m.Ctrl,
		Alt:    m.Alt,
		Shift:  m.Shift,
		Meta:   m.Meta,
	}
}

// serverMessage is one outbound message.
type serverMessage struct {
	T string `json:"t"`

	// Frame.
	Rev     uint64      `json:"rev,omitempty"`
	Full    bool        `json:"full,omitempty"`
	W       int         `json:"w,omitempty"`
	H       int         `json:"h,omitempty"`
	Updates []wireCell  `json:"u,omitempty"`
	Cursor  *wireCursor `json:"cur,omitempty"`

	// Ready.
	Session string `json:"session,omitempty"`

	// Bye.
	Reason string `json:"reason,omitempty"`
}

// wireCell is one cell update. Field names are short because a full-screen
// snapshot of a large terminal is thousands of them.
type wireCell struct {
	X int    `json:"x"`
	Y int    `json:"y"`
	S string `json:"s"`           // content
	W uint8  `json:"w,omitempty"` // display width; 0 means a continuation
	F string `json:"f,omitempty"` // foreground CSS color
	B string `json:"b,omitempty"` // background CSS color
	A uint16 `json:"a,omitempty"` // attribute mask
}

// wireCursor is the latched cursor.
type wireCursor struct {
	Visible bool  `json:"v"`
	X       int   `json:"x"`
	Y       int   `json:"y"`
	Shape   uint8 `json:"s,omitempty"`
}

// encodeFrame converts a [Frame] to its wire form.
//
// Colors are resolved to CSS here rather than in the client, so the client never
// needs a palette, a theme, or any notion of what an ANSI index means. The
// browser's job is to paint what it is told.
func encodeFrame(f Frame) serverMessage {
	out := serverMessage{T: msgFrame, Rev: f.Rev, Full: f.Full, W: f.W, H: f.H}
	out.Updates = make([]wireCell, 0, len(f.Updates))
	for _, u := range f.Updates {
		c := wireCell{X: u.X, Y: u.Y, S: u.Cell.Content, W: u.Cell.Width}
		fg, bg := u.Cell.Attrs.FG, u.Cell.Attrs.BG
		if u.Cell.Attrs.Mask&tui.AttrReverse != 0 {
			fg, bg = bg, fg
			if fg.Kind == tui.CellColorDefault {
				fg = defaultBGToken
			}
			if bg.Kind == tui.CellColorDefault {
				bg = defaultFGToken
			}
		}
		c.F = cssColor(fg)
		c.B = cssColor(bg)
		// Reverse is already applied above, so it is cleared from the mask the
		// client receives: sending it too would let a client apply the swap a
		// second time and undo it.
		c.A = uint16(u.Cell.Attrs.Mask & ^tui.AttrReverse)
		out.Updates = append(out.Updates, c)
	}
	cur := wireCursor{
		Visible: f.Cursor.Visible,
		X:       f.Cursor.X,
		Y:       f.Cursor.Y,
		Shape:   uint8(f.Cursor.Shape),
	}
	out.Cursor = &cur
	return out
}
