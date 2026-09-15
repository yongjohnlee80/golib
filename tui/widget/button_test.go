package widget_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/errs"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Button is the toolkit's claim that the runtime layers underneath it carry
// their weight: it contains no pointer arithmetic, no press/release
// bookkeeping, no hit-testing and no capture handling, and it is supposed to be
// fully interactive anyway. Most of what follows tests that claim rather than
// the button's own hundred lines.

// compile-time: Button satisfies the capability it advertises, with the whole
// method set. This fails to build if either half of Activatable drifts.
var _ tui.Activatable = (*widget.Button)(nil)

// armedOn / stateOn / enabledOn read Button state ON THE LOOP GOROUTINE.
//
// Component state is owned by the loop — that single-ownership rule is what
// lets widget authors write plain Go with no mutexes — so a test reading it
// directly is the exact race the model prevents, and -race says so.
func armedOn(h *harness, b *widget.Button) bool {
	var v bool
	h.onLoop(func() { v = b.Armed() })
	return v
}

func stateOn(h *harness, b *widget.Button) widget.WidgetState {
	var v widget.WidgetState
	h.onLoop(func() { v = b.State() })
	return v
}

func setEnabledOn(h *harness, b *widget.Button, v bool) {
	h.onLoop(func() { b.SetEnabled(v) })
}

// TestAButtonActivatesFromEveryProducer is the layer's whole point. Keyboard,
// pointer and program reach the same Activate through three entirely different
// paths — a resolved action, the gesture recogniser, and a direct call — and
// the button implements none of them.
func TestAButtonActivatesFromEveryProducer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		drive  func(h *harness, b *widget.Button)
		origin tui.ActionOrigin
	}{
		{"Enter", func(h *harness, b *widget.Button) {
			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
		}, tui.OriginKey},
		{"Space", func(h *harness, b *widget.Button) {
			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: ' '})
		}, tui.OriginKey},
		{"pointer press and release", func(h *harness, b *widget.Button) {
			h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0})
			h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 1, Y: 0})
		}, tui.OriginPointer},
		{"DoAction", func(h *harness, b *widget.Button) {
			h.onLoop(func() { b.Context().DoAction(tui.ActivateAction{}) })
		}, tui.OriginProgrammatic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fired atomic.Int64
			b := widget.NewButton("OK", widget.WithOnActivate(func() { fired.Add(1) }))
			h := startApp(t, b, 12, 1)
			defer h.stop()
			h.onLoop(func() { b.Context().RequestFocus() })
			h.sync()

			var mu sync.Mutex
			var events []tui.ControlActivatedEvent
			unsub := tui.Subscribe(h.app.Bus(), func(ev tui.ControlActivatedEvent) {
				mu.Lock()
				events = append(events, ev)
				mu.Unlock()
			})
			defer unsub()

			tc.drive(h, b)
			h.waitFor("activated", func() bool { return fired.Load() == 1 })
			h.sync()

			if got := fired.Load(); got != 1 {
				t.Errorf("callback ran %d times, want exactly 1", got)
			}
			mu.Lock()
			evs := append([]tui.ControlActivatedEvent(nil), events...)
			mu.Unlock()
			if len(evs) != 1 {
				t.Fatalf("%d ControlActivatedEvent(s) published, want 1", len(evs))
			}
			if evs[0].Origin != tc.origin {
				t.Errorf("event origin = %v, want %v", evs[0].Origin, tc.origin)
			}
		})
	}
}

// TestTheButtonArmsAndDisarmsWithoutOwningAnyPointerCode. The press/release
// feedback comes entirely from the runtime's recogniser; Button contributes
// SetArmed and nothing else.
func TestTheButtonArmsAndDisarmsWithoutOwningAnyPointerCode(t *testing.T) {
	// The button must NOT fill the terminal, or there is nowhere outside it to
	// drag to and the drag-out half of the test asserts nothing. A vertical
	// Flex leaves it its natural width at the left edge, so x >= its width is
	// genuinely beyond it.
	b := widget.NewButton("OK")
	// HORIZONTAL, not vertical: a vertical Flex stretches its children across
	// the full cross axis, which would give the button the whole terminal width
	// and leave nowhere outside it to drag to.
	root := tui.NewFlex(tui.Horizontal)
	root.Add(b)
	h := startApp(t, root, 20, 3)
	defer h.stop()
	h.sync()

	// PRECONDITION, through the public surface: x=18 really is outside the
	// button, so the drag-out assertion below is about leaving it rather than
	// about nothing. Checked while the button is DOWN — after a release it
	// reads unarmed either way, which is how an earlier version of this check
	// passed against a button that spanned the whole screen.
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 18, Y: 0})
	h.sync()
	outsideArmed := armedOn(h, b)
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 18, Y: 0})
	h.sync()
	if outsideArmed {
		t.Fatal("precondition failed: a press at x=18 armed the button, so x=18 is " +
			"inside it and the drag-out case below would prove nothing")
	}

	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0})
	h.waitFor("armed by the press", func() bool { return armedOn(h, b) })
	if got := stateOn(h, b); got != widget.WidgetStateArmed {
		t.Errorf("state while pressed = %v, want %v", got, widget.WidgetStateArmed)
	}

	// Dragging off disarms without ending the gesture; dragging back re-arms.
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: 18, Y: 0})
	h.waitFor("disarmed on the way out", func() bool { return !armedOn(h, b) })
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: 1, Y: 0})
	h.waitFor("re-armed on the way back", func() bool { return armedOn(h, b) })

	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 1, Y: 0})
	h.waitFor("disarmed after the release", func() bool { return !armedOn(h, b) })
}

