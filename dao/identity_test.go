package dao

import (
	"context"
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// A hook that rewrites SQL on an observe-only event has broken the contract it
// was handed, and the identity must say so.
//
// This site is worth driving rather than reasoning about, because it is the one
// place in the identity sweep where the choice of sentinel asserts WHO WAS
// WRONG: a precondition means the hook author broke a rule, not that the caller
// passed bad input. Getting that backwards would point every reader at the
// wrong person.
func TestHooks_MutatingAnObserveOnlyEventIsAPrecondition(t *testing.T) {
	// rewriteHook rewrites q.SQL and q.Args in BeforeExec, which is legal for a
	// statement op and forbidden for this one.
	p := &pipeline{hooks: []Hook{rewriteHook{}}, ctx: context.Background()}
	p.info.Op = OpBatchCopy

	err := p.beforeExecFrozen("COPY artist FROM STDIN")
	if err == nil {
		t.Fatal("a hook mutated SQL on an observe-only event and the flush was " +
			"allowed to continue")
	}
	if !errors.Is(err, errs.ErrPrecondition) {
		t.Errorf("must satisfy ErrPrecondition — the HOOK broke the contract; got %v", err)
	}
	if errors.Is(err, errs.ErrInvalidArgument) {
		t.Error("must NOT satisfy ErrInvalidArgument: the caller's arguments were " +
			"fine, and blaming them would send a reader to the wrong code")
	}

	// A hook that leaves the statement alone must not trip the guard, or the
	// assertions above would pass for a pipeline that rejects every hook.
	clean := &pipeline{hooks: []Hook{NopHook{}}, ctx: context.Background()}
	clean.info.Op = OpBatchCopy
	if err := clean.beforeExecFrozen("COPY artist FROM STDIN"); err != nil {
		t.Errorf("an observing hook that mutates nothing must be allowed: %v", err)
	}
}
