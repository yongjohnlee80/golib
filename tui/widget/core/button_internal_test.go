package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// These reach package-internal helpers, so they live in the internal test
// package. Everything testable through the public surface is in
// button_test.go, where it exercises the API a consumer actually has.

// TestWidgetStatePrecedence pins the one rule the whole toolkit shares, in
// every combination rather than the three that happen to arise.
func TestWidgetStatePrecedence(t *testing.T) {
	for _, tc := range []struct {
		disabled, armed, focused bool
		want                     WidgetState
	}{
		{false, false, false, WidgetStateNormal},
		{false, false, true, WidgetStateFocused},
		{false, true, false, WidgetStateArmed},
		{false, true, true, WidgetStateArmed}, // armed beats focused
		{true, false, false, WidgetStateDisabled},
		{true, false, true, WidgetStateDisabled}, // disabled beats focused
		{true, true, false, WidgetStateDisabled}, // disabled beats armed
		{true, true, true, WidgetStateDisabled},  // disabled beats everything
	} {
		got := resolveWidgetState(tc.disabled, tc.armed, tc.focused)
		if got != tc.want {
			t.Errorf("resolveWidgetState(disabled=%v armed=%v focused=%v) = %v, want %v",
				tc.disabled, tc.armed, tc.focused, got, tc.want)
		}
	}
}

// TestAnUnknownRoleIsRetainedNotRejected. Application-specific roles are
// additive: an app defines its own and carries it through the toolkit, which
// treats anything it does not recognise as normal.
func TestAnUnknownRoleIsRetainedNotRejected(t *testing.T) {
	const appRole ButtonRole = 42
	b := NewButton("Custom", WithRole(appRole))

	if got := b.Role(); got != appRole {
		t.Errorf("role = %v, want the value the app supplied (%d)", got, appRole)
	}
	// And it does not count as a Default or a Cancel for a container.
	checkButtonList("test", []*Button{b, NewButton("A", WithRole(appRole))}, nil)
}

// TestASecondDefaultOrRejectIsRefusedAtConstruction.
//
// A panic rather than a silent precedence rule: with two defaults, Enter picks
// one by an ordering the author never stated — with two Rejects, Escape does —
// and the dialog does the wrong thing in a way that reads as a toolkit bug
// rather than a mistake in the list.
func TestASecondDefaultOrRejectIsRefusedAtConstruction(t *testing.T) {
	for _, tc := range []struct {
		name string
		opt  ButtonOption
	}{
		{"two defaults", WithDefault(true)},
		{"two rejects", WithRole(ButtonRoleReject)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fatal := fatalFromWidget(func() {
				checkButtonList("test", []*Button{
					NewButton("A", tc.opt),
					NewButton("B", tc.opt),
				}, nil)
			})
			if fatal == nil {
				t.Fatalf("%s were accepted", tc.name)
			}
		})
	}

	// The control: one default, one Reject, and any number of Accepts and
	// Actions, is fine — Yes and Ok may both accept.
	fatal := fatalFromWidget(func() {
		checkButtonList("test", []*Button{
			NewButton("OK", WithRole(ButtonRoleAccept), WithDefault(true)),
			NewButton("Yes", WithRole(ButtonRoleAccept)),
			NewButton("Cancel", WithRole(ButtonRoleReject)),
			NewButton("Other"),
			NewButton("Another"),
		}, nil)
	})
	if fatal != nil {
		t.Errorf("a legal button set was rejected (%v)", fatal.Rule)
	}
}

// fatalFromWidget runs fn and returns the errs.Fatal it panicked with, or nil.
// The TYPE matters: "some panic" would be satisfied by a nil dereference in the
// guard, which is the opposite of the guard working.
func fatalFromWidget(fn func()) (f *errs.Fatal) {
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
