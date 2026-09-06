package token

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
)

func present(tok string) *auth.Request {
	return &auth.Request{Credentials: map[string]auth.Secret{"ticket": auth.NewSecret(tok)}}
}

func setup(t *testing.T) (*MemStore, *Issuer, *Factor) {
	t.Helper()
	s := NewMemStore(0)
	return s, NewIssuer(s), NewFactor(s)
}

func TestIssueVerify_RoundTrip(t *testing.T) {
	t.Parallel()

	_, iss, f := setup(t)
	tok, err := iss.Issue("alice", time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.Verify(context.Background(), present(tok.Reveal()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "alice" {
		t.Errorf("Subject = %q", c.Subject)
	}
	if c.ExpiresAt.IsZero() {
		t.Error("a token must bound the interval it contributes")
	}
	if f.Kind() != auth.FactorIdentity {
		t.Error("token must be identity-bearing")
	}
	// A multi-use token verifies repeatedly.
	if _, err := f.Verify(context.Background(), present(tok.Reveal())); err != nil {
		t.Errorf("second use of a multi-use token failed: %v", err)
	}
}

// The plaintext is never stored: the store holds a hash.
func TestStore_HoldsNoPlaintext(t *testing.T) {
	t.Parallel()

	s, iss, _ := setup(t)
	tok, err := iss.Issue("alice", time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for h, rec := range s.recs {
		if strings.Contains(string(h[:]), tok.Reveal()) {
			t.Error("the store key contains the plaintext")
		}
		if strings.Contains(rec.Subject, tok.Reveal()) {
			t.Error("the record contains the plaintext")
		}
	}
}

func TestVerify_Malformed(t *testing.T) {
	t.Parallel()

	_, _, f := setup(t)
	for name, r := range map[string]*auth.Request{
		"nil request":              nil,
		"no credential":            {},
		"empty":                    present(""),
		"too short":                present("abc"),
		"too long":                 present(strings.Repeat("a", encodedLen+1)),
		"right length, not base64": present(strings.Repeat("!", encodedLen)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.Verify(context.Background(), r); err == nil {
				t.Error("expected a failure")
			}
		})
	}
}

func TestVerify_ExpiryAndRevocation(t *testing.T) {
	t.Parallel()

	t.Run("expired", func(t *testing.T) {
		s := NewMemStore(0)
		now := time.Now()
		clock := func() time.Time { return now }
		iss, f := NewIssuer(s, Clock(clock)), NewFactor(s, Clock(func() time.Time { return now.Add(2 * time.Minute) }))
		tok, err := iss.Issue("alice", time.Minute, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Verify(context.Background(), present(tok.Reveal())); !errors.Is(err, ErrExpired) {
			t.Errorf("err = %v, want ErrExpired", err)
		}
		if s.Len() != 0 {
			t.Error("an expired record should be evicted on access")
		}
	})

	t.Run("revoked", func(t *testing.T) {
		_, iss, f := setup(t)
		tok, err := iss.Issue("alice", time.Minute, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := iss.Revoke(tok.Reveal()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Verify(context.Background(), present(tok.Reveal())); !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}

// Acceptance 6b: exactly one concurrent redeemer of a single-use ticket wins.
// This is the WebTUI attach case — two winners would mean two sessions from one
// credential.
func TestConsume_SingleUseIsAtomicUnderConcurrency(t *testing.T) {
	t.Parallel()

	const redeemers = 64
	for trial := 0; trial < 20; trial++ {
		s, iss, f := setup(t)
		tok, err := iss.Issue("alice", time.Minute, true)
		if err != nil {
			t.Fatal(err)
		}
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			wins    int
			errored int
		)
		start := make(chan struct{})
		for i := 0; i < redeemers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := f.Verify(context.Background(), present(tok.Reveal()))
				mu.Lock()
				if err == nil {
					wins++
				} else {
					errored++
				}
				mu.Unlock()
			}()
		}
		close(start)
		wg.Wait()
		if wins != 1 {
			t.Fatalf("trial %d: %d redeemers succeeded, want exactly 1 (%d failed)", trial, wins, errored)
		}
		if s.Len() != 0 {
			t.Fatalf("trial %d: the consumed ticket is still in the store", trial)
		}
	}
}

// A single-use ticket cannot be redeemed twice, sequentially either.
func TestConsume_SingleUseCannotBeReplayed(t *testing.T) {
	t.Parallel()

	_, iss, f := setup(t)
	tok, err := iss.Issue("alice", time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Verify(context.Background(), present(tok.Reveal())); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Verify(context.Background(), present(tok.Reveal())); !errors.Is(err, ErrNotFound) {
		t.Errorf("replay err = %v, want ErrNotFound", err)
	}
}

// The store is bounded: it fails rather than growing without limit.
func TestMemStore_Bounded(t *testing.T) {
	t.Parallel()

	s := NewMemStore(4)
	iss := NewIssuer(s)
	for i := 0; i < 4; i++ {
		if _, err := iss.Issue("u", time.Minute, false); err != nil {
			t.Fatalf("issue %d: %v", i, err)
		}
	}
	if _, err := iss.Issue("u", time.Minute, false); !errors.Is(err, ErrFull) {
		t.Errorf("err = %v, want ErrFull — the store must not grow past its cap", err)
	}
	if s.Len() != 4 {
		t.Errorf("Len = %d, want 4", s.Len())
	}
}

// Capacity pressure sweeps expired records before refusing.
func TestMemStore_SweepsExpiredUnderPressure(t *testing.T) {
	t.Parallel()

	now := time.Now()
	s := NewMemStore(2)
	iss := NewIssuer(s, Clock(func() time.Time { return now }))
	if _, err := iss.Issue("a", time.Millisecond, false); err != nil {
		t.Fatal(err)
	}
	if _, err := iss.Issue("b", time.Millisecond, false); err != nil {
		t.Fatal(err)
	}
	// Both are expired relative to the real clock used by Put's sweep.
	time.Sleep(5 * time.Millisecond)
	if _, err := iss.Issue("c", time.Minute, false); err != nil {
		t.Errorf("a sweep should have made room: %v", err)
	}
}

// Tokens are unguessable and fixed-length.
func TestIssue_TokenShape(t *testing.T) {
	t.Parallel()

	_, iss, _ := setup(t)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		tok, err := iss.Issue("u", time.Minute, false)
		if err != nil {
			t.Fatal(err)
		}
		s := tok.Reveal()
		if len(s) != encodedLen {
			t.Fatalf("length = %d, want the fixed %d", len(s), encodedLen)
		}
		if seen[s] {
			t.Fatal("duplicate token from the CSPRNG")
		}
		seen[s] = true
	}
}

func TestIssue_Rejects(t *testing.T) {
	t.Parallel()

	_, iss, _ := setup(t)
	if _, err := iss.Issue("", time.Minute, false); !errors.Is(err, ErrMalformed) {
		t.Error("an empty subject must be rejected")
	}
	if _, err := iss.Issue("u", 0, false); !errors.Is(err, ErrMalformed) {
		t.Error("a non-positive ttl must be rejected")
	}
}

// The WebTUI policy, built for real.
func TestWebTUIPolicy_TicketBranch(t *testing.T) {
	t.Parallel()

	s := NewMemStore(0)
	iss, f := NewIssuer(s), NewFactor(s)
	p, err := auth.NewPolicy(auth.Any(auth.Leaf(f)))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := iss.Issue("johno", 30*time.Second, true)
	if err != nil {
		t.Fatal(err)
	}
	id, err := p.Authenticate(context.Background(), present(tok.Reveal()))
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if id.Subject != "johno" {
		t.Errorf("Subject = %q", id.Subject)
	}
	// Reconnect requires a FRESH credential: the same ticket must not attach again.
	if _, err := p.Authenticate(context.Background(), present(tok.Reveal())); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("a consumed ticket re-attached: %v", err)
	}
}
