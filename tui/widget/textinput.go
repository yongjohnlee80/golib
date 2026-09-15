package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Single-Line Text Input & Form Field Architecture
//
// TextInput provides a feature-rich, single-line text input field designed for forms,
// prompt inputs, search bars, and dialog credentials. It features UAX #29 grapheme
// cluster cursor navigation, anchor selection, horizontal viewport scrolling, placeholder
// hints, password character masking, validation hooks, and synchronized form advance.
//
// # Subsystem Role & Responsibilities
//
//  1. Grapheme Cluster Integrity: Values are stored and addressed as Unicode Standard
//     Annex #29 extended grapheme clusters ([TextInput.cs]). Multi-byte runes, emoji
//     zwj sequences, and combining accents never suffer midpoint truncation or splitting.
//  2. IME & Hardware Cursor Integration: Implements [tui.CursorReporter]. While focused,
//     the runtime places the terminal's hardware cursor exactly at the input caret cell,
//     anchoring OS IME composition popups directly beneath the active insertion point.
//  3. Bracketed Paste Protection: Ingests [tui.PasteEvent] atomically without replaying
//     as simulated keystrokes. Internal newlines are transformed into spaces, preventing
//     malicious or pasted multiline strings from inadvertently triggering early form submission.
//  4. Synchronous Advance Hook ([WithOnSubmit]): Supports synchronous form focus transitions.
//     When Enter is pressed and validation passes, [WithOnSubmit] executes immediately inside
//     the key handler before subsequent buffered keystrokes can leak into the vacated field.
//  5. Readline Keyboard Navigation: Supports common Readline / Emacs motion chords
//     (Ctrl+A for start, Ctrl+E for end, Ctrl+U to clear before cursor, Ctrl+W to delete word)
//     alongside standard arrow keys and Shift+arrow range selection.
//
// # Layout & Cursor Pipeline
//
//	┌─────────────────────────────────────────────────────────────┐
//	│ [TextInput] Single-Line Viewport (Height = 1)               │
//	│                                                             │
//	│  Visible Viewport Cells: [ w = Layout Width ]               │
//	│  ┌────────────────────────────────────────────────────────┐ │
//	│  │ ...scrolled text... [ Active Caret █ ] remainder text  │ │
//	│  └────────────────────────────────────────────────────────┘ │
//	│       ▲                        ▲                            │
//	│   scroll offset           Hardware Cursor Anchor            │
//	└─────────────────────────────────────────────────────────────┘
//
// # Architectural Invariants
//
//  1. Tab Traversal Non-Consumption:
//     TextInput explicitly does NOT consume Tab or Shift+Tab. Focus navigation remains
//     strictly framework-owned and orchestratable across sibling components.
//  2. Atomic Paste Events:
//     Paste operations generate exactly one [ChangeEvent] and never trigger a [SubmitEvent].
//  3. Validation Preflight on Enter:
//     Failing validation ([WithValidate]) sets the internal error state, visually styles the
//     text with [TextInputStyles.Error], and suppresses both [WithOnSubmit] and [SubmitEvent].
//
// # Concurrency & Goroutine Ownership
//
// TextInput is loop-goroutine-owned. Reading or setting values ([TextInput.Value],
// [TextInput.SetValue]) must be performed on the main application loop goroutine.
//
// # Usage Examples
//
// 1. Creating a validated username field with placeholder:
//
//	usernameInput := widget.NewTextInput(
//		widget.WithPlaceholder("Enter username (min 3 chars)..."),
//		widget.WithValidate(func(val string) error {
//			if len(strings.TrimSpace(val)) < 3 {
//				return errors.New("username too short")
//			}
//			return nil
//		}),
//	)
//
// 2. Creating a password input field:
//
//	passwordInput := widget.NewTextInput(
//		widget.WithPlaceholder("Password..."),
//		widget.WithMask('*'),
//	)
type TextInput struct {
	Base
	cs     []string // value as grapheme clusters
	cur    int      // cursor: cluster index in [0, len(cs)]
	anchor int      // selection anchor cluster index; -1 = no selection
	scroll int      // cells scrolled off the left edge
	width  int      // last layout width (viewport cells)

	placeholder string
	mask        rune
	validate    func(string) error
	onSubmit    func(string)
	verr        error

	styles TextInputStyles
}

