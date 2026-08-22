package web

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/ipallow"
	"github.com/yongjohnlee80/golib/auth/mtls"
	"github.com/yongjohnlee80/golib/auth/password"
	"github.com/yongjohnlee80/golib/auth/sshkey"
	"github.com/yongjohnlee80/golib/auth/token"
	"github.com/yongjohnlee80/golib/logger"
)

// This file answers one question end to end: does web.SSO serve EVERY mechanism
// golib/auth implements, through the real serve path, with one allocation route
// and one release?
//
// The mechanisms split in two, and the split is the whole design:
//
//   - auth/password authenticates at a LOGIN, so it can park state while it still
//     holds the credential. SSO claims that state.
//   - auth/token (an SSH-minted or otherwise out-of-band ticket), auth/mtls and
//     auth/sshkey authenticate at the ATTACH itself. No login ran, nothing was
//     parked, so SSO provisions.
//   - auth/ipallow is contextual: it narrows every one of the above and admits
//     none of them on its own.
//
// A consumer writes one build function and gets all four.

// e2eUpstream records where its state came from, so a test can tell a claim from
// a provision rather than merely observing that something arrived.
type e2eUpstream struct {
	subject string
	origin  string // "login" or "provision"
}

type ssoRig struct {
	h        *Handler
	m        *Manager
	sso      *SSO[*e2eUpstream]
	store    *token.MemStore
	chal     *sshkey.Challenger
	signer   ssh.Signer
	leafCert *x509.Certificate
	// builds carries each App construction to the TEST goroutine. The ready frame
	// does not order the runner: the session is registered, attached and answered
	// before the runner goroutine is scheduled, so asserting on `built` right after
	// a ready frame read zero about one run in a hundred (lector r1 on PR #14).
	builds chan *e2eUpstream
	mu     sync.Mutex
	built  []*e2eUpstream
	// factoryErrs are complaints from inside the runner goroutine, reported on the
	// test goroutine instead: t.Error from a goroutine the test does not join can
	// land after the test has finished.
	factoryErrs []string
	released    []*e2eUpstream
}

func (r *ssoRig) record(u *e2eUpstream) {
	r.mu.Lock()
	r.built = append(r.built, u)
	r.mu.Unlock()
	r.builds <- u
}

func (r *ssoRig) complain(format string, args ...any) {
	r.mu.Lock()
	r.factoryErrs = append(r.factoryErrs, fmt.Sprintf(format, args...))
	r.mu.Unlock()
}

// waitBuild blocks until an App has been built and returns its upstream state.
func (r *ssoRig) waitBuild(t *testing.T) *e2eUpstream {
	t.Helper()
	select {
	case u := <-r.builds:
		return u
	case <-time.After(5 * time.Second):
		t.Fatal("no App was built for an authenticated attach")
		return nil
	}
}

// check reports anything the factory complained about. Called at the end of every
// test, on the test's own goroutine.
func (r *ssoRig) check(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.factoryErrs {
		t.Error(e)
	}
}

func (r *ssoRig) sawBuilt() []*e2eUpstream {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*e2eUpstream(nil), r.built...)
}

func (r *ssoRig) sawReleased() []*e2eUpstream {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*e2eUpstream(nil), r.released...)
}

// e2eLoginFactor is auth/password's shape: it claims a subject before verifying,
// and allocates upstream state in Verify — the only place holding the credential.
type e2eLoginFactor struct{ rig *ssoRig }

func (e2eLoginFactor) Kind() auth.FactorKind { return auth.FactorIdentity }
func (e2eLoginFactor) Claim(r *auth.Request) string {
	if r == nil {
		return ""
	}
	return r.Credentials["subject"].Reveal()
}

func (f e2eLoginFactor) Verify(ctx context.Context, r *auth.Request) (auth.Contribution, error) {
	subject := r.Credentials["subject"].Reveal()
	if subject != "alice" || r.Credentials["password"].Reveal() != "pw" {
		return auth.Contribution{}, auth.Reason("test: refused")
	}
	up := &e2eUpstream{subject: subject, origin: "login"}
	// The one line a consumer writes on the login side.
	if err := f.rig.sso.Stash(ctx, up); err != nil {
		return auth.Contribution{}, auth.Reason("test: stash refused")
	}
	return auth.Contribution{Method: "password", Subject: subject, IssuedAt: time.Now()}, nil
}

