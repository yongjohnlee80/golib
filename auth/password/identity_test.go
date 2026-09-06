package password

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// A caller rejecting bad configuration must be able to say so by IDENTITY.
// These errors previously had none: they were anonymous errors.New values, so
// the only way to react to one was to match its sentence.
func TestNew_RejectionsCarryInvalidArgument(t *testing.T) {
	cases := map[string]func() (*Factor, error){
		"a missing Store": func() (*Factor, error) { return New(nil) },
		"a nil Hasher":    func() (*Factor, error) { return New(stubStore{}, Hash(nil)) },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := call()
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Errorf("must satisfy ErrInvalidArgument, got %v", err)
			}
		})
	}
}

// stubStore satisfies Store so the Hash(nil) case fails for the reason under
// test rather than for a missing store.
type stubStore struct{ Store }