// TestADisabledButtonRefusesEveryProducerAndLeavesTheRing.
//
// Leaving the focus ring is what makes the container contracts expressible: a
// dialog cycles its ENABLED buttons, prefers the enabled default on opening,
// and falls back to itself only when every button is disabled. None of that can
// be stated while disabled controls remain tab stops.
func TestADisabledButtonRefusesEveryProducerAndLeavesTheRing(t *testing.T) {
	var fired, sentinelFired atomic.Int64
	b := widget.NewButton("OK", widget.WithOnActivate(func() { fired.Add(1) }))
	// An enabled sibling: an ordered lane-A sentinel, and also the node focus
	// should be repaired to when the first button is disabled.
	sentinel := widget.NewButton("S", widget.WithOnActivate(func() { sentinelFired.Add(1) }))
	root := tui.NewFlex(tui.Horizontal)
	root.Add(b, sentinel)
	h := startApp(t, root, 20, 1)
	defer h.stop()
	h.onLoop(func() { b.Context().RequestFocus() })
	h.sync()

	var focusedBefore bool
	h.onLoop(func() { focusedBefore = b.Context().Focused() })
	if !focusedBefore {
		t.Fatal("precondition failed: the button under test never held focus")
	}

	setEnabledOn(h, b, false)

	if b.AcceptsFocus() {
		t.Error("a disabled button is still a tab stop")
	}
	// Repaired SYNCHRONOUSLY. Anything after this line — a traversal, a query,
	// a repaint — would otherwise run against a focus that is already wrong.
	var stillFocused bool
	h.onLoop(func() { stillFocused = b.Context().Focused() })
	if stillFocused {
		t.Error("focus stayed on a button that stopped accepting it; SetEnabled must " +
			"revalidate the scope before returning")
	}
	if got := stateOn(h, b); got != widget.WidgetStateDisabled {
		t.Errorf("state = %v, want %v", got, widget.WidgetStateDisabled)
	}

	// No keyboard case here, and its absence is the point: once a disabled
	// button has left the focus ring it cannot hold focus, so a key can no
	// longer be addressed to it at all. Injecting Enter would land on whichever
	// enabled control focus was repaired to, and would be testing that instead.
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 1, Y: 0})

	// The sentinel press is the ordered proof the two above were dispatched. It
	// ALSO proves the disabled button no longer takes a gesture capture: if it
	// did, this press would be delivered to the capture owner and never arrive.
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 6, Y: 0})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 6, Y: 0})
	h.waitFor("the enabled sentinel activated", func() bool { return sentinelFired.Load() >= 1 })

	var direct bool
	h.onLoop(func() { direct = b.Activate(tui.OriginProgrammatic) })
	h.sync()

	if direct {
		t.Error("Activate returned true on a disabled button")
	}
	if got := fired.Load(); got != 0 {
		t.Errorf("a disabled button fired its callback %d time(s)", got)
	}
	if armedOn(h, b) {
		t.Error("a disabled button showed the pressed look")
	}

	// Re-enabling puts it back in the ring.
	setEnabledOn(h, b, true)
	if !b.AcceptsFocus() {
		t.Error("a re-enabled button did not return to the focus ring")
	}
}

// TestDisablingMidPressClearsThePressedLook. Leaving it armed would show a
// control held down that nothing will now release.
func TestDisablingMidPressClearsThePressedLook(t *testing.T) {
	b := widget.NewButton("OK")
	h := startApp(t, b, 12, 1)
	defer h.stop()
	h.sync()

	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0})
	h.waitFor("armed", func() bool { return armedOn(h, b) })

	setEnabledOn(h, b, false)
	if armedOn(h, b) {
		t.Error("still armed after being disabled mid-press")
	}
	if got := stateOn(h, b); got != widget.WidgetStateDisabled {
		t.Errorf("state = %v, want %v", got, widget.WidgetStateDisabled)
	}
}

// TestButtonStyleIsNilSafe. A button with no style asks a nil style for values
// on every paint, so the nil check belongs here rather than in every
func TestButtonStyleIsNilSafe(t *testing.T) {
	var s *widget.ButtonStyle
	def := widget.DefaultButtonStyle()

	if s.Normal() != def.Normal() {
		t.Error("nil.Normal() does not match the default")
	}
	if s.Focused() != def.Focused() {
		t.Error("nil.Focused() does not match the default")
	}
	if s.Armed() != def.Armed() {
		t.Error("nil.Armed() does not match the default")
	}
	if s.Disabled() != def.Disabled() {
		t.Error("nil.Disabled() does not match the default")
	}
	for _, st := range []widget.WidgetState{
		widget.WidgetStateNormal, widget.WidgetStateFocused, widget.WidgetStateArmed, widget.WidgetStateDisabled,
	} {
		if s.Style(st) != def.Style(st) {
			t.Errorf("nil.Style(%v) does not match the default", st)
		}
	}
	// A With* on nil yields a complete value, not one with three empty looks.
	got := s.WithNormal(style.New().Bold(true))
	if got.Focused() != def.Focused() || got.Armed() != def.Armed() || got.Disabled() != def.Disabled() {
		t.Error("With* on a nil style did not fill the other three looks from the default")
	}
}

// TestButtonStyleIsImmutable. These values are shared between widgets by
// design, so a With* that mutated in place would restyle every button holding
// the same pointer.
func TestButtonStyleIsImmutable(t *testing.T) {
	base := widget.NewButtonStyle(style.New().Bold(true), style.New().Italic(true))
	before := base.Normal()

	derived := base.WithNormal(style.New().Faint(true))

	if base.Normal() != before {
		t.Error("WithNormal mutated the receiver; styles are shared and must be copied")
	}
	if derived.Normal() == before {
		t.Error("WithNormal did not change the copy")
	}
	if derived.Focused() != base.Focused() {
		t.Error("the copy lost an unrelated look")
	}
}

