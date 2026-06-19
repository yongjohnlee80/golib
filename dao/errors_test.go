package dao

import (
	"errors"
	"strings"
	"testing"
)

func TestConstraintError_IsAndAs(t *testing.T) {
	t.Parallel()

	e := &ConstraintError{Constraint: "artist_uri_key", Kind: Unique, Err: ErrDuplicate}

	if !errors.Is(e, ErrDuplicate) {
		t.Error("errors.Is(e, ErrDuplicate) = false, want true")
	}
	var ce *ConstraintError
	if !errors.As(error(e), &ce) || ce.Kind != Unique {
		t.Errorf("errors.As failed or wrong kind: %+v", ce)
	}
	if !strings.Contains(e.Error(), "artist_uri_key") {
		t.Errorf("Error() = %q, want it to name the constraint", e.Error())
	}
}

func TestConstraintError_NoName(t *testing.T) {
	t.Parallel()

	e := &ConstraintError{Kind: NotNull, Err: ErrNotNull}
	if strings.Contains(e.Error(), `""`) {
		t.Errorf("Error() should omit an empty constraint name: %q", e.Error())
	}
}

func TestConstraintKind_String(t *testing.T) {
	t.Parallel()

	cases := map[ConstraintKind]string{
		Unique:            "unique",
		NotNull:           "not-null",
		ForeignKey:        "foreign-key",
		Check:             "check",
		UnknownConstraint: "unknown",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", k, got, want)
		}
	}
}
