package gui

import (
	"testing"
	"time"
)

// TestAReleasedMeasurerIsReused: a scale keeps its measurers, so a shaper sets its fonts up once,
// not again after every garbage collection as a sync.Pool's did.
func TestAReleasedMeasurerIsReused(t *testing.T) {
	const scale = 3.25 // a scale of its own: no other test shares its pool
	m1, release := AcquireMeasurer(scale)
	release()
	m2, release2 := AcquireMeasurer(scale)
	defer release2()
	if m1.(*measurer) != m2.(*measurer) {
		t.Fatal("the released measurer was not reused: a new shaper was made")
	}
}

// TestAScaleMakesAtMostMeasurerMax: with MaxMeasurers leased, the next acquire waits for a release
// and then takes the released measurer; no more than MaxMeasurers are ever made.
func TestAScaleMakesAtMostMeasurerMax(t *testing.T) {
	const scale = 3.5
	var releases []func()
	seen := map[*measurer]bool{}
	for range MaxMeasurers {
		m, release := AcquireMeasurer(scale)
		seen[m.(*measurer)] = true
		releases = append(releases, release)
	}
	got := make(chan *measurer)
	go func() {
		m, release := AcquireMeasurer(scale)
		got <- m.(*measurer)
		release()
	}()
	select {
	case <-got:
		t.Fatalf("an acquire past MaxMeasurers (%d) did not wait", MaxMeasurers)
	case <-time.After(50 * time.Millisecond):
	}
	releases[0]()
	select {
	case m := <-got:
		if !seen[m] {
			t.Fatal("the waiting acquire made a new measurer instead of taking the released one")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting acquire was not woken by a release")
	}
	for _, r := range releases[1:] {
		r()
	}
	measurers.mu.Lock()
	made := measurers.pools[scale].made
	measurers.mu.Unlock()
	if made != MaxMeasurers {
		t.Fatalf("made %d measurers, want %d", made, MaxMeasurers)
	}
}

// TestASecondReleaseDoesNothing: releasing twice must not put the measurer in the pool twice, or
// two leases would share one shaper.
func TestASecondReleaseDoesNothing(t *testing.T) {
	const scale = 3.75
	_, release := AcquireMeasurer(scale)
	release()
	release()
	measurers.mu.Lock()
	idle := len(measurers.pools[scale].idle)
	measurers.mu.Unlock()
	if idle != 1 {
		t.Fatalf("%d idle measurers after releasing one twice, want 1", idle)
	}
}