// TestButtonStyleDerivesTheTwoLooksAuthorsSkip, and the full constructor
// overrides both.
func TestButtonStyleDerivesTheTwoLooksAuthorsSkip(t *testing.T) {
	normal := style.New().Bold(true)
	focused := style.New().Italic(true)

	two := widget.NewButtonStyle(normal, focused)
	if two.Disabled() != normal.Faint(true) {
		t.Error("disabled is not derived as normal faded")
	}
	if two.Armed() != focused.Reverse(true) {
		t.Error("armed is not derived as focused inverted")
	}

	disabled := style.New().Underline(true)
	armed := style.New().Blink(true)
	full := widget.NewButtonStyleFull(normal, focused, disabled, armed)
	if full.Disabled() != disabled || full.Armed() != armed {
		t.Error("the full constructor did not override the derivations")
	}
}

// TestStyleSelectorMapsEveryStateAndFallsBack. One selector, so a widget cannot
// apply its own precedence by picking an accessor itself; and an unknown state
// paints normally rather than crashing mid-paint.
func TestStyleSelectorMapsEveryStateAndFallsBack(t *testing.T) {
	s := widget.NewButtonStyleFull(
		style.New().Bold(true), style.New().Italic(true),
		style.New().Underline(true), style.New().Blink(true))

	for st, want := range map[widget.WidgetState]style.Style{
		widget.WidgetStateNormal:   s.Normal(),
		widget.WidgetStateFocused:  s.Focused(),
		widget.WidgetStateArmed:    s.Armed(),
		widget.WidgetStateDisabled: s.Disabled(),
	} {
		if got := s.Style(st); got != want {
			t.Errorf("Style(%v) returned the wrong look", st)
		}
	}
	if got := s.Style(widget.WidgetState(200)); got != s.Normal() {
		t.Error("an unknown widget.WidgetState did not fall back to normal")
	}
}

// TestClosedEnumsNameEveryValue. These strings reach traces and failures, and
// the default arms catch a value added later without a name.
func TestClosedEnumsNameEveryValue(t *testing.T) {
	for v, want := range map[widget.ButtonRole]string{
		widget.ButtonRoleNormal:  "normal",
		widget.ButtonRoleDefault: "default",
		widget.ButtonRoleCancel:  "cancel",
	} {
		if got := v.String(); got != want {
			t.Errorf("widget.ButtonRole(%d).String() = %q, want %q", v, got, want)
		}
	}
	if got := widget.ButtonRole(200).String(); got != "unknown" {
		t.Errorf("an undefined role rendered as %q, want %q", got, "unknown")
	}

	for v, want := range map[widget.WidgetState]string{
		widget.WidgetStateNormal:   "normal",
		widget.WidgetStateFocused:  "focused",
		widget.WidgetStateArmed:    "armed",
		widget.WidgetStateDisabled: "disabled",
	} {
		if got := v.String(); got != want {
			t.Errorf("widget.WidgetState(%d).String() = %q, want %q", v, got, want)
		}
	}
	if got := widget.WidgetState(200).String(); got != "unknown" {
		t.Errorf("an undefined state rendered as %q, want %q", got, "unknown")
	}
}

// TestAZeroConfiguredButtonIsInertButUsable — the state an author reaches
// before wiring anything up.
func TestAZeroConfiguredButtonIsInertButUsable(t *testing.T) {
	b := widget.NewButton("Hi")

	if b.Role() != widget.ButtonRoleNormal {
		t.Errorf("role = %v, want %v", b.Role(), widget.ButtonRoleNormal)
	}
	if !b.Enabled() {
		t.Error("a new button is disabled")
	}
	if !b.AcceptsFocus() {
		t.Error("a new button is not focusable")
	}
	// Direct access is correct HERE and only here: nothing is mounted yet, so
	// there is no loop goroutine to race with. Everywhere below a running App,
	// state is read on the loop.
	if b.Armed() {
		t.Error("a new button is armed")
	}
	if got := b.State(); got != widget.WidgetStateNormal {
		t.Errorf("state = %v, want %v", got, widget.WidgetStateNormal)
	}

	h := startApp(t, b, 12, 1)
	defer h.stop()
	h.sync()
	// Visible, and activating it does nothing rather than panicking.
	h.wantContains("Hi")
	var ok bool
	h.onLoop(func() { ok = b.Activate(tui.OriginProgrammatic) })
	if !ok {
		t.Error("a button with no callback refused to activate; it should succeed and do nothing")
	}
}

// TestTheButtonPaintsTheLookItsStateSelects. Rendering asks State rather than
// choosing, so the precedence rule cannot drift from the rest of the toolkit.
func TestTheButtonPaintsTheLookItsStateSelects(t *testing.T) {
	st := widget.NewButtonStyleFull(
		style.New().Foreground(style.TokenForeground),
		style.New().Foreground(style.TokenPrimary),
		style.New().Foreground(style.TokenTextMuted),
		style.New().Foreground(style.TokenAccent))
	b := widget.NewButton("Go", widget.WithButtonStyle(st))

	h := startApp(t, b, 8, 1)
	defer h.stop()
	h.sync()
	h.wantContains("Go")

	normalAttrs := cellAttrs(h, 0, 0)

	// Focus changes the selected state, which must change what is PAINTED.
	// Asserting only that the label is still on screen would pass against a
	// Render that ignored the state entirely.
	h.onLoop(func() { b.Context().RequestFocus() })
	h.sync()
	if got := stateOn(h, b); got != widget.WidgetStateFocused {
		t.Errorf("state after focusing = %v, want %v", got, widget.WidgetStateFocused)
	}
	focusedAttrs := cellAttrs(h, 0, 0)
	if focusedAttrs == normalAttrs {
		t.Error("the painted cell is unchanged after focusing; Render is not asking " +
			"State for the look")
	}
	h.wantContains("Go")
}