var (
	_ tui.Focusable      = (*TextInput)(nil)
	_ tui.CursorReporter = (*TextInput)(nil)
)

// TextInputStyles are the TextInput style hooks. Zero
// fields inherit the defaults.
type TextInputStyles struct {
	Text        style.Style // value text (default: theme foreground)
	Placeholder style.Style // default: TokenTextMuted
	Selection   style.Style // default: inverted, no accent
	Error       style.Style // value text in the failed-validation state
}

// TextInputOption customizes a TextInput under construction.
type TextInputOption func(*TextInput)

// WithPlaceholder sets the text rendered (muted) while the value is empty.
func WithPlaceholder(s string) TextInputOption {
	return func(t *TextInput) { t.placeholder = s }
}

// WithMask enables password masking: the given rune paints in place of
// every cluster while Value() returns the raw string.
func WithMask(r rune) TextInputOption {
	if r == 0 {
		panic("widget: WithMask: zero mask rune")
	}
	return func(t *TextInput) { t.mask = r }
}

// WithValidate installs the validation hook consulted on Enter.
func WithValidate(fn func(string) error) TextInputOption {
	return func(t *TextInput) { t.validate = fn }
}

// WithOnSubmit installs a SYNCHRONOUS Enter hook, run inside the key handler.
//
// It exists because SubmitEvent is not synchronous, and for one class of
// subscriber that difference is a defect rather than a detail. Bus.Publish
// QUEUES delivery onto the program lane, and the App selects between the input
// lane and the program lane -- so when a keystroke is already waiting, Go
// picks between them pseudo-randomly. A subscriber that moves focus on Enter
// therefore moves it BEFORE or AFTER the next keystroke, about half the time
// each, and the keystroke that loses goes to the input the operator has just
// left.
//
// Measured, in a consumer whose form advances focus on Enter: typing "demo"
// Enter "sqlite" produced a name of "demos" and an engine of "qlite". Every
// paste is that race, since a paste is a burst of keystrokes with none of a
// human's delay.
//
// The SubmitEvent is still published, unchanged, so existing subscribers are
// unaffected. Use this hook only for work that must complete before the next
// key is dispatched -- moving focus, closing the surface -- and the event for
// everything else.
func WithOnSubmit(fn func(string)) TextInputOption {
	return func(t *TextInput) { t.onSubmit = fn }
}

// WithInitialValue sets the starting value (cursor at the end).
func WithInitialValue(s string) TextInputOption {
	return func(t *TextInput) {
		t.cs = clusters(sanitizeLine(s))
		t.cur = len(t.cs)
	}
}

// WithTextInputStyles overrides the style hooks; zero fields keep defaults.
func WithTextInputStyles(st TextInputStyles) TextInputOption {
	return func(t *TextInput) {
		t.styles = TextInputStyles{
			Text:        st.Text.Inherit(t.styles.Text),
			Placeholder: st.Placeholder.Inherit(t.styles.Placeholder),
			Selection:   st.Selection.Inherit(t.styles.Selection),
			Error:       st.Error.Inherit(t.styles.Error),
		}
	}
}

// NewTextInput builds an empty single-line editor.
func NewTextInput(opts ...TextInputOption) *TextInput {
	t := &TextInput{
		anchor: -1,
		styles: TextInputStyles{
			Placeholder: style.New().Foreground(style.TokenTextMuted),
			Selection:   style.New().Reverse(true),
			Error:       style.New().Foreground(style.TokenError),
		},
	}
	for _, o := range opts {
		if o != nil {
			o(t)
		}
	}
	return t
}

// Value returns the raw value (unmasked).
func (t *TextInput) Value() string { return strings.Join(t.cs, "") }

// SetValue replaces the value programmatically (loop goroutine): cursor to
// the end, selection and validation error cleared. No ChangeEvent — the
// event reports user edits.
func (t *TextInput) SetValue(s string) {
	t.cs = clusters(sanitizeLine(s))
	t.cur = len(t.cs)
	t.anchor = -1
	t.verr = nil
	t.ensureVisible()
	t.MarkDirty()
}

// Err returns the current validation error (set by a failed Enter, cleared
// on the next edit).
func (t *TextInput) Err() error { return t.verr }

// AcceptsFocus implements tui.Focusable.
func (t *TextInput) AcceptsFocus() bool { return true }

