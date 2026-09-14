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

// TestASecondDefaultOrCancelIsRefusedAtConstruction.
//
// A panic rather than a silent precedence rule: with two defaults, Enter picks
// one by an ordering the author never stated, and the dialog does the wrong
// thing in a way that reads as a toolkit bug rather than a mistake in the list.
func TestASecondDefaultOrCancelIsRefusedAtConstruction(t *testing.T) {
	for _, tc := range []struct {
		name string
		role ButtonRole
	}{
		{"two defaults", ButtonRoleDefault},
		{"two cancels", ButtonRoleCancel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fatal := fatalFromWidget(func() {
				checkButtonList("test", []*Button{
					NewButton("A", WithRole(tc.role)),
					NewButton("B", WithRole(tc.role)),
				}, nil)
			})
			if fatal == nil {
				t.Fatalf("a second %v was accepted", tc.role)
			}
		})
	}

	// The control: one of each, plus normals, is fine.
	fatal := fatalFromWidget(func() {
		checkButtonList("test", []*Button{
			NewButton("OK", WithRole(ButtonRoleDefault)),
			NewButton("Cancel", WithRole(ButtonRoleCancel)),
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
