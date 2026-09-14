package widget_test

import (
	"sync"
	"sync/atomic"
	"testing"

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

// TestADisabledButtonRefusesEveryProducerButKeepsItsPlace.
//
// It still takes focus. Dropping a disabled control out of the tab ring moves
// the user's focus out from under them mid-interaction, so a keyboard user
// would lose their place in a dialog every time a field validated.
func TestADisabledButtonRefusesEveryProducerButKeepsItsPlace(t *testing.T) {
	var fired, sentinelFired atomic.Int64
	b := widget.NewButton("OK", widget.WithOnActivate(func() { fired.Add(1) }))
	// An ENABLED sibling, used as an ordered lane-A sentinel. Injected input
	// and App.Update travel different lanes, so a lane-B sync cannot prove the
	// presses below were dispatched — and this test's central claim is that
	// nothing happened, where "not yet" and "never" are indistinguishable.
	sentinel := widget.NewButton("S", widget.WithOnActivate(func() { sentinelFired.Add(1) }))
	root := tui.NewFlex(tui.Horizontal)
	root.Add(b, sentinel)
	h := startApp(t, root, 20, 1)
	defer h.stop()
	h.onLoop(func() { b.Context().RequestFocus() })
	h.sync()
	setEnabledOn(h, b, false)

	if !b.AcceptsFocus() {
		t.Error("a disabled button dropped out of the focus ring")
	}
	var focused bool
	h.onLoop(func() { focused = b.Context().Focused() })
	if !focused {
		t.Error("a disabled button lost the focus it already held")
	}
	if got := stateOn(h, b); got != widget.WidgetStateDisabled {
		t.Errorf("state = %v, want %v", got, widget.WidgetStateDisabled)
	}

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 1, Y: 0})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 1, Y: 0})

	// Press the SENTINEL and wait for it. Dispatch is ordered, so its
	// activation proves everything above has already been handled, and the
	// "nothing fired" assertion below is a real never rather than a not-yet.
	//
	// It goes after the disabled button's RELEASE on purpose: a press starts a
	// gesture that holds the pointer even on a disabled control, so a sentinel
	// sent mid-gesture would be delivered to the capture owner and never reach
	// the sibling at all.
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 6, Y: 0})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 6, Y: 0})
	h.waitFor("the enabled sentinel activated", func() bool { return sentinelFired.Load() == 1 })

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