// TestPointerPolicyDisablesTheButtonsMouseOnly.
func TestPointerPolicyDisablesTheButtonsMouseOnly(t *testing.T) {
	var fired atomic.Int64
	b := widget.NewButton("OK", widget.WithOnActivate(func() { fired.Add(1) }))
	h := startApp(t, b, 12, 1)
	defer h.stop()
	h.onLoop(func() {
		b.Context().RequestFocus()
		b.WithPointerPolicy(tui.PointerDisabled)
	})
	h.sync()

	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 1, Y: 0})
	// The keyboard still works, which also proves the pointer events above were
	// dispatched rather than merely pending.
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.waitFor("the keyboard still activates", func() bool { return fired.Load() == 1 })
	h.sync()

	if got := fired.Load(); got != 1 {
		t.Errorf("activations = %d, want 1 (keyboard only): the pointer must be refused "+
			"and the keyboard untouched", got)
	}
	if armedOn(h, b) {
		t.Error("a mouse-disabled button was armed by a press")
	}
}

// TestTheCallbackRunsBeforeTheEventIsPublished, so anything observing the event
// sees state the callback has already settled.
func TestTheCallbackRunsBeforeTheEventIsPublished(t *testing.T) {
	var order []string
	var mu sync.Mutex
	note := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }

	b := widget.NewButton("OK", widget.WithOnActivate(func() { note("callback") }))
	h := startApp(t, b, 12, 1)
	defer h.stop()
	h.onLoop(func() { b.Context().RequestFocus() })
	h.sync()

	unsub := tui.Subscribe(h.app.Bus(), func(tui.ControlActivatedEvent) { note("event") })
	defer unsub()

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.waitFor("both observed", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) == 2
	})

	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	if got[0] != "callback" || got[1] != "event" {
		t.Errorf("order = %v, want [callback event]", got)
	}
}

// TestOnlyAnUnmodifiedKeyPressActivates. A key RELEASE would double every
// keyboard activation, and a modified Enter belongs to whatever binding the app
// has put on Ctrl-Enter rather than to the button underneath it.
func TestOnlyAnUnmodifiedKeyPressActivates(t *testing.T) {
	var fired atomic.Int64
	b := widget.NewButton("OK", widget.WithOnActivate(func() { fired.Add(1) }))
	h := startApp(t, b, 12, 1)
	defer h.stop()
	h.onLoop(func() { b.Context().RequestFocus() })
	h.sync()

	h.inject(tui.KeyEvent{Kind: tui.KeyRelease, Code: tui.KeyEnter})
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter, Mods: tui.ModCtrl})
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: ' ', Mods: tui.ModAlt})

	// The plain press that SHOULD work is also the ordered proof that the three
	// above were dispatched, so a count of one means three were refused rather
	// than that nothing has run yet.
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.waitFor("the unmodified press activated", func() bool { return fired.Load() == 1 })
	h.sync()

	if got := fired.Load(); got != 1 {
		t.Errorf("activations = %d, want exactly 1: a key release and a modified key "+
			"must not activate", got)
	}
}

// TestAnUnmountedButtonRefusesToActivate. Its callback may reference a tree
// that no longer exists, so activation after unmount is never safe.
func TestAnUnmountedButtonRefusesToActivate(t *testing.T) {
	var fired atomic.Int64
	b := widget.NewButton("OK", widget.WithOnActivate(func() { fired.Add(1) }))

	// Never mounted: no Context at all.
	if b.Activate(tui.OriginProgrammatic) {
		t.Error("an unmounted button reported a successful activation")
	}
	if got := fired.Load(); got != 0 {
		t.Errorf("an unmounted button fired its callback %d time(s)", got)
	}

	// The control: the same button, mounted, does activate — so the refusal
	// above is about being unmounted rather than about the button being inert.
	h := startApp(t, b, 12, 1)
	defer h.stop()
	h.sync()
	var ok bool
	h.onLoop(func() { ok = b.Activate(tui.OriginProgrammatic) })
	if !ok || fired.Load() != 1 {
		t.Errorf("the mounted control failed: ok=%v fired=%d", ok, fired.Load())
	}
}

// TestADisabledButtonRefusesToShowPressed.
//
// The runtime drives SetArmed and cannot see a widget's own disabled flag, so
// the refusal has to live in the widget. Driven directly rather than through a
// gesture: a press on a disabled control still starts one, so the state has to
// be read mid-gesture, and calling the method the runtime calls is both simpler
// and exactly what is being claimed.
func TestADisabledButtonRefusesToShowPressed(t *testing.T) {
	b := widget.NewButton("OK")
	h := startApp(t, b, 12, 1)
	defer h.stop()
	h.sync()
	setEnabledOn(h, b, false)

	h.onLoop(func() { b.SetArmed(true) })
	if armedOn(h, b) {
		t.Error("a disabled button showed the pressed look; disabling and looking " +
			"pressed at once is a state the user cannot resolve")
	}
	if got := stateOn(h, b); got != widget.WidgetStateDisabled {
		t.Errorf("state = %v, want %v", got, widget.WidgetStateDisabled)
	}

	// The control: re-enabled, the same call is honoured, so the refusal above
	// is about being disabled rather than about SetArmed never working.
	setEnabledOn(h, b, true)
	h.onLoop(func() { b.SetArmed(true) })
	if !armedOn(h, b) {
		t.Error("an enabled button ignored SetArmed(true)")
	}
}