// sanitizeLine strips a single-line value: newlines become spaces, other
// C0 controls are dropped.
func sanitizeLine(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, strings.ReplaceAll(s, "\r\n", "\n"))
}

// renderCluster returns the painted form of cluster i (mask-aware).
func (t *TextInput) renderCluster(i int) string {
	if t.mask != 0 {
		return string(t.mask)
	}
	return t.cs[i]
}

// cellAt is the display offset (cells) of cluster index i in painted form.
func (t *TextInput) cellAt(i int) int {
	w := 0
	for j := 0; j < i && j < len(t.cs); j++ {
		w += t.measure(t.renderCluster(j))
	}
	return w
}

// ensureVisible adjusts the horizontal scroll so the cursor cell is inside
// the viewport (using the last layout width).
func (t *TextInput) ensureVisible() {
	if t.width <= 0 {
		return
	}
	cx := t.cellAt(t.cur)
	if cx < t.scroll {
		t.scroll = cx
	}
	if cx >= t.scroll+t.width {
		t.scroll = cx - t.width + 1
	}
	total := t.cellAt(len(t.cs)) + 1 // +1: the cursor cell past the end
	t.scroll = max(0, min(t.scroll, max(total-t.width, 0)))
}

// selection returns the selected cluster range [lo, hi), ok=false when no
// selection exists.
func (t *TextInput) selection() (lo, hi int, ok bool) {
	if t.anchor < 0 || t.anchor == t.cur {
		return 0, 0, false
	}
	lo, hi = t.anchor, t.cur
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi, true
}

// deleteRange removes clusters [lo, hi) and moves the cursor to lo.
func (t *TextInput) deleteRange(lo, hi int) {
	t.cs = append(t.cs[:lo], t.cs[hi:]...)
	t.cur = lo
	t.anchor = -1
}

// insert places text at the cursor (replacing any selection) as one atomic
// edit, then emits ChangeEvent.
func (t *TextInput) insert(text string) {
	if lo, hi, ok := t.selection(); ok {
		t.deleteRange(lo, hi)
	}
	ins := clusters(sanitizeLine(text))
	if len(ins) > 0 {
		t.cs = append(t.cs[:t.cur], append(append([]string(nil), ins...), t.cs[t.cur:]...)...)
		t.cur += len(ins)
	}
	t.edited()
}

// edited finalizes a value mutation: error cleared, scroll fixed, repaint,
// ChangeEvent.
func (t *TextInput) edited() {
	t.verr = nil
	t.ensureVisible()
	t.MarkDirty()
	t.publish(ChangeEvent{Owner: t.NodeID(), Value: t.Value()})
}

// moveTo moves the cursor, extending the selection when extend is set and
// clearing it otherwise.
func (t *TextInput) moveTo(i int, extend bool) {
	i = max(0, min(i, len(t.cs)))
	if extend {
		if t.anchor < 0 {
			t.anchor = t.cur
		}
	} else {
		t.anchor = -1
	}
	t.cur = i
	t.ensureVisible()
	t.MarkDirty()
}

// wordLeft/wordRight are readline-style word hops (spaces, then non-spaces).
func (t *TextInput) wordLeft() int {
	i := t.cur
	for i > 0 && t.cs[i-1] == " " {
		i--
	}
	for i > 0 && t.cs[i-1] != " " {
		i--
	}
	return i
}

func (t *TextInput) wordRight() int {
	i := t.cur
	for i < len(t.cs) && t.cs[i] == " " {
		i++
	}
	for i < len(t.cs) && t.cs[i] != " " {
		i++
	}
	return i
}

// modMask are the modifiers that veto text insertion.
const nonTextMods = tui.ModCtrl | tui.ModAlt | tui.ModSuper | tui.ModHyper | tui.ModMeta

// HandleEvent implements the key contract.
func (t *TextInput) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.PasteEvent:
		t.insert(e.Text)
		return true
	case tui.KeyEvent:
		return t.handleKey(e)
	}
	return false
}

