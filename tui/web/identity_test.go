package web

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// Refusing to be built is a caller error, and a caller must be able to say so
// by identity rather than by reading the sentence.
func TestConstructionRefusalsCarryInvalidArgument(t *testing.T) {
	cases := map[string]func() error{
		// NewHandler is deliberately NOT in this table. Its Manager check sits
		// behind the policy check, so NewHandler(Config{}, nil) fails on
		// ErrNoPolicy and never reaches the site under test — a fixture that
		// would have passed for the wrong reason if this only asserted "an
		// error came back".
		"PasswordPolicyExample with no contextual constraint": func() error {
			_, err := PasswordPolicyExample(nil, nil)
			return err
		},
		"PasswordPolicyExample with a nil constraint": func() error {
			_, err := PasswordPolicyExample(nil, nil, nil)
			return err
		},
		"NewManager without an AppFactory": func() error {
			_, err := NewManager(nil)
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Errorf("must satisfy ErrInvalidArgument, got %v", err)
			}
			// Not a precondition: nothing was in the wrong STATE, the
			// arguments were wrong. Blaming state sends a reader looking for
			// an ordering bug that does not exist.
			if errors.Is(err, errs.ErrPrecondition) {
				t.Error("must NOT satisfy ErrPrecondition — the arguments were wrong, " +
					"not the order of calls")
			}
		})
	}
}

// One condition, one name. Attaching a client that reported no usable metrics
// was refused in three places with two different sentences and no identity at
// all; a caller could not act on any of them.
func TestAttach_UnmeasuredClientHasOneIdentity(t *testing.T) {
	b := New()

	err := b.Attach(Hello{})
	if err == nil {
		t.Fatal("a hello with no usable metrics must be refused")
	}
	if !errors.Is(err, ErrUnmeasuredClient) {
		t.Errorf("must satisfy ErrUnmeasuredClient, got %v", err)
	}

	// The sentinel says what it is on its own, without a wrapper restating it —
	// there is nothing the base could add here, so nothing is added.
	if got, want := ErrUnmeasuredClient.Error(),
		"web: client hello has no usable size or font metrics"; got != want {
		t.Errorf("sentinel text = %q, want %q", got, want)
	}
}
