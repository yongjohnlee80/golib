package dao

import (
	"errors"
	"strings"
	"testing"
)

// ADR-0017 §2.1 / §2.2a, criterion 2 — the driver-neutral half of the option
// contract: the value domain, the cross-field policy, and the error identities
// that let a caller tell "you typed something wrong" apart from "this driver
// cannot do that".

func TestTxOptions_ZeroValueIsServerDefault(t *testing.T) {
	t.Parallel()

	var o TxOptions
	if !o.IsDefault() {
		t.Fatal("the zero TxOptions must be the server-default case — it is what DataConn.Begin has always sent")
	}
	if got := o.String(); got != "" {
		t.Errorf("zero TxOptions renders %q, want the empty SQL tail", got)
	}
	if err := o.Validate(); err != nil {
		t.Errorf("zero TxOptions must validate: %v", err)
	}
	for _, o := range []TxOptions{
		{Access: TxReadOnly},
		{Isolation: TxReadCommitted},
		{Access: TxReadOnly, Isolation: TxSerializable, Deferrable: TxDeferrable},
	} {
		if o.IsDefault() {
			t.Errorf("%+v reported as default", o)
		}
	}
}

func TestTxOptions_StringRendersTheSQLTail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		opts TxOptions
		want string
	}{
		{TxOptions{}, ""},
		{TxOptions{Access: TxReadOnly}, "read only"},
		{TxOptions{Access: TxReadWrite}, "read write"},
		{TxOptions{Isolation: TxReadUncommitted}, "isolation level read uncommitted"},
		{TxOptions{Isolation: TxSerializable, Access: TxReadOnly}, "isolation level serializable read only"},
		{
			TxOptions{Isolation: TxSerializable, Access: TxReadOnly, Deferrable: TxDeferrable},
			"isolation level serializable read only deferrable",
		},
		{
			TxOptions{Isolation: TxSerializable, Access: TxReadOnly, Deferrable: TxNotDeferrable},
			"isolation level serializable read only not deferrable",
		},
	}
	for _, tt := range tests {
		if got := tt.opts.String(); got != tt.want {
			t.Errorf("%+v.String() = %q, want %q", tt.opts, got, tt.want)
		}
	}
}

// The full isolation domain is enumerable and stable: rev 3 added READ
// UNCOMMITTED because pgx and mysql both accept it, and a matrix that omitted
// it was dishonest.
func TestTxOptions_IsolationDomain(t *testing.T) {
	t.Parallel()

	want := []string{"", "read uncommitted", "read committed", "repeatable read", "serializable"}
	for i, w := range want {
		iso := TxIsolation(i)
		if !iso.valid() {
			t.Errorf("TxIsolation(%d) reported invalid", i)
		}
		if got := iso.String(); got != w {
			t.Errorf("TxIsolation(%d).String() = %q, want %q", i, got, w)
		}
	}
	if TxIsolation(len(want)).valid() {
		t.Errorf("TxIsolation(%d) must be outside the domain", len(want))
	}
}

func TestTxOptions_InvalidEnumValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opts   TxOptions
		option string
	}{
		{"access out of range", TxOptions{Access: TxAccess(9)}, "Access"},
		{"isolation out of range", TxOptions{Isolation: TxIsolation(9)}, "Isolation"},
		{"deferrable out of range", TxOptions{Deferrable: TxDeferrableMode(9)}, "Deferrable"},
		{"negative", TxOptions{Access: TxAccess(-1)}, "Access"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.opts.Validate()
			var invalid *ErrTxOptionInvalid
			if !errors.As(err, &invalid) {
				t.Fatalf("Validate() = %v, want *ErrTxOptionInvalid", err)
			}
			if invalid.Option != tt.option {
				t.Errorf("Option = %q, want %q", invalid.Option, tt.option)
			}
			// Malformed input is NOT a capability miss: a caller must not be
			// able to "handle" a typo as a driver limitation (ADR-0017 §2.2a).
			if errors.Is(err, ErrUnsupported) {
				t.Error("ErrTxOptionInvalid must NOT match dao.ErrUnsupported")
			}
		})
	}
}