func (t *TextInput) handleKey(e tui.KeyEvent) bool {
	if e.Kind == tui.KeyRelease {
		return false
	}
	shift := e.Mods&tui.ModShift != 0
	word := e.Mods&(tui.ModCtrl|tui.ModAlt) != 0

	switch e.Code {
	case tui.KeyEnter:
		if t.validate != nil {
			if err := t.validate(t.Value()); err != nil {
				t.verr = err
				t.MarkDirty()
				return true
			}
		}
		t.verr = nil
		t.MarkDirty()
		// The hook runs HERE, on the input lane, before this handler returns
		// and therefore before any further keystroke is dispatched. The event
		// follows and is delivered on the program lane as it always was.
		if t.onSubmit != nil {
			t.onSubmit(t.Value())
		}
		t.publish(SubmitEvent{Owner: t.NodeID(), Value: t.Value()})
		return true
	case tui.KeyBackspace:
		switch lo, hi, ok := t.selection(); {
		case ok:
			t.deleteRange(lo, hi)
		case word && t.wordLeft() < t.cur:
			t.deleteRange(t.wordLeft(), t.cur)
		case !word && t.cur > 0:
			t.deleteRange(t.cur-1, t.cur)
		default:
			return true // consumed; nothing to delete
		}
		t.edited()
		return true
	case tui.KeyDelete:
		switch lo, hi, ok := t.selection(); {
		case ok:
			t.deleteRange(lo, hi)
		case t.cur < len(t.cs):
			t.deleteRange(t.cur, t.cur+1)
		default:
			return true
		}
		t.edited()
		return true
	case tui.KeyLeft:
		if word {
			t.moveTo(t.wordLeft(), shift)
		} else {
			t.moveTo(t.cur-1, shift)
		}
		return true
	case tui.KeyRight:
		if word {
			t.moveTo(t.wordRight(), shift)
		} else {
			t.moveTo(t.cur+1, shift)
		}
		return true
	case tui.KeyHome:
		t.moveTo(0, shift)
		return true
	case tui.KeyEnd:
		t.moveTo(len(t.cs), shift)
		return true
	}

	// Readline subset (Ctrl+A/E/U/W).
	if e.Mods&tui.ModCtrl != 0 {
		switch e.Code {
		case 'a':
			t.moveTo(0, false)
			return true
		case 'e':
			t.moveTo(len(t.cs), false)
			return true
		case 'u':
			if t.cur > 0 {
				t.deleteRange(0, t.cur)
				t.edited()
			}
			return true
		case 'w':
			if w := t.wordLeft(); w < t.cur {
				t.deleteRange(w, t.cur)
				t.edited()
			}
			return true
		}
		return false
	}

	// Printable text (never Tab — focus traversal is framework-owned).
	if e.Text != "" && e.Mods&nonTextMods == 0 && e.Code != tui.KeyTab {
		t.insert(e.Text)
		return true
	}
	return false
}

// Layout: height 1, width greedy; the viewport width feeds cursor-visible
// scrolling.
func (t *TextInput) Layout(c tui.Constraints) tui.Size {
	w := boundedMax(c.MaxW, max(c.MinW, t.cellAt(len(t.cs))+1))
	t.width = w
	t.ensureVisible()
	return c.Constrain(tui.Size{W: w, H: 1})
}

// Cursor implements tui.CursorReporter: the insertion point in local
// coordinates (the runtime consults it only while this widget is focused).
func (t *TextInput) Cursor() (int, int, bool) {
	x := t.cellAt(t.cur) - t.scroll
	if t.width > 0 {
		x = min(x, t.width-1)
	}
	return max(x, 0), 0, true
}

// Render paints value (or placeholder), selection fill, and mask runes.
func (t *TextInput) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	if len(t.cs) == 0 {
		if t.placeholder != "" {
			drawText(s, 0, 0, truncate(t.placeholder, sz.W, s.StringWidth), t.styles.Placeholder)
		}
		return
	}
	base := t.styles.Text
	if t.verr != nil {
		base = t.styles.Error.Inherit(t.styles.Text)
	}
	selLo, selHi, hasSel := t.selection()
	x := -t.scroll
	for i := range t.cs {
		cl := t.renderCluster(i)
		cw := s.StringWidth(cl)
		if x+cw > sz.W {
			break
		}
		if x >= 0 {
			st := base
			if hasSel && t.focused() && i >= selLo && i < selHi {
				st = t.styles.Selection.Inherit(base)
			}
			s.SetCell(x, 0, cl, st)
		}
		x += cw
	}
}