// TestEveryWithMethodCopiesAndTouchesOnlyItsOwnLook.
//
// Table-driven over all four rather than one representative: they are separate
// methods with separate field assignments, and a copy-paste slip in any of them
// — the classic being three that all write the same field — is invisible to a
// test that only exercises WithNormal.
func TestEveryWithMethodCopiesAndTouchesOnlyItsOwnLook(t *testing.T) {
	base := widget.NewButtonStyleFull(
		style.New().Bold(true),
		style.New().Italic(true),
		style.New().Faint(true),
		style.New().Reverse(true))
	marker := style.New().Underline(true)

	for _, tc := range []struct {
		name  string
		apply func(*widget.ButtonStyle) *widget.ButtonStyle
		got   func(*widget.ButtonStyle) style.Style
	}{
		{"WithNormal", func(s *widget.ButtonStyle) *widget.ButtonStyle { return s.WithNormal(marker) },
			func(s *widget.ButtonStyle) style.Style { return s.Normal() }},
		{"WithFocused", func(s *widget.ButtonStyle) *widget.ButtonStyle { return s.WithFocused(marker) },
			func(s *widget.ButtonStyle) style.Style { return s.Focused() }},
		{"WithArmed", func(s *widget.ButtonStyle) *widget.ButtonStyle { return s.WithArmed(marker) },
			func(s *widget.ButtonStyle) style.Style { return s.Armed() }},
		{"WithDisabled", func(s *widget.ButtonStyle) *widget.ButtonStyle { return s.WithDisabled(marker) },
			func(s *widget.ButtonStyle) style.Style { return s.Disabled() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			derived := tc.apply(base)

			if got := tc.got(derived); got != marker {
				t.Errorf("%s did not set the look it names", tc.name)
			}
			// The other three are carried across untouched, which is what
			// catches a method writing the wrong field.
			all := map[string][2]style.Style{
				"normal":   {derived.Normal(), base.Normal()},
				"focused":  {derived.Focused(), base.Focused()},
				"armed":    {derived.Armed(), base.Armed()},
				"disabled": {derived.Disabled(), base.Disabled()},
			}
			for look, pair := range all {
				if pair[0] != marker && pair[0] != pair[1] {
					t.Errorf("%s also changed the %s look", tc.name, look)
				}
			}
			// And the receiver is untouched: these values are shared.
			if base.Normal() != style.New().Bold(true) ||
				base.Focused() != style.New().Italic(true) ||
				base.Armed() != style.New().Reverse(true) ||
				base.Disabled() != style.New().Faint(true) {
				t.Errorf("%s mutated the receiver", tc.name)
			}
		})
	}
}

// TestTheChainableStyleSetterRestylesALiveButton — the runtime half of the
// styling story, and the one a consumer uses to re-theme without rebuilding.
func TestTheChainableStyleSetterRestylesALiveButton(t *testing.T) {
	loud := widget.NewButtonStyleFull(
		style.New().Foreground(style.TokenError),
		style.New().Foreground(style.TokenError),
		style.New().Foreground(style.TokenError),
		style.New().Foreground(style.TokenError))

	b := widget.NewButton("Go")
	h := startApp(t, b, 8, 1)
	defer h.stop()
	h.sync()
	before := cellAttrs(h, 0, 0)

	var returned *widget.Button
	h.onLoop(func() { returned = b.WithStyle(loud) })
	h.settle()

	if returned != b {
		t.Error("WithStyle did not return the receiver, so it cannot be chained")
	}
	if after := cellAttrs(h, 0, 0); after == before {
		t.Error("the painted cell is unchanged after restyling a live button")
	}
}

// TestTheLabelCanBeReadAndReplaced.
func TestTheLabelCanBeReadAndReplaced(t *testing.T) {
	b := widget.NewButton("First")
	if got := b.Label(); got != "First" {
		t.Errorf("Label() = %q, want %q", got, "First")
	}

	h := startApp(t, b, 12, 1)
	defer h.stop()
	h.sync()
	h.wantContains("First")

	h.onLoop(func() { b.SetLabel("Second") })
	h.settle()

	var got string
	h.onLoop(func() { got = b.Label() })
	if got != "Second" {
		t.Errorf("Label() = %q, want %q", got, "Second")
	}
	h.wantContains("Second")
	h.wantNotContains("First")
}

// TestLabelWidthFollowsGraphemeClustersNotRunes.
//
// A rune count is wrong in three independent ways, and each one is a different
// visible defect: a CJK ideograph needs two columns, a combining mark needs
// none and would otherwise overwrite the character it belongs to, and an emoji
// ZWJ sequence is many runes in a single cell.
func TestLabelWidthFollowsGraphemeClustersNotRunes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		label string
		want  int // expected label width in columns
	}{
		{"ascii", "OK", 2},
		{"cjk wide", "確定", 4},
		{"combining mark", "é", 1},
		{"zwj emoji", "\U0001F468‍\U0001F4BB", 2},
		{"mixed", "a確", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// UNDECORATED, because this is about how the LABEL is measured.
			// The brackets a button wears by default are clusters of their own
			// and would just be added to every expectation below, testing the
			// decoration twice and the measurement less clearly.
			b := widget.NewButton(tc.label, widget.WithButtonDecoration("", ""))
			flex := tui.NewFlex(tui.Horizontal)
			flex.Add(b)
			// A terminal far wider than any label, so what is measured is the
			// size the button CHOOSES rather than a clamp against the screen.
			h := startApp(t, flex, 40, 1)
			defer h.stop()
			h.settle()

			var got tui.Size
			h.onLoop(func() { got = b.Layout(tui.Loose(tui.Size{W: 40, H: 1})) })
			if got.W != tc.want+2 {
				t.Errorf("intrinsic width = %d, want %d (label %d + 2 padding): a rune "+
					"count would give %d", got.W, tc.want+2, tc.want, len([]rune(tc.label))+2)
			}
		})
	}
}