// newSSORig builds one server whose attach policy accepts a ticket, an mTLS
// chain OR an SSHSIG challenge, narrowed by an IP allowlist, with a password
// login alongside — i.e. every mechanism at once, sharing a single SSO.
// rigOption tweaks the rig for a single test.
type rigOption func(*rigConfig)

type rigConfig struct {
	loginFactor  auth.Factor
	noProvision  bool
	brokenIssuer bool
	panicApp     bool
	lateRefusal  bool
	holdBuild    chan struct{}
}

func noProvision() rigOption { return func(c *rigConfig) { c.noProvision = true } }

func newSSORig(t *testing.T) *ssoRig {
	t.Helper()
	return newSSORigWith(t, nil)
}

// newSSORigWith builds the rig with a specific login factor. A nil factor uses
// the custom stashing one; the stock auth/password factor goes through the same
// wiring, which is the point of having the seam.
func newSSORigWith(t *testing.T, loginFactor auth.Factor, opts ...rigOption) *ssoRig {
	t.Helper()
	cfg := rigConfig{loginFactor: loginFactor}
	for _, o := range opts {
		o(&cfg)
	}
	rig := &ssoRig{store: token.NewMemStore(64), builds: make(chan *e2eUpstream, 8)}
	t.Cleanup(func() { rig.check(t) })

	ssoCfg := SSOConfig[*e2eUpstream]{
		Max: 8, TTL: time.Minute,
		Provision: func(_ context.Context, id *auth.Identity) (*e2eUpstream, error) {
			u := &e2eUpstream{subject: id.Subject, origin: "provision"}
			return u, nil
		},
		Release: func(u *e2eUpstream, _ HandoffReason) {
			rig.mu.Lock()
			rig.released = append(rig.released, u)
			rig.mu.Unlock()
		},
	}
	if cfg.noProvision {
		ssoCfg.Provision = nil
	}
	sso, err := NewSSO(ssoCfg)
	if err != nil {
		t.Fatal(err)
	}
	rig.sso = sso
	hOpt, mOpt := sso.Options()

	// --- the attach mechanisms, all real ------------------------------------
	leaf := &x509.Certificate{
		Subject:     pkix.Name{CommonName: "mtls-user"},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	rig.signer = signer
	keyStore := sshkey.NewMemStore(0)
	verifier, err := sshkey.NewPureGo([]sshkey.Allowed{{Key: signer.PublicKey(), Subject: "ssh-user"}})
	if err != nil {
		t.Fatal(err)
	}
	rig.chal = sshkey.NewChallenger(keyStore, time.Minute)

	prefix, err := netip.ParsePrefix("203.0.113.0/24")
	if err != nil {
		t.Fatal(err)
	}
	allow := ipallow.New([]netip.Prefix{prefix})
	attach, err := auth.NewPolicy(
		auth.All(
			auth.Any(
				auth.Leaf(token.NewFactor(rig.store)),
				auth.Leaf(mtls.New(func(c auth.Certificate) (string, error) { return c.CommonName, nil })),
				auth.Leaf(sshkey.New(verifier, keyStore, sshkey.Namespace("webtui.golib.test"))),
			),
			auth.Leaf(allow),
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	tracker, err := auth.NewMemTracker(64, auth.Backoff{
		Threshold: 100, Base: time.Second, Max: time.Second, Forget: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	login := cfg.loginFactor
	if login == nil {
		login = e2eLoginFactor{rig: rig}
	}
	loginPolicy, err := PasswordPolicyExample(login, tracker, allow)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.lateRefusal {
		// The identity factor first, the refusing constraint second: the order in
		// which allocation happens before the policy says no.
		loginPolicy, err = auth.NewPolicy(auth.All(
			auth.Leaf(login),
			auth.Leaf(contextualFactor{allow: false}),
		))
		if err != nil {
			t.Fatal(err)
		}
	}

	// --- the consumer's whole app wiring: one build function ----------------
	build := func(_ *Backend, id *auth.Identity, up *e2eUpstream) Runner {
		if up == nil {
			rig.complain("the build function received no upstream state")
			return newFakeApp()
		}
		if id == nil || id.Subject != up.subject {
			rig.complain("identity %v does not match the upstream state for %q", id, up.subject)
		}
		rig.record(up)
		if cfg.panicApp {
			return runnerFunc(func(context.Context) error { panic("the app exploded") })
		}
		return newFakeApp()
	}
	factory := sso.Factory(build)
	if cfg.holdBuild != nil {
		// A hand-written Runner that calls SSO.Session itself — the form the doc
		// describes for a consumer writing their own — so the test controls WHEN the
		// claim happens. Gating sso.settle instead would have been a test of the
		// gate stub rather than of the code.
		factory = func(b *Backend, info *SessionInfo) Runner {
			return runnerFunc(func(ctx context.Context) error {
				<-cfg.holdBuild
				up, release, err := sso.Session(ctx, info)
				if err != nil {
					return err
				}
				defer release()
				app := build(b, info.Identity, up)
				return app.Run(ctx)
			})
		}
	}
	m, err := NewManager(factory, mOpt)
	if err != nil {
		t.Fatal(err)
	}
	rig.m = m
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	})

	issuer := token.NewIssuer(rig.store)
	if cfg.brokenIssuer {
		issuer = token.NewIssuer(failingStore{MemStore: rig.store})
	}
	h, err := NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: attach, LoginPolicy: loginPolicy,
		Issuer:         issuer,
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}, m, HandlerLogger(nopLogger{}), hOpt)
	if err != nil {
		t.Fatal(err)
	}
	rig.h = h
	rig.leafCert = leaf
	return rig
}

// attach drives one real connection through the serve path and returns its
// session id.
func (r *ssoRig) attach(t *testing.T, msg clientMessage, info requestInfo) (string, context.CancelFunc) {
	t.Helper()
	c := &fakeConn{block: make(chan struct{})}
	c.push(msg)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = r.h.loop.serve(ctx, c, info) }()
	return waitForSession(t, c), cancel
}

