package postgres

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// LastInsertId is not a thing this driver can do, and "cannot" is the shared
// UNSUPPORTED condition rather than a failure. A caller choosing between
// LastInsertId and a RETURNING clause needs to recognise that by identity.
func TestLastInsertId_IsUnsupportedNotAFailure(t *testing.T) {
	_, err := pgxResult{}.LastInsertId()
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("must satisfy ErrUnsupported, got %v", err)
	}
	if errors.Is(err, errs.ErrInvalidArgument) {
		t.Error("must NOT satisfy ErrInvalidArgument: the caller did nothing " +
			"wrong, the driver simply cannot answer")
	}
}