// TestAWideClusterIsNeverSplitAcrossTheEdge. Half a double-width cluster in the
// final column is a broken cell, not a truncated string: the terminal has no
// way to draw it and the row's alignment is lost from there on.
func TestAWideClusterIsNeverSplitAcrossTheEdge(t *testing.T) {
	// Three wide clusters want six columns; the surface offers five.
	b := widget.NewButton("確定中")
	flex := tui.NewFlex(tui.Horizontal)
	flex.Add(b)
	h := startApp(t, flex, 5, 1)
	defer h.stop()
	h.settle()

	snap := h.tb.Snapshot()
	for x, cell := range snap[0] {
		if cell.Width == 2 && x == len(snap[0])-1 {
			t.Errorf("a double-width cluster was written into the last column (%d); "+
				"its second half has nowhere to go", x)
		}
	}
}

// TestChangingTheLabelRelayoutsTheParent. The label IS the button's intrinsic
// width, so a repaint alone leaves the parent holding stale geometry — and
// therefore stale hit bounds, which is a wrong answer to a mouse click rather
// than merely a stale picture.
func TestChangingTheLabelRelayoutsTheParent(t *testing.T) {
	var fired atomic.Int64
	b := widget.NewButton("Hi", widget.WithOnActivate(func() { fired.Add(1) }))
	flex := tui.NewFlex(tui.Horizontal)
	flex.Add(b)
	h := startApp(t, flex, 30, 1)
	defer h.stop()
	h.settle()

	// x=8 is beyond "Hi" (4 columns) and inside "Much longer label" (19).
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 8, Y: 0})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 8, Y: 0})
	h.settle()
	if got := fired.Load(); got != 0 {
		t.Fatalf("precondition failed: x=8 already hit the short button (%d activations)", got)
	}

	h.onLoop(func() { b.SetLabel("Much longer label") })
	h.settle()
	h.wantContains("Much longer label")

	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 8, Y: 0})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 8, Y: 0})
	h.waitFor("the widened button is hit at x=8", func() bool { return fired.Load() == 1 })
}

// TestPointerPolicyChainedBeforeMountIsHonoured. NewButton(...).WithPointerPolicy(...)
// is the most natural way to write this and runs before any Context exists;
// applying it only when mounted made that chain a silent no-op.
func TestPointerPolicyChainedBeforeMountIsHonoured(t *testing.T) {
	var fired atomic.Int64
	b := widget.NewButton("OK", widget.WithOnActivate(func() { fired.Add(1) })).
		WithPointerPolicy(tui.PointerDisabled)

	h := startApp(t, b, 12, 1)
	defer h.stop()
	h.onLoop(func() { b.Context().RequestFocus() })
	h.sync()

	var eff tui.PointerPolicy
	h.onLoop(func() { eff = b.Context().EffectivePointerPolicy() })
	if eff != tui.PointerDisabled {
		t.Errorf("effective policy after mount = %v, want %v: a policy requested "+
			"before mount must be applied when the Context arrives", eff, tui.PointerDisabled)
	}

	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 1, Y: 0})
	// The keyboard still works, and is the ordered proof the pointer events
	// above were dispatched.
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.waitFor("the keyboard still activates", func() bool { return fired.Load() == 1 })
	h.sync()

	if got := fired.Load(); got != 1 {
		t.Errorf("activations = %d, want 1 (keyboard only)", got)
	}
}

// TestAnInvalidPointerPolicyIsRejectedBeforeMountWithoutMutating.
func TestAnInvalidPointerPolicyIsRejectedBeforeMountWithoutMutating(t *testing.T) {
	b := widget.NewButton("OK").WithPointerPolicy(tui.PointerDisabled)

	fatal := fatalFromWidgetExt(func() { b.WithPointerPolicy(tui.PointerPolicy(200)) })
	if fatal == nil {
		t.Fatal("an out-of-range policy was accepted before mount")
	}

	// The earlier request survives: a rejected call changes nothing.
	h := startApp(t, b, 12, 1)
	defer h.stop()
	h.sync()
	var eff tui.PointerPolicy
	h.onLoop(func() { eff = b.Context().EffectivePointerPolicy() })
	if eff != tui.PointerDisabled {
		t.Errorf("effective policy = %v, want %v: a rejected call must leave the "+
			"previous request intact", eff, tui.PointerDisabled)
	}
}

// TestACallbackFreeButtonStillPublishesItsActivation. It is callback-free, not
// inert — observers on the bus still see the activation, and the documentation
// now says so.
func TestACallbackFreeButtonStillPublishesItsActivation(t *testing.T) {
	b := widget.NewButton("Hi") // no WithOnActivate
	h := startApp(t, b, 12, 1)
	defer h.stop()
	h.sync()

	var seen atomic.Int64
	unsub := tui.Subscribe(h.app.Bus(), func(tui.ControlActivatedEvent) { seen.Add(1) })
	defer unsub()

	var ok bool
	h.onLoop(func() { ok = b.Context().DoAction(tui.ActivateAction{}) })
	h.waitFor("the activation was published", func() bool { return seen.Load() == 1 })

	if !ok {
		t.Error("a callback-free button refused to activate")
	}
}

// fatalFromWidgetExt is the external-package twin of the internal helper.
func fatalFromWidgetExt(fn func()) (f *errs.Fatal) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(errs.Fatal); ok {
				f = &e
			}
		}
	}()
	fn()
	return nil
}

