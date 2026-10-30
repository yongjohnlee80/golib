package web

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
)

// TestSSO_AParkIsClaimedOnlyByItsSubject: a park is the login's subject's alone. An attach by
// another principal (admitted by another factor while it carried this login's ticket), or one with
// no Identity, claims nothing and leaves the park where it is, so the subject who logged in still
// claims it, and nothing is released.
func TestSSO_AParkIsClaimedOnlyByItsSubject(t *testing.T) {
	t.Parallel()
	s, released, _, _ := ssoFor(t, 4, time.Minute)
	u := &upstream{id: "alice's"}
	if err := s.hold("h1", testSubject, u); err != nil {
		t.Fatal(err)
	}
	for _, info := range []*SessionInfo{
		{Handoff: "h1", Identity: &auth.Identity{Subject: "bob"}},
		{Handoff: "h1", Identity: &auth.Identity{}},
		{Handoff: "h1"},
	} {
		if got, ok := s.Claim(info); ok {
			t.Errorf("an attach as %v claimed %v, parked for %q", info.Identity, got, testSubject)
		}
	}
	if len(*released) != 0 {
		t.Errorf("a refused claim released the park: %v", *released)
	}
	if got, ok := s.Claim(&SessionInfo{Handoff: "h1", Identity: testIdentity}); !ok || got != u {
		t.Errorf("the login's own subject: Claim = %v %v, want its park", got, ok)
	}
}

// TestSSO_AnotherSubjectIsProvisionedItsOwnState: Session gives an attach that is not the park's
// subject its own provisioned state, never the parked one.
func TestSSO_AnotherSubjectIsProvisionedItsOwnState(t *testing.T) {
	t.Parallel()
	s, err := NewSSO(SSOConfig[*upstream]{
		Max: 4, TTL: time.Minute, Release: func(*upstream, HandoffReason) {},
		Provision: func(_ context.Context, id *auth.Identity) (*upstream, error) {
			return &upstream{id: "provisioned for " + id.Subject}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	parked := &upstream{id: "alice's"}
	if err := s.hold("h1", testSubject, parked); err != nil {
		t.Fatal(err)
	}
	got, release, err := s.Session(context.Background(), &SessionInfo{Handoff: "h1", Identity: &auth.Identity{Subject: "bob"}})
	if err != nil {
		t.Fatal(err)
	}
	release()
	if got == parked || got.id != "provisioned for bob" {
		t.Errorf("bob's attach with alice's handoff got %q, want state provisioned for bob", got.id)
	}
	if v, ok := s.Claim(&SessionInfo{Handoff: "h1", Identity: testIdentity}); !ok || v != parked {
		t.Errorf("alice's park after bob's attach: %v %v, want it still hers", v, ok)
	}
}

// TestSSO_ALoginWithNoSubjectParksNothing: a park needs the subject that may claim it, so a login
// with none parks nothing: the hook refuses, and the stashed value is released, not dropped.
func TestSSO_ALoginWithNoSubjectParksNothing(t *testing.T) {
	t.Parallel()
	for _, id := range []*auth.Identity{nil, {}} {
		s, released, _, _ := ssoFor(t, 4, time.Minute)
		st := &Stash{}
		st.Set(&upstream{id: "stashed"})
		if err := s.parkLogin("h1", id, st); err == nil {
			t.Errorf("a login as %v parked", id)
		}
		if len(*released) != 1 || (*released)[0].reason != LoginFailed {
			t.Errorf("a login as %v: released %v, want the stashed value released as LoginFailed", id, *released)
		}
		if s.Len() != 0 {
			t.Errorf("a login as %v left %d parked", id, s.Len())
		}
	}
}

// TestManager_ASubjectIsCappedWithinTheWhole: with MaxSessionsPerSubject a subject past its cap is
// refused while the Manager's own cap still has room for others; without it, one subject may hold
// every session.
func TestManager_ASubjectIsCappedWithinTheWhole(t *testing.T) {
	t.Parallel()
	m, _ := manager(t, MaxSessions(4), MaxSessionsPerSubject(2))
	for range 2 {
		if _, err := m.Create(context.Background(), identity("alice"), hello()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Create(context.Background(), identity("alice"), hello()); !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("alice's third session: %v, want ErrSessionLimit", err)
	}
	if _, err := m.Create(context.Background(), identity("bob"), hello()); err != nil {
		t.Fatalf("bob, within the Manager's cap: %v", err)
	}
	open, _ := manager(t, MaxSessions(4))
	for i := range 4 {
		if _, err := open.Create(context.Background(), identity("alice"), hello()); err != nil {
			t.Fatalf("no per-subject cap, alice's session %d: %v", i+1, err)
		}
	}
}
