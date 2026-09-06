package dao_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

// This file lives OUTSIDE package dao on purpose: it must see the sentinel the
// way a consumer sees it, not the way the package declares it.

// The published text must not move. Normally a message may be reworded freely,
// because callers compare identity — but this is the change that GIVES them the
// cross-package identity, so it cannot also assume they already had it.
func TestErrUnsupported_TextIsUnchanged(t *testing.T) {
	const published = "dao: operation not supported by this dialect"
	if got := dao.ErrUnsupported.Error(); got != published {
		t.Errorf("the published sentinel text changed:\n got  %q\n want %q", got, published)
	}
	// And through the wrap shape the package's own call sites use.
	wrapped := fmt.Errorf("%w: COPY", dao.ErrUnsupported)
	if got, want := wrapped.Error(), published+": COPY"; got != want {
		t.Errorf("a wrapped site's text changed:\n got  %q\n want %q", got, want)
	}
}

// The point of the change: an error raised by dao now answers BOTH the
// package's question and the workspace-wide one.
func TestErrUnsupported_AnswersBothQuestions(t *testing.T) {
	// Wrapped the way dao's existing call sites wrap it — code written before
	// this change and not touched by it.
	err := fmt.Errorf("bigquery: %w: interactive transactions", dao.ErrUnsupported)

	if !errors.Is(err, dao.ErrUnsupported) {
		t.Error("must still satisfy dao.ErrUnsupported — every existing consumer " +
			"asks this question and none of them may break")
	}
	if !errors.Is(err, errs.ErrUnsupported) {
		t.Error("must now ALSO satisfy errs.ErrUnsupported; that is the whole " +
			"point of the change")
	}
}

// The direction that must NOT hold, and the reason an assignment alias was
// rejected.
//
// If dao.ErrUnsupported were assigned errs.ErrUnsupported's value, the two
// would be the SAME error, and any unrelated package's unsupported condition
// would answer dao's question. A caller asking "is this a dialect limitation?"
// would get yes for a terminal that cannot do colour.
func TestErrUnsupported_DoesNotOverMatchOtherPackages(t *testing.T) {
	elsewhere := fmt.Errorf("tui: truecolour %w", errs.ErrUnsupported)

	if !errors.Is(elsewhere, errs.ErrUnsupported) {
		t.Fatal("the fixture is wrong: it must carry the shared condition")
	}
	if errors.Is(elsewhere, dao.ErrUnsupported) {
		t.Error("another package's unsupported error must NOT answer dao's " +
			"question; the relation is a wrap, not an alias, and this is the " +
			"assertion that tells the two apart")
	}
}

// A layered sentinel is still a plain comparable value, so the ordinary
// equality a caller might reach for keeps working.
func TestErrUnsupported_IsItselfInBothDirections(t *testing.T) {
	if !errors.Is(dao.ErrUnsupported, dao.ErrUnsupported) {
		t.Error("the sentinel must satisfy itself")
	}
	if !errors.Is(dao.ErrUnsupported, errs.ErrUnsupported) {
		t.Error("the bare sentinel, unwrapped by any call site, must already " +
			"carry the shared identity")
	}
}
