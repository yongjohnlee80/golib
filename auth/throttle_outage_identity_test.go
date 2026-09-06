package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

var (
	errTrackerA = errors.New("tracker: shard a is down")
	errTrackerB = errors.New("tracker: shard b is down")
)

// perKeyFailTracker fails a different way for each key, so a test can tell
// whether BOTH causes survived or only the rendering of them.
type perKeyFailTracker struct{}

func (perKeyFailTracker) Locked(_ context.Context, key string, _ time.Time) (bool, time.Duration, error) {
	switch key {
	case "a":
		return false, 0, errTrackerA
	case "b":
		return false, 0, errTrackerB
	}
	return false, 0, nil
}

func (perKeyFailTracker) Fail(_ context.Context, _ string, _ time.Time) (time.Duration, error) {
	return 0, nil
}
func (perKeyFailTracker) Reset(_ context.Context, _ string) error { return nil }

// A tracker outage that spans several keys must keep EVERY cause reachable.
//
// The failures are collected with errors.Join, which is the legitimate use of
// it — they are independent and an operator needs all of them. Rendering that
// joined value with %v destroyed all of them at once: one verb eating N
// identities, while ErrTrackerUnavailable kept answering so the error looked
// complete.
func TestLockedAny_OutageKeepsEveryCause(t *testing.T) {
	th := &Throttle{tracker: perKeyFailTracker{}, failOpen: false}

	_, err := th.lockedAny(context.Background(), []string{"a", "b"}, time.Now())
	if err == nil {
		t.Fatal("a fail-closed throttle must deny when the tracker cannot answer")
	}
	if !errors.Is(err, ErrTrackerUnavailable) {
		t.Errorf("must still satisfy ErrTrackerUnavailable; got %v", err)
	}
	if !errors.Is(err, errTrackerA) {
		t.Error("the first key's cause must survive the wrap")
	}
	if !errors.Is(err, errTrackerB) {
		t.Error("the second key's cause must survive too — a joined outage loses " +
			"ALL of its causes to a single rendering verb, not just the last")
	}
}
