package sshkey

import (
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"
)

// clockAt is a settable clock for a Challenger.
type clockAt struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clockAt) get() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clockAt) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// TestChallenger_IssueFromBoundsEachAddress: IssueFrom gives one address at most
// ChallengesPerAddress unexpired challenges, an IPv6 address counted by its /64, and refuses the
// next before the store is asked; other addresses are untouched, a challenge counts until it
// expires, and Issue (no address) counts nothing.
func TestChallenger_IssueFromBoundsEachAddress(t *testing.T) {
	t.Parallel()
	clock := &clockAt{now: time.Unix(1_700_000_000, 0)}
	store := NewMemStore(100)
	c := NewChallenger(store, time.Minute, Clock(clock.get), ChallengesPerAddress(2))
	a, b := netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")
	v6a, v6b := netip.MustParseAddr("2001:db8:1:2::5"), netip.MustParseAddr("2001:db8:1:2:ffff::9")
	other64 := netip.MustParseAddr("2001:db8:1:3::5")
	for _, peer := range []netip.Addr{a, a, v6a, v6b} {
		if _, err := c.IssueFrom(peer, Binding{}); err != nil {
			t.Fatalf("%v, within its bound: %v", peer, err)
		}
	}
	before := store.Len()
	for _, peer := range []netip.Addr{a, v6a, netip.MustParseAddr("2001:db8:1:2::77")} {
		if _, err := c.IssueFrom(peer, Binding{}); !errors.Is(err, ErrTooManyChallenges) {
			t.Errorf("%v past its bound: %v, want ErrTooManyChallenges", peer, err)
		}
	}
	if store.Len() != before {
		t.Errorf("refused issues reached the store: %d challenges, want %d", store.Len(), before)
	}
	for _, peer := range []netip.Addr{b, other64} {
		if _, err := c.IssueFrom(peer, Binding{}); err != nil {
			t.Errorf("%v, another address: %v", peer, err)
		}
	}
	for range 5 {
		if _, err := c.Issue(Binding{}); err != nil {
			t.Fatalf("Issue, which counts no address: %v", err)
		}
	}
	clock.add(time.Minute) // a's challenges have expired
	if _, err := c.IssueFrom(a, Binding{}); err != nil {
		t.Errorf("a, once its challenges expired: %v", err)
	}
}

// TestChallenger_AStoreRefusalIsNotCounted: an issue the store refuses was not issued, so it does
// not count against the address.
func TestChallenger_AStoreRefusalIsNotCounted(t *testing.T) {
	t.Parallel()
	clock := &clockAt{now: time.Unix(1_700_000_000, 0)}
	store := NewMemStore(1)
	c := NewChallenger(store, time.Minute, Clock(clock.get), ChallengesPerAddress(1))
	first, err := c.IssueFrom(netip.MustParseAddr("192.0.2.1"), Binding{})
	if err != nil {
		t.Fatal(err)
	}
	b := netip.MustParseAddr("192.0.2.2")
	if _, err := c.IssueFrom(b, Binding{}); !errors.Is(err, ErrFull) {
		t.Fatalf("b, the store full: %v, want ErrFull", err)
	}
	if _, err := store.Consume(first.ID, clock.get()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.IssueFrom(b, Binding{}); err != nil {
		t.Errorf("b, once the store had room: %v (a refused issue was counted against it)", err)
	}
}

// TestMemStore_SweepsAtTheIssuersClock: a full MemStore drops what has expired by the new record's
// issue time, the Challenger's clock, not the wall clock.
func TestMemStore_SweepsAtTheIssuersClock(t *testing.T) {
	t.Parallel()
	// an hour ahead of the wall clock, by which the record has not expired: only the issuer's
	// clock says it has
	clock := &clockAt{now: time.Now().Add(time.Hour)}
	c := NewChallenger(NewMemStore(1), time.Minute, Clock(clock.get))
	if _, err := c.Issue(Binding{}); err != nil {
		t.Fatal(err)
	}
	clock.add(2 * time.Minute)
	if _, err := c.Issue(Binding{}); err != nil {
		t.Errorf("past the first challenge's expiry on the Challenger's clock: %v", err)
	}
}