// TestDisablingMidPressCancelsTheGestureOnce.
//
// The capture has to be released through the ordinary loss path, not merely
// dropped: the owner is told once, with a reason, exactly as it would be for
// any other way a gesture dies.
func TestDisablingMidPressCancelsTheGestureOnce(t *testing.T) {
	b := widget.NewButton("OK")
	var sentinelFired atomic.Int64
	sentinel := widget.NewButton("S", widget.WithOnActivate(func() { sentinelFired.Add(1) }))
	root := tui.NewFlex(tui.Horizontal)
	root.Add(b, sentinel)
	h := startApp(t, root, 20, 1)
	defer h.stop()
	h.settle()

	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0})
	h.waitFor("armed", func() bool { return armedOn(h, b) })

	setEnabledOn(h, b, false)
	if armedOn(h, b) {
		t.Error("still armed after being disabled mid-press")
	}

	// The capture must be gone: a press on the sibling now reaches it. While a
	// capture is held every pointer event goes to the owner instead, so this is
	// the observable difference between releasing and merely disarming.
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 6, Y: 0})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 6, Y: 0})
	h.waitFor("the sibling is reachable again", func() bool { return sentinelFired.Load() == 1 })
}

// TestADisabledButtonNeverStartsAGesture is the routing half of F4, separate
// from the visual half: the runtime must not begin a gesture whose only
// possible outcome is an activation that will be refused.
func TestADisabledButtonNeverStartsAGesture(t *testing.T) {
	b := widget.NewButton("OK")
	var sibFired atomic.Int64
	sib := widget.NewButton("S", widget.WithOnActivate(func() { sibFired.Add(1) }))
	root := tui.NewFlex(tui.Horizontal)
	root.Add(b, sib)
	h := startApp(t, root, 20, 1)
	defer h.stop()
	h.settle()
	setEnabledOn(h, b, false)

	// Press the disabled button and DO NOT release it. If a gesture had begun,
	// the capture would still be held here and the sibling press below would be
	// delivered to the disabled button instead of reaching the sibling.
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0})
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 6, Y: 0})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 6, Y: 0})
	h.waitFor("the sibling was reached mid-press", func() bool { return sibFired.Load() == 1 })

	if armedOn(h, b) {
		t.Error("a disabled button was armed")
	}
}

// lossRecordingButton records the capture-loss events its Button is sent, so a
// cancellation can be checked at the boundary a real widget would act on rather
// than through the runtime's private state.
type lossRecordingButton struct {
	*widget.Button
	losses atomic.Int64
	reason atomic.Int64
}

func (l *lossRecordingButton) HandleEvent(ev tui.Event) bool {
	if e, ok := ev.(tui.PointerCaptureLostEvent); ok {
		l.losses.Add(1)
		l.reason.Store(int64(e.Reason))
	}
	return l.Button.HandleEvent(ev)
}

// TestDisablingMidPressCancelsRatherThanDriftingOut.
//
// Focus repair would release the capture anyway, so the count alone proves
// nothing — both paths deliver exactly one loss. What differs is the REASON,
// and the reason is what a widget branches on: "the program ended this
// gesture" and "focus wandered off" call for different cleanup.
func TestDisablingMidPressCancelsRatherThanDriftingOut(t *testing.T) {
	lb := &lossRecordingButton{Button: widget.NewButton("OK")}
	flex := tui.NewFlex(tui.Horizontal)
	flex.Add(lb)
	h := startApp(t, flex, 20, 1)
	defer h.stop()
	h.settle()

	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0})
	h.waitFor("armed", func() bool { return armedOn(h, lb.Button) })

	h.onLoop(func() { lb.SetEnabled(false) })
	h.waitFor("the owner was told", func() bool { return lb.losses.Load() == 1 })
	h.settle()

	if got := lb.losses.Load(); got != 1 {
		t.Errorf("capture losses = %d, want exactly 1", got)
	}
	if got := tui.CaptureLostReason(lb.reason.Load()); got != tui.CaptureLostCancelled {
		t.Errorf("loss reason = %v, want %v: disabling is the program ending the "+
			"gesture, not focus drifting away from it", got, tui.CaptureLostCancelled)
	}
}

// TestACombiningMarkIsPaintedWithItsBaseCharacter.
//
// Painting rune by rune puts the zero-width mark in its own cell, where it
// overwrites the character it belongs to — the label renders as the wrong text
// rather than merely being mis-measured, which is why the width test alone does
// not cover this.
func TestACombiningMarkIsPaintedWithItsBaseCharacter(t *testing.T) {
	// Undecorated for the same reason as the width test above: this counts
	// PAINTED CELLS, and brackets are painted cells with nothing to do with
	// combining marks.
	b := widget.NewButton("éx", widget.WithButtonDecoration("", "")) // "éx" as base + combining acute
	flex := tui.NewFlex(tui.Horizontal)
	flex.Add(b)
	h := startApp(t, flex, 10, 1)
	defer h.stop()
	h.settle()

	snap := h.tb.Snapshot()
	var painted []string
	for _, c := range snap[0] {
		if c.Content != "" && c.Content != " " {
			painted = append(painted, c.Content)
		}
	}
	if len(painted) != 2 {
		t.Fatalf("painted %d non-blank cells %q, want 2: the base and its combining "+
			"mark belong in ONE cell", len(painted), painted)
	}
	if painted[0] != "é" {
		t.Errorf("first painted cell = %q, want %q: the combining mark must travel "+
			"with its base character", painted[0], "é")
	}
	if painted[1] != "x" {
		t.Errorf("second painted cell = %q, want %q", painted[1], "x")
	}
}

