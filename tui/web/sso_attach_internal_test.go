package web

import (
	"runtime"
	"testing"

	"github.com/yongjohnlee80/golib/auth"
)

// An SSO is whole when it attaches to its Admission: a Handler already serving logins (an SSO may
// attach after the Manager and Handler are built) can reach the park the moment it is attached,
// and parking reads the SSO's clock and Admission. Attached before they were set, a login in that
// window met a nil clock (a panic) and no Admission (a slot unaccounted), a race -race reports.
func TestAnSSOIsWholeWhenItAttaches(t *testing.T) {
	adm := NewAdmission()
	// the login took its keyed slot before the SSO existed, as the Handler does before parking
	if !adm.gate.hold("handoff-1", adm.deadline()) {
		t.Fatal("no slot for the login")
	}
	parked := make(chan error, 1)
	go func() {
		p := adm.attached()
		for p == nil {
			runtime.Gosched()
			p = adm.attached()
		}
		st := &Stash{}
		st.Set("upstream")
		parked <- p.parkLogin("handoff-1", &auth.Identity{Subject: "u"}, st)
	}()
	s, err := NewSSO(SSOConfig[string]{Admission: adm, Release: func(string, HandoffReason) {}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := <-parked; err != nil {
		t.Fatalf("a login parked the moment the SSO attached: %v", err)
	}
}