// DEFERRABLE has an effect only under SERIALIZABLE READ ONLY. Anywhere else it
// is refused rather than silently accepted and ignored.
func TestTxOptions_DeferrableRequiresSerializableReadOnly(t *testing.T) {
	t.Parallel()

	valid := []TxOptions{
		{Isolation: TxSerializable, Access: TxReadOnly, Deferrable: TxDeferrable},
		{Isolation: TxSerializable, Access: TxReadOnly, Deferrable: TxNotDeferrable},
		{Isolation: TxSerializable, Access: TxReadOnly},
	}
	for _, o := range valid {
		if err := o.Validate(); err != nil {
			t.Errorf("%+v must validate: %v", o, err)
		}
	}

	invalid := []TxOptions{
		{Deferrable: TxDeferrable},
		{Deferrable: TxNotDeferrable},
		{Isolation: TxSerializable, Deferrable: TxDeferrable}, // no READ ONLY
		{Access: TxReadOnly, Deferrable: TxDeferrable},        // no SERIALIZABLE
		{Isolation: TxRepeatableRead, Access: TxReadOnly, Deferrable: TxDeferrable},
		{Isolation: TxSerializable, Access: TxReadWrite, Deferrable: TxDeferrable},
	}
	for _, o := range invalid {
		err := o.Validate()
		var e *ErrTxOptionInvalid
		if !errors.As(err, &e) {
			t.Errorf("%+v: Validate() = %v, want *ErrTxOptionInvalid", o, err)
			continue
		}
		if e.Option != "Deferrable" {
			t.Errorf("%+v: Option = %q, want %q", o, e.Option, "Deferrable")
		}
		if e.Reason == "" {
			t.Errorf("%+v: a combination violation must explain the rule", o)
		}
		if errors.Is(err, ErrUnsupported) {
			t.Errorf("%+v: combination errors must NOT match dao.ErrUnsupported", o)
		}
	}
}

// The capability-miss error is the mirror image: it DOES match ErrUnsupported
// (the ADR-0008 contract every capability-gated operation in this package
// honors), and errors.As reaches the driver and option names.
func TestErrTxOptionUnsupported_Identity(t *testing.T) {
	t.Parallel()

	err := error(&ErrTxOptionUnsupported{Driver: "mysql", Option: "Access=read write"})
	if !errors.Is(err, ErrUnsupported) {
		t.Error("ErrTxOptionUnsupported must match dao.ErrUnsupported")
	}
	// It is emphatically not the std library's sentinel — rev 3 pinned the
	// existing dao one, and matching both would blur the ADR-0008 contract.
	if errors.Is(err, errors.ErrUnsupported) {
		t.Error("ErrTxOptionUnsupported must not match errors.ErrUnsupported (std)")
	}
	var unsup *ErrTxOptionUnsupported
	if !errors.As(err, &unsup) {
		t.Fatalf("errors.As failed on %v", err)
	}
	if unsup.Driver != "mysql" || unsup.Option != "Access=read write" {
		t.Errorf("As gave Driver=%q Option=%q", unsup.Driver, unsup.Option)
	}
	for _, want := range []string{"mysql", "Access=read write"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not name %q", err.Error(), want)
		}
	}
}

func TestErrTxOptionInvalid_Message(t *testing.T) {
	t.Parallel()

	bare := (&ErrTxOptionInvalid{Option: "Access", Value: "TxAccess(9)"}).Error()
	if !strings.Contains(bare, "Access") || !strings.Contains(bare, "TxAccess(9)") {
		t.Errorf("message %q must name the option and the value", bare)
	}
	withReason := (&ErrTxOptionInvalid{
		Option: "Deferrable", Value: "deferrable", Reason: "valid only with X",
	}).Error()
	if !strings.Contains(withReason, "valid only with X") {
		t.Errorf("message %q must carry the reason", withReason)
	}
}