// TestDisablingClearsAnArmedLookThatNoGestureOwns.
//
// Distinct from disabling mid-press, and not covered by it: SetArmed is public,
// so a control can be showing pressed without the runtime holding a gesture for
// it. Cancelling the gesture then clears nothing, and only SetEnabled's own
// reset gets the button out of a pressed-and-unavailable state.
func TestDisablingClearsAnArmedLookThatNoGestureOwns(t *testing.T) {
	b := widget.NewButton("OK")
	h := startApp(t, b, 12, 1)
	defer h.stop()
	h.sync()

	h.onLoop(func() { b.SetArmed(true) })
	if !armedOn(h, b) {
		t.Fatal("precondition failed: the button is not armed, so disabling it clears nothing")
	}
	setEnabledOn(h, b, false)

	if armedOn(h, b) {
		t.Error("a button armed without a gesture stayed pressed after being disabled; " +
			"cancelling a gesture cannot clear a look no gesture owns")
	}
	if got := stateOn(h, b); got != widget.WidgetStateDisabled {
		t.Errorf("state = %v, want %v", got, widget.WidgetStateDisabled)
	}
}

// focusWatcher records the focus-loss events bubbled to it, so the promised
// notification can be observed rather than inferred from the focused id.
type focusWatcher struct {
	*widget.Button
	losses atomic.Int64
}

func (f *focusWatcher) HandleEvent(ev tui.Event) bool {
	if e, ok := ev.(tui.FocusEvent); ok && !e.Gained && !e.Terminal {
		f.losses.Add(1)
	}
	return f.Button.HandleEvent(ev)
}

// TestDisablingTheOnlyFocusedButtonNotifiesItBeforeReturning.
//
// Clearing the focused id directly changes it behind the node's back: the
// component and its ancestors never learn they lost focus, so anything that
// repaints on the event keeps drawing itself focused. The notification is the
// point, not the id.
func TestDisablingTheOnlyFocusedButtonNotifiesItBeforeReturning(t *testing.T) {
	fw := &focusWatcher{Button: widget.NewButton("Only")}
	flex := tui.NewFlex(tui.Horizontal)
	flex.Add(fw)
	h := startApp(t, flex, 20, 1)
	defer h.stop()
	h.onLoop(func() { fw.Context().RequestFocus() })
	h.sync()

	var focused bool
	h.onLoop(func() { focused = fw.Context().Focused() })
	if !focused {
		t.Fatal("precondition failed: the only button never held focus")
	}
	if got := fw.losses.Load(); got != 0 {
		t.Fatalf("precondition failed: %d focus losses before disabling", got)
	}

	setEnabledOn(h, fw.Button, false)

	// Both checked with no further sync: SetEnabled must have completed the
	// whole transition before returning.
	if got := fw.losses.Load(); got != 1 {
		t.Errorf("bubbled FocusEvent{Gained:false} count = %d, want exactly 1: the "+
			"node must be told it lost focus, not merely stop being the focused id", got)
	}
	var stillFocused bool
	h.onLoop(func() { stillFocused = fw.Context().Focused() })
	if stillFocused {
		t.Error("the disabled button still holds focus")
	}
}

// TestDisablingTheFocusedButtonFocusesTheEnabledSibling.
//
// Asserting only that the disabled button lost focus is not enough: a repair
// that cleared focus to nothing would satisfy that and leave the dialog with no
// keyboard target at all. The sibling must be the actual destination.
func TestDisablingTheFocusedButtonFocusesTheEnabledSibling(t *testing.T) {
	a := widget.NewButton("A")
	b := widget.NewButton("B")
	flex := tui.NewFlex(tui.Horizontal)
	flex.Add(a, b)
	h := startApp(t, flex, 20, 1)
	defer h.stop()
	h.onLoop(func() { a.Context().RequestFocus() })
	h.sync()

	setEnabledOn(h, a, false)

	var bFocused, aFocused bool
	h.onLoop(func() { bFocused = b.Context().Focused(); aFocused = a.Context().Focused() })
	if aFocused {
		t.Error("the disabled button still holds focus")
	}
	if !bFocused {
		t.Error("focus was not moved to the enabled sibling; a repair that merely " +
			"cleared focus would leave the scope with no keyboard target")
	}
}

// TestLabelWidthFollowsTheActiveWidthPolicy.
//
// The Unicode table above runs entirely under the default policy, so it kills a
// rune count but not the subtler error: measuring with the PACKAGE-level width
// function instead of the mounted Context's. An East Asian ambiguous character
// is the only input that separates them, because it is the one whose width the
// policy actually changes.
func TestLabelWidthFollowsTheActiveWidthPolicy(t *testing.T) {
	const ambiguous = "①" // East Asian Ambiguous: narrow by default, wide under the CJK policy

	widthUnder := func(p tui.WidthPolicy) int {
		b := widget.NewButton(ambiguous)
		flex := tui.NewFlex(tui.Horizontal)
		flex.Add(b)
		h := startAppOpts(t, flex, 20, 1, tui.WithWidthPolicy(p))
		defer h.stop()
		h.settle()
		var got tui.Size
		h.onLoop(func() { got = b.Layout(tui.Loose(tui.Size{W: 20, H: 1})) })
		return got.W
	}

	narrow := widthUnder(tui.WidthPolicyDefault)
	wide := widthUnder(tui.WidthPolicyAmbiguousWide)

	if wide-narrow != 1 {
		t.Errorf("width under AmbiguousWide - width under Default = %d, want exactly 1 "+
			"(%d vs %d): the button must measure through the MOUNTED policy, not a "+
			"package-level default", wide-narrow, wide, narrow)
	}
}