// --- the four mechanisms ----------------------------------------------------

// auth/password: the LOGIN route parks, and the attach CLAIMS.
func TestSSO_EndToEnd_PasswordLoginClaims(t *testing.T) {
	t.Parallel()
	rig := newSSORig(t)

	rec := httptest.NewRecorder()
	rig.h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	var out loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if rig.sso.Len() != 1 {
		t.Fatalf("%d parked after a login, want 1", rig.sso.Len())
	}

	msg := helloMsg()
	msg.Ticket = out.Ticket
	_, cancel := rig.attach(t, msg, rig.plainReq())
	defer cancel()

	up := rig.waitBuild(t)
	if up.origin != "login" {
		t.Errorf("the app got %s state — a login that stashed must be CLAIMED, not "+
			"re-provisioned, or the login's own allocation leaks", up.origin)
	}
	if rig.sso.Len() != 0 {
		t.Errorf("%d entries still parked after the claim", rig.sso.Len())
	}
}

// auth/token: a ticket minted out of band — the SSH-key handoff of ADR-0001 —
// authenticates at the attach, so nothing was parked and SSO must provision.
func TestSSO_EndToEnd_TicketProvisions(t *testing.T) {
	t.Parallel()
	rig := newSSORig(t)

	secret, err := token.NewIssuer(rig.store).Issue("ticket-user", time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	msg := helloMsg()
	msg.Ticket = secret.Reveal()
	_, cancel := rig.attach(t, msg, rig.plainReq())
	defer cancel()

	up := rig.waitBuild(t)
	if up.origin != "provision" || up.subject != "ticket-user" {
		t.Errorf("app got %+v, want provisioned state for ticket-user", up)
	}
}

// auth/mtls: a verified chain, no ticket at all.
func TestSSO_EndToEnd_MTLSProvisions(t *testing.T) {
	t.Parallel()
	rig := newSSORig(t)

	_, cancel := rig.attach(t, helloMsg(), rig.tlsReq())
	defer cancel()

	up := rig.waitBuild(t)
	if up.origin != "provision" || up.subject != "mtls-user" {
		t.Errorf("app got %+v, want provisioned state for mtls-user", up)
	}
}

// auth/sshkey: a REAL SSHSIG signature over a real challenge, verified by the
// pure-Go verifier. This is the mechanism least like the others — it needs a
// challenge round trip — and it still reaches the App the same way.
func TestSSO_EndToEnd_SSHSIGProvisions(t *testing.T) {
	t.Parallel()
	rig := newSSORig(t)

	chal, err := rig.chal.Issue(sshkey.Binding{Origin: "https://tui.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	msg := helloMsg()
	msg.Identity = "ssh-user"
	msg.Chal = chal.ID
	msg.Sig = string(sshsigArmor(t, rig.signer, "webtui.golib.test", chal.Message))

	_, cancel := rig.attach(t, msg, rig.plainReq())
	defer cancel()

	up := rig.waitBuild(t)
	if up.origin != "provision" || up.subject != "ssh-user" {
		t.Errorf("app got %+v, want provisioned state for ssh-user", up)
	}
}

// auth/ipallow narrows all of them: the same ticket that worked above is refused
// from an address outside the allowlist, and NO upstream state is allocated for
// a connection that never authenticated.
func TestSSO_EndToEnd_ContextualFactorRefusesBeforeAllocation(t *testing.T) {
	t.Parallel()
	rig := newSSORig(t)

	secret, err := token.NewIssuer(rig.store).Issue("ticket-user", time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	msg := helloMsg()
	msg.Ticket = secret.Reveal()

	c := &fakeConn{block: make(chan struct{})}
	c.push(msg)
	req := rig.plainReq()
	req.http.RemoteAddr = "198.51.100.9:5555" // outside 203.0.113.0/24
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rig.h.loop.serve(ctx, c, req); err == nil {
		t.Fatal("an attach from outside the allowlist was admitted")
	}
	if n := len(rig.sawBuilt()); n != 0 {
		t.Errorf("%d apps built for a refused attach — upstream state must not be "+
			"allocated before authentication succeeds", n)
	}
}

// And the release: whatever the mechanism, ending the session releases the state
// exactly once.
func TestSSO_EndToEnd_ReleaseOnSessionEnd(t *testing.T) {
	t.Parallel()
	rig := newSSORig(t)

	sid, cancel := rig.attach(t, helloMsg(), rig.tlsReq())
	cancel()

	// Dropping the connection must NOT release: the session is detached and
	// resumable, and its upstream state has to survive for the reattach.
	s, ok := rig.m.Get(sid)
	if !ok {
		t.Fatal("the session vanished with its connection")
	}
	if n := len(rig.sawReleased()); n != 0 {
		t.Fatalf("%d releases on a mere disconnect — a resumable session lost its state", n)
	}

	// Ending the session does release it.
	rig.m.Close(s.ID())
	waitFor(t, func() bool { return len(rig.sawReleased()) == 1 },
		"the upstream state to be released when the session ended")
	if got := rig.sawReleased()[0]; got.subject != "mtls-user" {
		t.Errorf("released %+v", got)
	}
}

// --- request shapes ---------------------------------------------------------

func (r *ssoRig) plainReq() requestInfo {
	req := httptest.NewRequest(http.MethodGet, "/attach", nil)
	req.RemoteAddr = "203.0.113.7:44321"
	req.Host = "tui.example.test"
	req.Header.Set("Origin", "https://tui.example.test")
	return requestInfo{http: req}
}

func (r *ssoRig) tlsReq() requestInfo {
	info := r.plainReq()
	info.http.TLS = &tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{r.leafCert}},
	}
	return info
}

// sshsigArmor builds the armored SSHSIG blob `ssh-keygen -Y sign` produces.
//
// It mirrors the wire layout that auth/sshkey parses. The mirror is safe to keep
// here because the test below feeds it to the REAL verifier: if either side of
// the format drifts, this goes red rather than quietly stale. auth/sshkey's own
// interop test is what pins the format to OpenSSH itself.
func sshsigArmor(t *testing.T, s ssh.Signer, namespace string, message []byte) []byte {
	t.Helper()
	const magic = "SSHSIG"
	digest := sha512.Sum512(message)
	toSign := append([]byte(magic), ssh.Marshal(struct {
		Namespace string
		Reserved  string
		HashAlgo  string
		Hash      []byte
	}{namespace, "", "sha512", digest[:]})...)
	sig, err := s.Sign(rand.Reader, toSign)
	if err != nil {
		t.Fatal(err)
	}
	body := append([]byte(magic), ssh.Marshal(struct {
		Version   uint32
		PublicKey []byte
		Namespace string
		Reserved  string
		HashAlgo  string
		Signature []byte
	}{
		1, s.PublicKey().Marshal(), namespace, "", "sha512",
		ssh.Marshal(struct {
			Format string
			Blob   []byte
		}{sig.Format, sig.Blob}),
	})...)
	return pem.EncodeToMemory(&pem.Block{Type: "SSH SIGNATURE", Bytes: body})
}

// The STOCK auth/password factor, not a shape-compatible stand-in.
//
// This is the mechanism the ADR's table names, and it is the one the helper got
// wrong: password.Factor verifies a hash and knows nothing about this package, so
// it cannot call SSO.Stash. Options treated an empty stash as a fatal login error
// and the shipped password mechanism returned 503 through the helper that claimed
// to support every mechanism (lector r1 on PR #14).
//
// A login through it now succeeds, parks nothing, and the attach provisions.
func TestSSO_EndToEnd_StockPasswordFactorProvisions(t *testing.T) {
	t.Parallel()
	rig := newSSORigWith(t, stockPasswordFactor(t))

	rec := httptest.NewRecorder()
	rig.h.ServeLogin(rec, loginPost(`{"subject":"pw-user","password":"correct horse"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("a login through the stock auth/password factor: %d %s — the helper "+
			"must accept the mechanism it says it supports", rec.Code, rec.Body.String())
	}
	var out loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if n := rig.sso.Len(); n != 0 {
		t.Errorf("%d entries parked by a factor that cannot stash", n)
	}

	msg := helloMsg()
	msg.Ticket = out.Ticket
	_, cancel := rig.attach(t, msg, rig.plainReq())
	defer cancel()

	up := rig.waitBuild(t)
	if up.origin != "provision" || up.subject != "pw-user" {
		t.Errorf("app got %+v, want provisioned state for pw-user", up)
	}

	// And a wrong password is still refused: provisioning on an empty stash must
	// not have become a way in.
	rec = httptest.NewRecorder()
	rig.h.ServeLogin(rec, loginPost(`{"subject":"pw-user","password":"wrong"}`))
	if rec.Code == http.StatusOK {
		t.Error("a wrong password was accepted")
	}
}

// Without a Provision, a login that stashes nothing must fail at the LOGIN rather
// than mint a ticket guaranteed to fail at the attach.
func TestSSO_EndToEnd_StockPasswordRefusedWithoutProvision(t *testing.T) {
	t.Parallel()
	rig := newSSORigWith(t, stockPasswordFactor(t), noProvision())

	rec := httptest.NewRecorder()
	rig.h.ServeLogin(rec, loginPost(`{"subject":"pw-user","password":"correct horse"}`))
	if rec.Code == http.StatusOK {
		t.Error("a ticket was minted for a session that could never get upstream state")
	}
	if n := rig.sso.Len(); n != 0 {
		t.Errorf("%d entries parked", n)
	}
}

func stockPasswordFactor(t *testing.T) auth.Factor {
	t.Helper()
	// Interactive parameters: Default's 64 MiB per attempt makes a test suite that
	// logs in a few times noticeably slow, and this is not testing the KDF.
	hasher := password.Interactive()
	store := password.NewMemStore()
	if err := store.Add("pw-user", "correct horse", hasher); err != nil {
		t.Fatal(err)
	}
	f, err := password.New(store, password.Hash(hasher))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// --- lector r1 on PR #14: the lifecycle regressions -------------------------

// The admission slot a parked login owns must come BACK.
//
// ServeLogin took a pending-login slot before verifying and simply stopped
// returning it once a handoff was parked. Nothing else knew about the gate, so
// every successful login consumed a slot for the life of the process: with the
// default of 8, the ninth login was 503 even though all eight sessions had ended.
// An authenticated denial of service, and the "bounds agree" claim was false.
func TestSSO_EndToEnd_AdmissionSlotsAreReturned(t *testing.T) {
	t.Parallel()
	rig := newSSORig(t)

	// One more cycle than the pending budget: the last one can only succeed if the
	// slots were returned.
	const cycles = DefaultMaxPendingLogins + 1
	for i := range cycles {
		rec := httptest.NewRecorder()
		rig.h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
		if rec.Code != http.StatusOK {
			t.Fatalf("login %d/%d: %d %s — parked logins are not returning their "+
				"admission slots", i+1, cycles, rec.Code, rec.Body.String())
		}
		var out loginResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		msg := helloMsg()
		msg.Ticket = out.Ticket
		sid, cancel := rig.attach(t, msg, rig.plainReq())
		rig.waitBuild(t)
		cancel()
		if s, ok := rig.m.Get(sid); ok {
			rig.m.Close(s.ID())
		}
	}
	if n := rig.h.pending.pending(); n != 0 {
		t.Errorf("%d admission slots still held after %d completed cycles", n, cycles)
	}
}

// A login REFUSED after the identity factor already allocated must release.
//
// Verification is the only place holding the credential, so it is where a
// consumer allocates — and the policy can still refuse afterwards. Any factor
// evaluated after the identity leaf does it, and so does the policy's own
// post-processing (a subject conflict, an already-expired merged validity).
// PasswordPolicyExample happens to put its constraint FIRST, so this composes the
// other order directly, which a consumer is free to do.
func TestSSO_EndToEnd_LaterRefusalReleasesTheStash(t *testing.T) {
	t.Parallel()
	rig := newSSORigWith(t, nil, lateRefusal())

	rec := httptest.NewRecorder()
	rig.h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code == http.StatusOK {
		t.Fatal("a login refused by a later factor succeeded")
	}
	rel := rig.sawReleased()
	if len(rel) != 1 {
		t.Fatalf("%d releases: the state the identity factor allocated was dropped "+
			"when a later factor refused, so its upstream session is never closed", len(rel))
	}
	if rel[0].origin != "login" {
		t.Errorf("released %+v", rel[0])
	}
	if n := rig.h.pending.pending(); n != 0 {
		t.Errorf("%d admission slots held after a failed login", n)
	}
}

// The other order is better still: a constraint evaluated BEFORE the identity
// factor means nothing was allocated to release. Asserted so the two orders are
// not silently the same test.
func TestSSO_EndToEnd_ConstraintFirstAllocatesNothing(t *testing.T) {
	t.Parallel()
	rig := newSSORig(t) // PasswordPolicyExample: All(constraint, Any(identity))

	req := loginPost(`{"subject":"alice","password":"pw"}`)
	req.RemoteAddr = "198.51.100.9:5555" // outside the allowlist
	rec := httptest.NewRecorder()
	rig.h.ServeLogin(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("a login from outside the allowlist succeeded")
	}
	if n := len(rig.sawReleased()); n != 0 {
		t.Errorf("%d releases: the constraint runs first, so the credential is never "+
			"verified and nothing should have been allocated", n)
	}
	if n := rig.h.pending.pending(); n != 0 {
		t.Errorf("%d admission slots held after a failed login", n)
	}
}

// The same, one step later: the credential was right and the TICKET failed.
func TestSSO_EndToEnd_IssuerFailureReleasesTheStash(t *testing.T) {
	t.Parallel()
	rig := newSSORigWith(t, nil, brokenIssuer())

	rec := httptest.NewRecorder()
	rig.h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code == http.StatusOK {
		t.Fatal("a login whose ticket could not be minted returned a ticket")
	}
	if n := len(rig.sawReleased()); n != 1 {
		t.Errorf("%d releases: a failure between allocation and parking loses the "+
			"upstream session", n)
	}
}

// A second stash in one login is refused, and the login fails rather than
// silently orphaning the first value.
func TestSSO_StashRefusesASecondValue(t *testing.T) {
	t.Parallel()
	s, released, _, _ := ssoFor(t, 4, time.Minute)
	slot := &Stash{}
	ctx := withStash(context.Background(), slot)

	first := &upstream{id: "first"}
	if err := s.Stash(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := s.Stash(ctx, &upstream{id: "second"}); err == nil {
		t.Error("a second stash overwrote the first, whose cleanup was already " +
			"registered — the first upstream session becomes unreachable")
	}
	// The first is still the one parked, and discarding releases exactly it.
	slot.discard()
	if len(*released) != 1 || (*released)[0] != first {
		t.Errorf("released %v, want just the first value", *released)
	}
}

// An abandoned login is released BY TIME, with no login and no Sweep call after
// it. The doc promised an internal sweep and the implementation only swept when
// another login arrived, so a lone abandoned login on an idle server stayed
// logged in upstream indefinitely.
func TestSSO_ExpiryIsAutomatic(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	released := 0
	s, err := NewSSO(SSOConfig[*upstream]{
		Max: 4, TTL: 40 * time.Millisecond,
		Release: func(*upstream, HandoffReason) {
			mu.Lock()
			released++
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.hold("abandoned", &upstream{id: "a"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := released
		mu.Unlock()
		if n == 1 {
			if l := s.Len(); l != 0 {
				t.Errorf("released but still parked (%d)", l)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("an abandoned login was never released: nothing but another login or an " +
		"explicit Sweep expires the park")
}

// And Claim refuses an entry past its deadline rather than handing an App state
// the park had already given up on.
func TestSSO_ClaimRefusesAnExpiredEntry(t *testing.T) {
	t.Parallel()
	s, released, _, now := ssoFor(t, 4, time.Minute)
	if err := s.hold("h1", &upstream{id: "a"}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute) // past the TTL, on the test clock

	if _, ok := s.Claim(&SessionInfo{Handoff: "h1"}); ok {
		t.Error("Claim handed over an entry past its TTL, so a TTL shorter than the " +
			"attach ticket's has no effect")
	}
	if len(*released) != 1 {
		t.Errorf("%d releases: a refused expired entry must still be cleaned up", len(*released))
	}
}

// An App panic ends the SESSION, not the process — and the release still runs.
// This goes through the production topology (Manager), not a hand-rolled recover
// around the runner.
func TestSSO_EndToEnd_AppPanicIsContained(t *testing.T) {
	t.Parallel()
	rig := newSSORigWith(t, nil, panickingApp())

	secret, err := token.NewIssuer(rig.store).Issue("ticket-user", time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	msg := helloMsg()
	msg.Ticket = secret.Reveal()
	sid, cancel := rig.attach(t, msg, rig.plainReq())
	defer cancel()

	// The session ends, and the process is still here to assert it.
	waitFor(t, func() bool { _, ok := rig.m.Get(sid); return !ok },
		"the panicking session to be torn down")
	waitFor(t, func() bool { return len(rig.sawReleased()) == 1 },
		"the upstream state to be released while the panic unwound")
}

// A panicking Release is contained too: it must not replace the failure being
// handled, and the remaining cleanup must still run.
func TestSSO_PanickingReleaseIsContained(t *testing.T) {
	t.Parallel()
	var released int
	s, err := NewSSO(SSOConfig[*upstream]{
		Max: 4, TTL: time.Minute,
		Provision: func(_ context.Context, id *auth.Identity) (*upstream, error) {
			return &upstream{id: id.Subject}, nil
		},
		Release: func(*upstream, HandoffReason) {
			released++
			panic("consumer cleanup exploded")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := s.Session(context.Background(),
		&SessionInfo{Identity: &auth.Identity{Subject: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	release() // must not panic out of here
	if released != 1 {
		t.Errorf("Release ran %d times", released)
	}
	if s.ReleasePanics() != 1 {
		t.Errorf("ReleasePanics = %d, want the contained panic recorded", s.ReleasePanics())
	}
}

// And the containment must be VISIBLE. A private counter nobody can read is
// swallowing the panic with extra steps, which is what the doc promises not to do.
func TestSSO_PanickingReleaseIsLogged(t *testing.T) {
	t.Parallel()
	sink := &countingSink{}
	s, err := NewSSO(SSOConfig[*upstream]{
		Max: 4, TTL: time.Minute, Logger: sink,
		Provision: func(_ context.Context, id *auth.Identity) (*upstream, error) {
			return &upstream{id: id.Subject}, nil
		},
		Release: func(*upstream, HandoffReason) { panic("consumer cleanup exploded") },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := s.Session(context.Background(),
		&SessionInfo{Identity: &auth.Identity{Subject: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	release()
	if n := sink.errors(); n != 1 {
		t.Errorf("%d error records for a panicking Release — the upstream state is "+
			"gone and the log line is the only trace of it", n)
	}
}

type countingSink struct {
	mu sync.Mutex
	n  int
}

func (c *countingSink) Log(sev logger.Severity, _ any) {
	if sev != logger.SeverityError {
		return
	}
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *countingSink) errors() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// Shutdown order: after Close, a session that is only now starting must not be
// able to provision state nothing will release.
func TestSSO_SessionRefusedAfterClose(t *testing.T) {
	t.Parallel()
	provisions := 0
	s, err := NewSSO(SSOConfig[*upstream]{
		Provision: func(_ context.Context, id *auth.Identity) (*upstream, error) {
			provisions++
			return &upstream{id: id.Subject}, nil
		},
		Release: func(*upstream, HandoffReason) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, _, err := s.Session(context.Background(),
		&SessionInfo{Identity: &auth.Identity{Subject: "a"}}); !errors.Is(err, ErrStopped) {
		t.Errorf("err = %v, want ErrStopped", err)
	}
	if provisions != 0 {
		t.Error("state was provisioned after Close, so nothing is left to release it")
	}
}

// --- rig options for the failure-path tests ---------------------------------

func brokenIssuer() rigOption { return func(c *rigConfig) { c.brokenIssuer = true } }
func lateRefusal() rigOption  { return func(c *rigConfig) { c.lateRefusal = true } }

// holdBuild blocks the App's construction — and so the claim inside Session —
// until the channel closes.
func holdBuild(ch chan struct{}) rigOption {
	return func(c *rigConfig) { c.holdBuild = ch }
}
func panickingApp() rigOption { return func(c *rigConfig) { c.panicApp = true } }

// failingStore is a token store that cannot write, so Issue fails AFTER the
// credential has been verified — the window in which a consumer has already
// allocated.
type failingStore struct{ *token.MemStore }

func (failingStore) Put(token.Hash, token.Record) error {
	return errors.New("test: the token store is down")
}

// --- lector r2 on PR #14: the two concurrency boundaries --------------------

// The admission slot must not come back before the PARK entry is gone.
//
// CreateFor returns as soon as the factory hands back a Runner; the claim happens
// later, on the session's own goroutine. Settling on the Manager's schedule left a
// window where the gate had a free slot while the park was still full — so a new
// login passed the door, verified a credential, allocated upstream state, and was
// then told 503. Lector reproduced it by blocking the claim.
//
// This test blocks it the same way: the App's construction is held up, so the
// claim cannot have happened, and the gate must still show the slot as taken.
func TestSSO_AdmissionSlotHeldUntilTheParkEntryIsGone(t *testing.T) {
	t.Parallel()
	gateHeld := make(chan struct{})
	rig := newSSORigWith(t, nil, holdBuild(gateHeld))

	rec := httptest.NewRecorder()
	rig.h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	var out loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if rig.sso.Len() != 1 || rig.h.pending.pending() != 1 {
		t.Fatalf("parked=%d held=%d after a login, want 1 and 1",
			rig.sso.Len(), rig.h.pending.pending())
	}

	msg := helloMsg()
	msg.Ticket = out.Ticket
	c := &fakeConn{block: make(chan struct{})}
	c.push(msg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = rig.h.loop.serve(ctx, c, rig.plainReq()) }()

	// The session exists and the ready frame has gone out, but the claim is held.
	waitForSession(t, c)
	waitFor(t, func() bool { return rig.m.Len() == 1 }, "the session to be registered")

	// THE ASSERTION. The park still holds the entry, so the slot must still be
	// held: the two have to move together or a login can be admitted against
	// capacity that does not exist.
	if parked, held := rig.sso.Len(), rig.h.pending.pending(); parked == 1 && held == 0 {
		t.Fatalf("parked=%d but held=%d: the admission slot came back while the park "+
			"was still full, so the next login is admitted and then 503'd",
			parked, held)
	}

	close(gateHeld) // let the claim happen
	rig.waitBuild(t)
	waitFor(t, func() bool { return rig.sso.Len() == 0 && rig.h.pending.pending() == 0 },
		"the claim to empty the park and return the slot")
}

// Close must not return while a Provision is still in flight.
//
// Session checked the closed flag, unlocked, and then called Provision — so a
// Provision blocked on I/O could start, Close could return, and the Provision
// could then resume and hand back a live upstream session that nothing was left
// to close. Lector reproduced it with a channel-controlled Provision; this is the
// same shape.
func TestSSO_CloseWaitsForInFlightProvisioning(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	unblock := make(chan struct{})
	var mu sync.Mutex
	var made, released []string

	s, err := NewSSO(SSOConfig[*upstream]{
		Max: 4, TTL: time.Minute,
		Provision: func(_ context.Context, id *auth.Identity) (*upstream, error) {
			close(started)
			<-unblock // the I/O a real Provision does
			u := &upstream{id: id.Subject}
			mu.Lock()
			made = append(made, u.id)
			mu.Unlock()
			return u, nil
		},
		Release: func(u *upstream, _ HandoffReason) {
			mu.Lock()
			released = append(released, u.id)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		_, _, err := s.Session(context.Background(),
			&SessionInfo{Identity: &auth.Identity{Subject: "alice"}})
		done <- result{err: err}
	}()
	<-started

	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()

	// Close must still be waiting: the Provision has not returned.
	select {
	case <-closed:
		t.Fatal("Close returned while a Provision was still in flight, so what that " +
			"Provision produces has nothing left to release it")
	case <-time.After(100 * time.Millisecond):
	}

	close(unblock)
	select {
	case r := <-done:
		if !errors.Is(r.err, ErrStopped) {
			t.Errorf("Session err = %v, want ErrStopped: Close won the race", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Session never returned")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close never returned")
	}

	// Whatever the Provision produced was released by the losing side.
	mu.Lock()
	defer mu.Unlock()
	if len(made) != 1 {
		t.Fatalf("%d values provisioned", len(made))
	}
	if len(released) != 1 || released[0] != made[0] {
		t.Errorf("provisioned %v but released %v — a value allocated across Close is "+
			"exactly the one nobody else can clean up", made, released)
	}
}
