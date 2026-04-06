package ingestor

import (
	"errors"
	"testing"
)

func TestBatchErrors_Error(t *testing.T) {
	t.Parallel()

	be := &BatchErrors{
		Errors: []error{
			errors.New("write failed"),
			errors.New("disk full"),
		},
	}

	got := be.Error()
	want := "write failed; disk full"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestBatchErrors_Unwrap(t *testing.T) {
	t.Parallel()

	inner := errors.New("write failed")
	be := &BatchErrors{Errors: []error{inner}}

	if !errors.Is(be, inner) {
		t.Error("errors.Is should match inner error via Unwrap")
	}
}

func TestBatchErrors_Single(t *testing.T) {
	t.Parallel()

	be := &BatchErrors{Errors: []error{errors.New("oops")}}
	if be.Error() != "oops" {
		t.Errorf("Error() = %q, want %q", be.Error(), "oops")
	}
}
