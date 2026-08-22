package web_test

import (
	"context"
	"fmt"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/tui/web"
)

// upstreamSession stands in for whatever a consumer allocates per login — an RPC
// connection to a daemon, a database session, a remote API client. The thing that
// matters is that it needs releasing, and that closing it is not the same as
// revoking the credential it holds.
type upstreamSession struct{ user string }

func (u *upstreamSession) Logout() { fmt.Printf("logout %s\n", u.user) }
func (u *upstreamSession) Close()  { fmt.Printf("close %s\n", u.user) }

// loginFactor authenticates against the upstream service rather than a local
// store, which is the case web.SSO exists for.
type loginFactor struct{ sso *web.SSO[*upstreamSession] }

func (loginFactor) Kind() auth.FactorKind { return auth.FactorIdentity }

// Claim names the principal before verifying, so auth.Throttle can key per-user
// backoff. Required: NewThrottle refuses a factor without it.
func (loginFactor) Claim(r *auth.Request) string {
	return r.Credentials["subject"].Reveal()
}

func (f loginFactor) Verify(ctx context.Context, r *auth.Request) (auth.Contribution, error) {
	user := r.Credentials["subject"].Reveal()

	// The upstream decides. This factor holds no password store: it forwards the
	// credential once and carries the answer, so there is one source of truth
	// about who a user is.
	up := &upstreamSession{user: user}

	// Verify is the only place holding the credential, so it is the only place
	// that can allocate. Stash moves the result into the request's slot; the SSO
	// helper moves it into the park when the ticket is minted.
	if err := f.sso.Stash(ctx, up); err != nil {
		up.Close()
		return auth.Contribution{}, err
	}
	return auth.Contribution{Method: "upstream", Subject: user, IssuedAt: time.Now()}, nil
}

// Example_singleSignOn shows the whole login-handoff workflow.
//
// The shape to copy is that a consumer writes exactly two things — how to
// allocate (in Verify) and how to release (in SSOConfig.Release) — and the helper
// owns every path in between: parking on login, claiming on create, releasing on
// reattach or a failed attach, and sweeping abandoned logins.
func Example_singleSignOn() {
	sso, err := web.NewSSO(web.SSOConfig[*upstreamSession]{
		Max: 8,
		TTL: 30 * time.Second,

		// REQUIRED, and the reason this type is worth using: every path that
		// discards a parked value goes through here, so "allocated and never
		// cleaned up" is not a state the workflow can reach.
		//
		// Revoke BEFORE closing. Closing a transport does not usually revoke the
		// credential it carried, so a close-only teardown leaves a live session
		// upstream.
		Release: func(u *upstreamSession, r web.HandoffReason) {
			fmt.Printf("releasing %s: %v\n", u.user, r)
			u.Logout()
			u.Close()
		},
	})
	if err != nil {
		panic(err)
	}
	defer sso.Close()

	// Both hooks arrive together, so the login side cannot be wired without the
	// release side.
	handlerOpt, managerOpt := sso.Options()

	mgr, err := web.NewManager(
		func(b *web.Backend, info *web.SessionInfo) web.Runner {
			// Claim takes the state parked by the login that created THIS session.
			// ok is false for an attach that carried no login — an mTLS or SSH
			// attach parks nothing — which is a normal case, not an error.
			up, ok := sso.Claim(info)
			if !ok {
				return newGuestApp(b, info.Identity)
			}
			return newUserApp(b, info.Identity, up)
		},
		managerOpt,
		web.MaxSessions(8),
	)
	if err != nil {
		panic(err)
	}
	_ = mgr

	// The login policy: the upstream-backed factor, throttled. A password reaching
	// a shell-adjacent tool over a network must have backoff, and NewThrottle
	// refuses to build without a Tracker.
	tracker, err := auth.NewMemTracker(64, auth.DefaultBackoff())
	if err != nil {
		panic(err)
	}
	loginPolicy, err := web.PasswordPolicyExample(
		loginFactor{sso: sso}, tracker, ipAllowlist(),
	)
	if err != nil {
		panic(err)
	}
	_ = loginPolicy
	_ = handlerOpt

	// web.NewHandler(cfg, mgr, handlerOpt, ...) then wires the login route, and
	// Serve builds the listener from the validated config.
	fmt.Println("wired")
	// Output: wired
}

func newGuestApp(*web.Backend, *auth.Identity) web.Runner                  { return nil }
func newUserApp(*web.Backend, *auth.Identity, *upstreamSession) web.Runner { return nil }

// ipAllowlist stands in for a contextual factor. PasswordPolicyExample requires
// one: a contextual factor cannot satisfy a policy alone, so it narrows who may
// attempt without adding a way in.
func ipAllowlist() auth.Factor { return contextualStub{} }

type contextualStub struct{}

func (contextualStub) Kind() auth.FactorKind { return auth.FactorContextual }
func (contextualStub) Verify(context.Context, *auth.Request) (auth.Contribution, error) {
	return auth.Contribution{Method: "ipallow"}, nil
}
