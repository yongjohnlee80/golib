package httpserver_test

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
	httpserver "github.com/yongjohnlee80/golib/server/http"
)

// Calling Serve before Listen is a broken precondition. A caller must be able
// to tell it apart from a serving failure without reading the sentence.
func TestServe_BeforeListenIsAPrecondition(t *testing.T) {
	s := httpserver.New()

	err := s.Serve()
	if err == nil {
		t.Fatal("want an error when Serve is called before Listen, got nil")
	}
	if !errors.Is(err, errs.ErrPrecondition) {
		t.Errorf("must satisfy ErrPrecondition, got %v", err)
	}
}
