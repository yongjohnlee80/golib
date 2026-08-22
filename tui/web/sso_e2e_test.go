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
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
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

// A panicking Provision must not turn a leak into a HANG.
//
// Close waits for in-flight provisioning; if the bracket's retirement were not
// deferred, a Provision that panicked would leave it permanently non-empty and
// Close would wait forever. This asserts Close still returns.
func TestSSO_CloseSurvivesAPanickingProvision(t *testing.T) {
	t.Parallel()
	s, err := NewSSO(SSOConfig[*upstream]{
		Max: 4, TTL: time.Minute,
		Provision: func(context.Context, *auth.Identity) (*upstream, error) {
			panic("the consumer's dial exploded")
		},
		Release: func(*upstream, HandoffReason) {},
	})
	if err != nil {
		t.Fatal(err)
	}

	panicked := make(chan struct{})
	go func() {
		defer func() { _ = recover(); close(panicked) }()
		_, _, _ = s.Session(context.Background(),
			&SessionInfo{Identity: &auth.Identity{Subject: "alice"}})
	}()
	<-panicked

	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close hung: a panicking Provision left the in-flight bracket " +
			"permanently non-empty, which trades a leak for a deadlock")
	}
}

// Close twice must not deadlock either.
func TestSSO_CloseIsIdempotent(t *testing.T) {
	t.Parallel()
	s, err := NewSSO(SSOConfig[*upstream]{Release: func(*upstream, HandoffReason) {}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.Close(); s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a second Close deadlocked")
	}
}

// --- the settle path, exhaustively -----------------------------------------

// EVERY route that removes a parked entry must return its admission slot.
//
// Asserted as a table rather than as a claim in a review message, because "I
// audited it and there is no such route" is the shape of statement this whole
// review history has been correcting.
func TestSSO_EverySettlePath(t *testing.T) {
	t.Parallel()

	cases := map[string]func(t *testing.T, s *SSO[*upstream], handoff string){
		"claim": func(t *testing.T, s *SSO[*upstream], h string) {
			if _, ok := s.Claim(&SessionInfo{Handoff: h}); !ok {
				t.Fatal("the entry was not claimable")
			}
		},
		"claim refuses an expired entry": func(t *testing.T, s *SSO[*upstream], h string) {
			// Age it on the test clock, then claim: the refusal path must settle too,
			// or an expired login holds a slot until the gate's own backstop.
			s.now = func() time.Time { return time.Now().Add(time.Hour) }
			if _, ok := s.Claim(&SessionInfo{Handoff: h}); ok {
				t.Fatal("an expired entry was handed over")
			}
		},
		"release": func(t *testing.T, s *SSO[*upstream], h string) {
			s.Release(h, ReattachedExisting)
		},
		"expire": func(t *testing.T, s *SSO[*upstream], h string) {
			s.now = func() time.Time { return time.Now().Add(time.Hour) }
			s.expire(h)
		},
		"sweep": func(t *testing.T, s *SSO[*upstream], h string) {
			s.now = func() time.Time { return time.Now().Add(time.Hour) }
			s.Sweep()
		},
		"close": func(t *testing.T, s *SSO[*upstream], h string) {
			s.Close()
		},
	}

	for name, remove := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var settled []string
			s, err := NewSSO(SSOConfig[*upstream]{
				Max: 4, TTL: time.Minute,
				Release: func(*upstream, HandoffReason) {},
			})
			if err != nil {
				t.Fatal(err)
			}
			s.settle = func(h string) {
				mu.Lock()
				settled = append(settled, h)
				mu.Unlock()
			}
			if err := s.hold("the-handoff", &upstream{id: "a"}); err != nil {
				t.Fatal(err)
			}

			remove(t, s, "the-handoff")

			mu.Lock()
			defer mu.Unlock()
			if len(settled) != 1 || settled[0] != "the-handoff" {
				t.Errorf("settled %v via %s, want exactly [the-handoff] — an entry "+
					"removed without settling holds an admission slot until the "+
					"gate's backstop expiry", settled, name)
			}
			if n := s.Len(); n != 0 {
				t.Errorf("%d entries still parked after %s", n, name)
			}
		})
	}
}

// A raw-hook consumer must be no worse off than before the park took over
// settlement: it has no park, so the Manager's view IS the right signal, and its
// factory claims synchronously inside CreateFor.
func TestHandoff_RawHooksStillReturnAdmissionSlots(t *testing.T) {
	t.Parallel()
	p := newPark()
	h, m, _ := handoffHandler(t, p) // raw OnLogin / OnHandoffUnused, no SSO

	if h.parkSettles {
		t.Fatal("a raw-hook handler must keep the Manager's settlement")
	}
	for i := range DefaultMaxPendingLogins + 1 {
		ticket := login(t, h)
		c := &fakeConn{block: make(chan struct{})}
		msg := helloMsg()
		msg.Ticket = ticket
		c.push(msg)
		ctx, cancel := context.WithCancel(context.Background())
		go func() { _ = h.loop.serve(ctx, c, handshakeReq("https://tui.example.test")) }()
		sid := waitForSession(t, c)
		cancel()
		m.Close(sid)
		if n := h.pending.pending(); n > 1 {
			t.Fatalf("cycle %d: %d admission slots held", i+1, n)
		}
	}
	if n := h.pending.pending(); n != 0 {
		t.Errorf("%d admission slots held after %d cycles", n, DefaultMaxPendingLogins+1)
	}
}

// The gate's backstop expiry and a late claim must not both decrement.
//
// If the backstop frees a slot and the claim then settles the same handoff, a
// naive counter would go down twice and the gate would admit one login more than
// its cap for the rest of the process's life.
func TestGate_BackstopExpiryThenLateClaimDoesNotDoubleFree(t *testing.T) {
	t.Parallel()
	now := time.Now()
	g := newGate(2)
	g.now = func() time.Time { return now }

	if !g.enter() {
		t.Fatal("first enter refused")
	}
	if !g.hold("old", now.Add(time.Minute)) {
		t.Fatal("hold refused")
	}
	if g.pending() != 1 {
		t.Fatalf("occupancy %d, want 1", g.pending())
	}

	// The backstop fires: the slot is swept at the door.
	now = now.Add(2 * time.Minute)
	if g.pending() != 0 {
		t.Fatalf("occupancy %d after the backstop, want 0", g.pending())
	}

	// A second login takes a slot, and only THEN does the old claim settle.
	if !g.enter() {
		t.Fatal("a slot freed by the backstop was not reusable")
	}
	if !g.hold("new", now.Add(time.Minute)) {
		t.Fatal("hold refused for the new login")
	}
	g.release("old") // the late claim

	if n := g.pending(); n != 1 {
		t.Errorf("occupancy %d after a late settle of an already-expired slot, want 1 "+
			"— the live login's slot was freed by someone else's claim", n)
	}
}

// A park entry settled BEFORE the lease exists must not leave a ghost slot.
//
// The window: SSO's OnLogin inserts the entry and arms its timer, and only after
// the hook returns did ServeLogin install the keyed lease. Anything that settles
// the entry in between — its own expiry, a Sweep, a concurrent Close — found no
// key and did nothing, and the lease installed a moment later was owned by a
// handoff that no longer existed. It then sat on the budget until the backstop
// expiry, tens of seconds later. Lector r3 reproduced parked=0, held=1.
//
// Deterministic rather than timed: the OnLogin hook parks and then sweeps the park
// itself, which puts the settle exactly inside the window.
func TestSSO_SettleBeforeTheLeaseLeavesNoGhostSlot(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var released []*upstream
	sso, err := NewSSO(SSOConfig[*upstream]{
		Max: 4, TTL: time.Minute,
		Release: func(u *upstream, _ HandoffReason) {
			mu.Lock()
			released = append(released, u)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	hOpt, mOpt := sso.Options()

	store := token.NewMemStore(16)
	tracker, err := auth.NewMemTracker(16, auth.Backoff{
		Threshold: 100, Base: time.Second, Max: time.Second, Forget: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	loginPolicy, err := PasswordPolicyExample(
		ghostFactor{sso: sso}, tracker, contextualFactor{allow: true})
	if err != nil {
		t.Fatal(err)
	}
	attach, err := auth.NewPolicy(auth.Leaf(token.NewFactor(store)))
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { return newFakeApp() }, mOpt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	})

	h, err := NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: attach, LoginPolicy: loginPolicy,
		Issuer:         token.NewIssuer(store),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}, m, HandlerLogger(nopLogger{}), hOpt,
		// Replaces SSO's own OnLogin, keeping everything else hOpt wired (the
		// settle callback, MaxPendingLogins, parkSettles). It parks exactly as SSO
		// does and then settles the entry, landing the settle inside the window.
		OnLogin(func(handoff string, _ *auth.Identity, st *Stash) error {
			v, ok := st.Take().(*upstream)
			if !ok {
				return errors.New("nothing stashed")
			}
			if err := sso.hold(handoff, v); err != nil {
				return err
			}
			// The expiry / Sweep / Close that can land here.
			sso.now = func() time.Time { return time.Now().Add(time.Hour) }
			sso.Sweep()
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}

	if n := sso.Len(); n != 0 {
		t.Fatalf("%d entries parked, want 0 — the sweep inside the hook should have "+
			"emptied the park", n)
	}
	if n := h.pending.pending(); n != 0 {
		t.Errorf("%d admission slots held with an EMPTY park: the settle landed before "+
			"the lease existed, so the lease is a ghost nothing will ever return and "+
			"it sits on the budget until the backstop", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(released) != 1 {
		t.Errorf("%d releases: the swept value must still be cleaned up", len(released))
	}
}

// ghostFactor is the stashing login factor for the test above.
type ghostFactor struct{ sso *SSO[*upstream] }

func (ghostFactor) Kind() auth.FactorKind { return auth.FactorIdentity }
func (ghostFactor) Claim(r *auth.Request) string {
	if r == nil {
		return ""
	}
	return r.Credentials["subject"].Reveal()
}

func (f ghostFactor) Verify(ctx context.Context, r *auth.Request) (auth.Contribution, error) {
	subject := r.Credentials["subject"].Reveal()
	if subject != "alice" || r.Credentials["password"].Reveal() != "pw" {
		return auth.Contribution{}, auth.Reason("test: refused")
	}
	if err := f.sso.Stash(ctx, &upstream{id: subject}); err != nil {
		return auth.Contribution{}, auth.Reason("test: stash refused")
	}
	return auth.Contribution{Method: "test", Subject: subject, IssuedAt: time.Now()}, nil
}

// A login that outlives its admission reservation must NOT park.
//
// The fourth schedule of the family, and the one that answered the shape
// question. The reservation goes in before the login factor runs; the park
// entry's own deadline is set inside it. With equal durations the slot's deadline
// is necessarily EARLIER in absolute time than the entry's, so a hook slow enough
// — a dial to an upstream that hangs is exactly that — came back to find its slot
// lazily swept by another login's arrival, and parked anyway. The result was an
// entry nothing accounted for: the budget under-counts, so more logins can park
// than the cap allows. Lector r4 reproduced parked=1, held=0.
//
// Deterministic on a SHARED fake clock: the hook advances it past the reservation,
// triggers the lazy sweep the way a real second login would (at the door), and
// only then parks.
func TestSSO_LoginOutlivingItsReservationCannotPark(t *testing.T) {
	t.Parallel()

	var clockMu sync.Mutex
	now := time.Now()
	shared := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	}
	advance := func(d time.Duration) {
		clockMu.Lock()
		now = now.Add(d)
		clockMu.Unlock()
	}

	var relMu sync.Mutex
	var released []HandoffReason
	sso, err := NewSSO(SSOConfig[*upstream]{
		Max: 4, TTL: time.Minute, Clock: shared,
		Release: func(_ *upstream, r HandoffReason) {
			relMu.Lock()
			released = append(released, r)
			relMu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	hOpt, mOpt := sso.Options()

	store := token.NewMemStore(16)
	tracker, err := auth.NewMemTracker(16, auth.Backoff{
		Threshold: 100, Base: time.Second, Max: time.Second, Forget: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	loginPolicy, err := PasswordPolicyExample(
		ghostFactor{sso: sso}, tracker, contextualFactor{allow: true})
	if err != nil {
		t.Fatal(err)
	}
	attach, err := auth.NewPolicy(auth.Leaf(token.NewFactor(store)))
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { return newFakeApp() }, mOpt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	})

	// SSO's OWN OnLogin is used — not a stand-in — so the release path under test
	// is the shipped one.
	h, err := NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: attach, LoginPolicy: loginPolicy,
		Issuer:         token.NewIssuer(store),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}, m, HandlerLogger(nopLogger{}), hOpt)
	if err != nil {
		t.Fatal(err)
	}
	h.now = shared
	h.pending.now = shared

	// The delay goes exactly where a slow hook puts it: after the reservation,
	// before the park's insert. Only the TIMING is injected — the decision is still
	// the real gate's, since inner is the commit hOpt installed.
	inner := sso.commit
	sso.commit = func(handoff string, publish func()) bool {
		advance(2 * h.pendingHold)
		// Another login arriving is what reclaims the slot; the sweep is lazy, at
		// the door, and pending() sweeps the same way enter() does.
		if held := h.pending.pending(); held != 0 {
			t.Errorf("%d slots held after the reservation lapsed", held)
		}
		return inner(handoff, publish)
	}

	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))

	if rec.Code == http.StatusOK {
		t.Error("a login that outlived its admission reservation was allowed to " +
			"park and got a ticket")
	}
	if n := sso.Len(); n != 0 {
		t.Errorf("%d entries parked with 0 admission slots held: the budget now "+
			"under-counts, so more logins can park than the cap allows", n)
	}
	if n := h.pending.pending(); n != 0 {
		t.Errorf("%d admission slots held after the failed login", n)
	}
	// The value the factor allocated is still cleaned up.
	relMu.Lock()
	defer relMu.Unlock()
	if len(released) != 1 || released[0] != LoginFailed {
		t.Errorf("released %v, want one LoginFailed — a refused park must not leak "+
			"the upstream session the factor already opened", released)
	}
	// And the ticket that WAS minted is gone from the store. Verifying a literal
	// "x" proved only that nonsense is rejected, which was never in question
	// (lector r5); the store's own count is the thing that shows the revoke
	// happened.
	if n := store.Len(); n != 0 {
		t.Errorf("%d tickets left in the store: the ticket minted for a login that "+
			"could not park is still usable", n)
	}
}

// --- the invariant, under concurrency --------------------------------------

// Everything allocated is released exactly once, and both budgets return to zero.
//
// Four review rounds found four ordering windows in this flow, every one of them
// by pausing one side of a pair. A stress run cannot replace those — it would have
// found none of them reliably — but it covers the thing they were all symptoms of:
// across many interleavings of login, attach, claim, settle and expiry, no upstream
// value is leaked or released twice, and neither the park nor the admission gate
// ends up holding anything.
//
// It also fails loudly on a lock-order inversion between the park's mutex and the
// gate's, which the race detector cannot see: the test simply would not finish.
//
// The paths it actually reaches, established by MUTATION rather than by reading
// the code: a login that parks and is claimed, and a login whose factor allocates
// and is then refused — deleting either one's cleanup turns this red. Expiry
// competes with both throughout, and the documented shutdown order runs at the end.
//
// It does NOT reach the park-full refusal, the Close/provision race, the
// reservation lapse, or a double release; deleting those cleanups leaves this test
// green. Each has its own deterministic test, which is where that coverage lives —
// and saying so here is the difference between a stress test and a stress test
// whose passing gets read as a guarantee it never made.
func TestSSO_ConcurrentLoginsPreserveTheInvariant(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	allocated := map[string]int{}
	released := map[string]int{}
	var seq int

	nextID := func(kind string) string {
		mu.Lock()
		defer mu.Unlock()
		seq++
		id := kind + "-" + strconv.Itoa(seq)
		allocated[id]++
		return id
	}

	sso, err := NewSSO(SSOConfig[*upstream]{
		Max: 4, TTL: 50 * time.Millisecond, // short on purpose: expiry competes
		Provision: func(_ context.Context, id *auth.Identity) (*upstream, error) {
			return &upstream{id: nextID("provisioned")}, nil
		},
		Release: func(u *upstream, _ HandoffReason) {
			mu.Lock()
			released[u.id]++
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	hOpt, mOpt := sso.Options()

	store := token.NewMemStore(256)
	tracker, err := auth.NewMemTracker(256, auth.Backoff{
		Threshold: 10_000, Base: time.Second, Max: time.Second, Forget: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	var verifies int
	loginPolicy, err := PasswordPolicyExample(
		stressFactor{
			sso:   sso,
			alloc: func() string { return nextID("parked") },
			mu:    &mu,
			n:     &verifies,
		},
		tracker, contextualFactor{allow: true})
	if err != nil {
		t.Fatal(err)
	}
	attach, err := auth.NewPolicy(auth.Leaf(token.NewFactor(store)))
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(sso.Factory(
		func(*Backend, *auth.Identity, *upstream) Runner { return newFakeApp() }), mOpt)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: attach, LoginPolicy: loginPolicy,
		Issuer:         token.NewIssuer(store),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}, m, HandlerLogger(nopLogger{}), hOpt)
	if err != nil {
		t.Fatal(err)
	}

	const workers, cycles = 8, 4
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for range cycles {
				rec := httptest.NewRecorder()
				h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
				if rec.Code != http.StatusOK {
					// A refusal is a legitimate outcome under contention — the point
					// is that a refusal leaks nothing either.
					continue
				}
				var out loginResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
					return
				}
				msg := helloMsg()
				msg.Ticket = out.Ticket
				c := &fakeConn{block: make(chan struct{})}
				c.push(msg)
				// Short: the fake connection never sends again, so serve runs until
				// the context expires. The invariant under test does not depend on
				// the session being created — a refused attach must release too —
				// so this buys interleavings rather than waiting them out.
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				_ = h.loop.serve(ctx, c, stressReq())
				cancel()
			}
		}(w)
	}

	// Expiry and shutdown competing with all of the above.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				sso.Sweep()
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	wg.Wait()
	close(stop)

	// Drain: end every session, then close the park. The documented order.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	sso.Close()

	if n := sso.Len(); n != 0 {
		t.Errorf("%d entries still parked after shutdown", n)
	}
	if n := h.pending.pending(); n != 0 {
		t.Errorf("%d admission slots still held after shutdown", n)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(allocated) == 0 {
		t.Fatal("nothing was allocated: the stress run did no work, so it proved nothing")
	}
	var leaked, doubled []string
	for id := range allocated {
		switch released[id] {
		case 1:
		case 0:
			leaked = append(leaked, id)
		default:
			doubled = append(doubled, id)
		}
	}
	sort.Strings(leaked)
	sort.Strings(doubled)
	if len(leaked) > 0 {
		t.Errorf("%d of %d upstream sessions were never released: %v",
			len(leaked), len(allocated), leaked)
	}
	if len(doubled) > 0 {
		t.Errorf("%d upstream sessions were released more than once: %v",
			len(doubled), doubled)
	}
	for id := range released {
		if allocated[id] == 0 {
			t.Errorf("released %q, which was never allocated", id)
		}
	}
	t.Logf("%d upstream sessions allocated and released exactly once", len(allocated))
}

// stressFactor stashes a freshly allocated value on every verify — and then fails
// a deterministic fraction of them.
//
// The failing fraction is the point. Without it every login in the run either
// parked successfully or failed inside hold with the value already taken, so the
// PRE-PARK path — a factor that allocates and is then refused, whose cleanup is
// the login route's deferred discard — was never exercised. The control proved it:
// deleting that discard left this test green. A stress test that cannot see a leak
// it claims to cover is worse than no stress test, because its passing is read as
// evidence.
type stressFactor struct {
	sso   *SSO[*upstream]
	alloc func() string
	mu    *sync.Mutex
	n     *int
}

func (stressFactor) Kind() auth.FactorKind { return auth.FactorIdentity }
func (stressFactor) Claim(r *auth.Request) string {
	if r == nil {
		return ""
	}
	return r.Credentials["subject"].Reveal()
}

func (f stressFactor) Verify(ctx context.Context, r *auth.Request) (auth.Contribution, error) {
	if r.Credentials["subject"].Reveal() != "alice" ||
		r.Credentials["password"].Reveal() != "pw" {
		return auth.Contribution{}, auth.Reason("test: refused")
	}
	u := &upstream{id: f.alloc()}
	if err := f.sso.Stash(ctx, u); err != nil {
		return auth.Contribution{}, auth.Reason("test: stash refused")
	}
	// Every third verify allocates and THEN refuses — a factor that opens an
	// upstream session and only afterwards finds the account disabled. Nothing has
	// parked yet, so the login route's deferred discard is the only thing that can
	// release it.
	f.mu.Lock()
	*f.n++
	fail := *f.n%3 == 0
	f.mu.Unlock()
	if fail {
		return auth.Contribution{}, auth.Reason("test: refused after allocating")
	}
	return auth.Contribution{Method: "test", Subject: "alice", IssuedAt: time.Now()}, nil
}

func stressReq() requestInfo {
	r := httptest.NewRequest(http.MethodGet, "/attach", nil)
	r.RemoteAddr = "203.0.113.7:44321"
	r.Host = "tui.example.test"
	r.Header.Set("Origin", "https://tui.example.test")
	return requestInfo{http: r}
}

// A login refused because the PARK IS FULL must not leak what its factor already
// allocated.
//
// Reaching this at all took discovering something worth writing down: when
// SSO.Options ties the two budgets — the park's Max SETS MaxPendingLogins — the
// park can never be full at the moment a committed reservation exists. Our own slot
// is one of the at-most-Max slots and our entry is not inserted yet, so parked
// entries are at most Max-1. The gate refuses first, at the door, with a 503. The
// park-full branch is therefore UNREACHABLE in the tied configuration.
//
// It becomes reachable again the moment a consumer unties them, which this test
// does deliberately: apply Options and then override MaxPendingLogins upward. That
// is an ill-advised configuration and it is a supported one, so the release on that
// branch is not dead code — and the value has already been taken out of the stash
// by then, so SSO's own release is the only cleanup that can see it.
func TestSSO_ParkFullReleasesTheTakenValue(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var released []HandoffReason
	sso, err := NewSSO(SSOConfig[*upstream]{
		Max: 1, TTL: time.Minute,
		Release: func(_ *upstream, r HandoffReason) {
			mu.Lock()
			released = append(released, r)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	hOpt, mOpt := sso.Options()

	store := token.NewMemStore(16)
	tracker, err := auth.NewMemTracker(16, auth.Backoff{
		Threshold: 100, Base: time.Second, Max: time.Second, Forget: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	loginPolicy, err := PasswordPolicyExample(
		ghostFactor{sso: sso}, tracker, contextualFactor{allow: true})
	if err != nil {
		t.Fatal(err)
	}
	attach, err := auth.NewPolicy(auth.Leaf(token.NewFactor(store)))
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { return newFakeApp() }, mOpt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	})
	h, err := NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: attach, LoginPolicy: loginPolicy,
		Issuer:         token.NewIssuer(store),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
		// The untying: the gate is now larger than the park, so a second login
		// reaches the park instead of being turned away at the door.
	}, m, HandlerLogger(nopLogger{}), hOpt, MaxPendingLogins(8))
	if err != nil {
		t.Fatal(err)
	}

	// The first login fills the park legitimately, through the real route.
	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("the first login: %d %s", rec.Code, rec.Body.String())
	}
	if n := sso.Len(); n != 1 {
		t.Fatalf("park holds %d after one login, want 1", n)
	}

	// The second reaches a full park.
	rec = httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code == http.StatusOK {
		t.Error("a login was admitted with the park already at capacity")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(released) != 1 || released[0] != LoginFailed {
		t.Errorf("released %v, want one LoginFailed — the value SSO took out of the "+
			"stash before hold failed is SSO's to release, and the login route's "+
			"discard cannot see it", released)
	}
	if n := sso.Len(); n != 1 {
		t.Errorf("park holds %d, want just the first login's entry", n)
	}
	// The first login's slot is still held (its entry is still parked); the second
	// login's is not.
	if n := h.pending.pending(); n != 1 {
		t.Errorf("%d admission slots held, want 1 — the refused login's slot must go "+
			"back and the parked one's must not", n)
	}
}

// The reservation must still be alive at the moment the entry is PUBLISHED, not
// merely at the moment it was checked.
//
// r4's repair checked the reservation before inserting. r5 showed that was still
// not atomic: gate.commit released the gate's lock as it returned its answer, and
// the key could be swept between the answer and the insert. A bool returned before
// a mutation is a time-of-check-to-time-of-use gap however carefully it is
// computed.
//
// This pauses in exactly that gap. The previous test
// (TestSSO_LoginOutlivingItsReservationCannotPark) pauses BEFORE the check, so it
// cannot see this; the two windows are one instruction apart and need separate
// probes.
func TestSSO_ReservationSweptBetweenCheckAndPublish(t *testing.T) {
	t.Parallel()

	var clockMu sync.Mutex
	now := time.Now()
	shared := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	}
	advance := func(d time.Duration) {
		clockMu.Lock()
		now = now.Add(d)
		clockMu.Unlock()
	}

	var relMu sync.Mutex
	var released []HandoffReason
	sso, err := NewSSO(SSOConfig[*upstream]{
		Max: 4, TTL: time.Minute, Clock: shared,
		Release: func(_ *upstream, r HandoffReason) {
			relMu.Lock()
			released = append(released, r)
			relMu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	hOpt, mOpt := sso.Options()

	store := token.NewMemStore(16)
	tracker, err := auth.NewMemTracker(16, auth.Backoff{
		Threshold: 100, Base: time.Second, Max: time.Second, Forget: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	loginPolicy, err := PasswordPolicyExample(
		ghostFactor{sso: sso}, tracker, contextualFactor{allow: true})
	if err != nil {
		t.Fatal(err)
	}
	attach, err := auth.NewPolicy(auth.Leaf(token.NewFactor(store)))
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { return newFakeApp() }, mOpt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	})
	h, err := NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: attach, LoginPolicy: loginPolicy,
		Issuer:         token.NewIssuer(store),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}, m, HandlerLogger(nopLogger{}), hOpt)
	if err != nil {
		t.Fatal(err)
	}
	h.now = shared
	h.pending.now = shared

	// The pause goes INSIDE the publish callback: by the time this runs, the gate
	// has already checked membership and re-stamped the deadline. Advancing the
	// clock past it and sweeping is what r5's probe does — and if publication were
	// not happening under the gate's own lock, the sweep would take the key and the
	// entry would land anyway.
	// The pause goes INSIDE the publish callback, which is the gap itself: the gate
	// has already checked membership and re-stamped the deadline by the time this
	// runs.
	//
	// The clock is advanced so a sweep WANTS to take the key, a sweeper is started
	// concurrently, and then the entry is published. If publication happened outside
	// the gate's lock, the sweeper would win and the entry would land on a key that
	// no longer exists. The clock is rolled back before the callback returns, so the
	// sweeper — once it is finally let in — measures the live state rather than the
	// artificial one, and the end-state assertions mean what they say.
	inner := sso.commit
	sso.commit = func(handoff string, publish func()) bool {
		return inner(handoff, func() {
			before := shared()
			advance(2 * h.pendingHold)

			// DETERMINISTIC, not timed. A sleep-and-check can miss a sweeper that is
			// merely slow to start, so it proves nothing on a fast pass (lector r6).
			// TryLock asks the question directly: can anything else enter the gate
			// right now? While publication is atomic the answer must be no, and the
			// answer does not depend on scheduling.
			if h.pending.mu.TryLock() {
				h.pending.mu.Unlock()
				t.Error("the gate's lock is free during publication, so a sweep can " +
					"take the key and the entry will be published against a slot " +
					"that no longer exists")
			}

			publish()

			// Rolled back before returning, so anything that enters the gate after
			// this measures the real deadline rather than the artificial one.
			clockMu.Lock()
			now = before
			clockMu.Unlock()
		})
	}

	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	// THE INVARIANT: an entry exists only if a slot accounts for it.
	parked, held := sso.Len(), h.pending.pending()
	if parked != 1 || held != 1 {
		t.Errorf("parked=%d held=%d, want 1 and 1 — an entry published with no "+
			"admission slot accounting for it makes the budget under-count", parked, held)
	}
	relMu.Lock()
	defer relMu.Unlock()
	if len(released) != 0 {
		t.Errorf("released %v: the parked value is still live and must not have been "+
			"cleaned up", released)
	}
}

// A RAW-HOOK consumer can be atomic too, without access to the gate.
//
// The gate is unexported, so before Stash.CommitPark a consumer not using web.SSO
// had no way to publish its park entry safely at all — the docs told it to keep the
// hook short and hope. That is not a contract, and lector r5 was right to say so.
//
// This is the raw-hook equivalent of the SSO path: publish inside CommitPark, and
// refuse to park when it returns false.
func TestOnLogin_RawHookCanPublishAtomically(t *testing.T) {
	t.Parallel()

	var clockMu sync.Mutex
	now := time.Now()
	shared := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	}

	type hookResult struct {
		committed bool
		parked    bool
	}
	var resMu sync.Mutex
	var results []hookResult
	var lapse atomic.Bool

	store := token.NewMemStore(16)
	tracker, err := auth.NewMemTracker(16, auth.Backoff{
		Threshold: 100, Base: time.Second, Max: time.Second, Forget: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	loginPolicy, err := PasswordPolicyExample(
		claimingFactor{subject: "alice", password: "pw"}, tracker,
		contextualFactor{allow: true})
	if err != nil {
		t.Fatal(err)
	}
	attach, err := auth.NewPolicy(auth.Leaf(token.NewFactor(store)))
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { return newFakeApp() })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	})

	// A hand-rolled park: a plain map the hook writes under CommitPark.
	var parkMu sync.Mutex
	park := map[string]string{}

	var h *Handler
	h, err = NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: attach, LoginPolicy: loginPolicy,
		Issuer:         token.NewIssuer(store),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}, m, HandlerLogger(nopLogger{}), MaxPendingLogins(4),
		OnLogin(func(handoff string, id *auth.Identity, st *Stash) error {
			if lapse.Load() {
				// Simulate a hook that took too long: its reservation is gone.
				clockMu.Lock()
				now = now.Add(2 * h.pendingHold)
				clockMu.Unlock()
				_ = h.pending.pending() // the sweep another login would trigger
			}
			ok := st.CommitPark(func() {
				parkMu.Lock()
				park[handoff] = id.Subject
				parkMu.Unlock()
			})
			resMu.Lock()
			results = append(results, hookResult{committed: ok, parked: ok})
			resMu.Unlock()
			if !ok {
				return errors.New("the admission reservation lapsed; not parking")
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	h.now = shared
	h.pending.now = shared

	// The ordinary case: commits, parks, and holds its slot.
	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("the ordinary login: %d %s", rec.Code, rec.Body.String())
	}
	parkMu.Lock()
	n := len(park)
	parkMu.Unlock()
	if n != 1 {
		t.Errorf("hand-rolled park holds %d after a successful login, want 1", n)
	}
	if held := h.pending.pending(); held != 1 {
		t.Errorf("%d admission slots held, want 1", held)
	}

	// The lapsed case: refuses to commit, so the hook must not park.
	lapse.Store(true)
	rec = httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	if rec.Code == http.StatusOK {
		t.Error("a login whose reservation had lapsed still got a ticket")
	}

	resMu.Lock()
	defer resMu.Unlock()
	if len(results) != 2 {
		t.Fatalf("%d hook runs, want 2", len(results))
	}
	if !results[0].committed {
		t.Error("the ordinary login could not commit its reservation")
	}
	if results[1].committed {
		t.Error("CommitPark returned true for a lapsed reservation, so a hand-rolled " +
			"park has no way to know it must not publish")
	}
	parkMu.Lock()
	defer parkMu.Unlock()
	if len(park) != 1 {
		t.Errorf("hand-rolled park holds %d, want just the first login's entry", len(park))
	}
}

// A reservation is not a commitment.
//
// The distinction is what lets the login route decide whether to return the slot
// when the hook is done: a hook that parked committed, one that did not did not.
// Collapsing the two — treating any held key as accounting for an entry — means a
// hook that parks nothing keeps a slot until the backstop, and a hook that parks
// without committing keeps one that accounts for an entry the gate never agreed to.
func TestGate_ReservationIsNotACommitment(t *testing.T) {
	t.Parallel()
	now := time.Now()
	g := newGate(2)
	g.now = func() time.Time { return now }

	if !g.enter() {
		t.Fatal("enter refused")
	}
	if !g.hold("k", now.Add(time.Minute)) {
		t.Fatal("hold refused")
	}
	if g.committed("k") {
		t.Error("a bare reservation reports as committed, so a hook that parks " +
			"nothing would keep its slot")
	}

	published := false
	if !g.commit("k", now.Add(time.Minute), func() { published = true }) {
		t.Fatal("commit refused a live reservation")
	}
	if !published {
		t.Error("commit did not run the publish callback")
	}
	if !g.committed("k") {
		t.Error("a committed slot does not report as committed, so the login route " +
			"would return a slot that accounts for a parked entry")
	}

	// And a commit on a key that is gone neither publishes nor lies about it.
	g.release("k")
	published = false
	if g.commit("k", now.Add(time.Minute), func() { published = true }) {
		t.Error("commit succeeded for a slot that no longer exists")
	}
	if published {
		t.Error("commit published for a slot that no longer exists — the entry would " +
			"be unaccounted, which is the whole defect this exists to prevent")
	}
	if g.committed("k") {
		t.Error("a released key reports as committed")
	}
}

// A panicking publish callback must not wedge the gate.
//
// Publication runs while the gate holds its own lock, which is what makes it
// atomic — and also what would make a panic there catastrophic if the lock were not
// released on the way out. Every other login goes through this lock, so a wedged
// gate is a server that accepts no further logins at all.
func TestGate_PanickingPublishDoesNotWedgeTheGate(t *testing.T) {
	t.Parallel()
	now := time.Now()
	g := newGate(2)
	g.now = func() time.Time { return now }

	if !g.enter() || !g.hold("k", now.Add(time.Minute)) {
		t.Fatal("setup")
	}
	func() {
		defer func() {
			if rec := recover(); rec == nil {
				t.Error("the panic did not propagate to the caller")
			}
		}()
		g.commit("k", now.Add(time.Minute), func() { panic("consumer publish exploded") })
	}()

	// The gate is still usable. Without the deferred unlock this call never returns
	// and the test times out — which is what a wedged login route looks like.
	done := make(chan bool, 1)
	go func() { done <- g.enter() }()
	select {
	case ok := <-done:
		if !ok {
			t.Error("the gate refused a slot it should have had")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the gate is wedged after a panicking publish: no further login can " +
			"pass the door")
	}
}

// One reservation publishes at most ONE entry, and never zero-with-a-commitment.
//
// Lector r6's two probes: "one admission slot published 2 park entries" and "nil
// publisher committed the slot even though no park entry exists". Both came from
// the same mistake — I had reasoned that a concurrent second commit on one Stash
// might be legitimate and so declined to make the capability single-use. It is not
// legitimate: a Stash belongs to ONE request, which holds one reservation and may
// publish one entry. Exactly one caller may win.
//
// Both properties are enforced TWICE — once on the Stash before the reservation's
// lock is taken, once in the gate — so a control has to remove both layers to turn
// these red. That is deliberate: the Stash guard is what makes a recursive call a
// false instead of a deadlock, and the gate guard is what protects a caller that is
// not single-use.
func TestStash_CommitParkIsSingleUseAndRejectsNil(t *testing.T) {
	t.Parallel()

	newRig := func() (*Stash, *gate, *int, string) {
		now := time.Now()
		g := newGate(2)
		g.now = func() time.Time { return now }
		if !g.enter() || !g.hold("h", now.Add(time.Minute)) {
			t.Fatal("setup")
		}
		entries := 0
		st := &Stash{}
		st.commit = func(publish func()) bool {
			return g.commit("h", now.Add(time.Minute), publish)
		}
		return st, g, &entries, "h"
	}

	t.Run("a second call publishes nothing", func(t *testing.T) {
		t.Parallel()
		st, g, entries, key := newRig()
		if !st.CommitPark(func() { *entries++ }) {
			t.Fatal("the first commit was refused")
		}
		if st.CommitPark(func() { *entries++ }) {
			t.Error("a second CommitPark succeeded: one admission slot would account " +
				"for two park entries")
		}
		if *entries != 1 {
			t.Errorf("%d entries published against one reservation, want 1", *entries)
		}
		if !g.committed(key) {
			t.Error("the slot is not committed after a successful publish")
		}
	})

	t.Run("a nil publisher is refused", func(t *testing.T) {
		t.Parallel()
		st, g, _, key := newRig()
		if st.CommitPark(nil) {
			t.Error("a nil publisher committed the slot, so the accounting has a slot " +
				"with no park entry — the mirror of the defect this prevents")
		}
		if g.committed(key) {
			t.Error("the slot was committed for a publisher that wrote nothing")
		}
	})

	t.Run("a recursive call is refused, not deadlocked", func(t *testing.T) {
		t.Parallel()
		st, _, entries, _ := newRig()
		var inner bool
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = st.CommitPark(func() {
				*entries++
				inner = st.CommitPark(func() { *entries++ })
			})
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("a recursive CommitPark deadlocked: it must be refused before the " +
				"reservation's lock is taken")
		}
		if inner {
			t.Error("the recursive call succeeded")
		}
		if *entries != 1 {
			t.Errorf("%d entries published, want 1", *entries)
		}
	})

	t.Run("concurrent callers: exactly one wins", func(t *testing.T) {
		t.Parallel()
		st, _, entries, _ := newRig()
		var mu sync.Mutex
		wins := 0
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if st.CommitPark(func() {
					mu.Lock()
					*entries++
					mu.Unlock()
				}) {
					mu.Lock()
					wins++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		if wins != 1 || *entries != 1 {
			t.Errorf("%d winners and %d entries from one reservation, want 1 and 1", wins, *entries)
		}
	})

	t.Run("the capability dies with the hook", func(t *testing.T) {
		t.Parallel()
		st, _, entries, _ := newRig()
		st.disarm()
		if st.CommitPark(func() { *entries++ }) {
			t.Error("a Stash kept beyond its request could still publish")
		}
		if *entries != 0 {
			t.Errorf("%d entries published after the hook returned", *entries)
		}
	})
}

// The gate refuses a second commit on its own, independently of the Stash.
//
// This is a BACKSTOP and it is deliberately untested from above: no path in this
// package can reach it, because Stash.CommitPark is single-use and SSO.hold refuses
// a duplicate handoff before it commits. Removing this check leaves every
// higher-level test green, which is exactly why it gets a direct one — a defence
// nothing exercises is a defence nobody notices has gone.
func TestGate_RefusesASecondCommitOnItsOwn(t *testing.T) {
	t.Parallel()
	now := time.Now()
	g := newGate(2)
	g.now = func() time.Time { return now }
	if !g.enter() || !g.hold("k", now.Add(time.Minute)) {
		t.Fatal("setup")
	}
	first, second := 0, 0
	if !g.commit("k", now.Add(time.Minute), func() { first++ }) {
		t.Fatal("the first commit was refused")
	}
	if g.commit("k", now.Add(time.Minute), func() { second++ }) {
		t.Error("a second commit on one reservation succeeded, so one slot would " +
			"account for two entries")
	}
	if first != 1 || second != 0 {
		t.Errorf("published %d then %d, want 1 then 0", first, second)
	}
}

// A panicking publish KEEPS the commitment, and the backstop reclaims the slot.
//
// This asserts the opposite of what it asserted one revision ago, and the reversal
// is the point. Rolling the commitment back looked obviously right — nothing was
// published, so nothing should be accounted for — but that reasoning holds only for
// a callback that panics BEFORE it mutates. A consumer's callback can insert into
// its own park and then panic, and this package cannot undo a write to a data
// structure it does not own (lector r7 reproduced parked=1, committed=0 that way).
//
// The two errors are not symmetric, which is what settles it. Keeping a commitment
// for an entry never written holds one slot until the backstop expiry — bounded,
// self-healing. Dropping one for an entry that WAS written breaks the pending-login
// cap and stays broken. So the accounting is preserved conservatively.
func TestGate_PanickingPublishKeepsTheCommitmentConservatively(t *testing.T) {
	t.Parallel()
	now := time.Now()
	g := newGate(2)
	g.now = func() time.Time { return now }
	if !g.enter() || !g.hold("k", now.Add(time.Minute)) {
		t.Fatal("setup")
	}

	// The dangerous shape: the callback mutates and THEN panics.
	externalPark := map[string]bool{}
	func() {
		defer func() {
			if rec := recover(); rec == nil {
				t.Error("the panic did not propagate to the caller")
			}
		}()
		g.commit("k", now.Add(2*time.Minute), func() {
			externalPark["k"] = true
			panic("consumer publish exploded after mutating its park")
		})
	}()

	if !externalPark["k"] {
		t.Fatal("the callback did not mutate: this test needs the mutating shape")
	}
	if !g.committed("k") {
		t.Error("the commitment was dropped for an entry that WAS written, so the " +
			"pending-login cap now under-counts permanently")
	}

	// The gate is not wedged — every other login goes through this lock.
	done := make(chan bool, 1)
	go func() { done <- g.enter() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the gate is wedged after a panicking publish")
	}

	// And the slot is not held forever: the backstop reclaims it.
	now = now.Add(3 * time.Minute)
	if n := g.pending(); n != 1 {
		t.Errorf("occupancy %d after the backstop, want only the second enter's slot", n)
	}
	if g.committed("k") {
		t.Error("the slot survived its backstop expiry")
	}
}

// A hook that spawns a goroutine and returns must not be able to publish behind
// the login route's back.
//
// This is the race I flagged to lector rather than found: Stash.disarm runs when
// the hook returns, and it sets the commit closure to nil. But CommitPark treats a
// nil closure as "this handler keeps no pending-login budget, so just write" —
// which after a disarm would publish into the consumer's park with nothing
// accounting for it.
//
// The state guard is what actually prevents it: disarm marks the Stash spent, and
// CommitPark checks the state BEFORE it looks at the closure. Worth a test because
// the safety comes from the ordering of two checks inside one function, which is
// exactly the kind of thing this review history has been finding.
func TestStash_LateCommitAfterTheHookReturnsIsRefused(t *testing.T) {
	t.Parallel()

	published := make(chan bool, 1)
	release := make(chan struct{})

	store := token.NewMemStore(16)
	tracker, err := auth.NewMemTracker(16, auth.Backoff{
		Threshold: 100, Base: time.Second, Max: time.Second, Forget: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	loginPolicy, err := PasswordPolicyExample(
		claimingFactor{subject: "alice", password: "pw"}, tracker,
		contextualFactor{allow: true})
	if err != nil {
		t.Fatal(err)
	}
	attach, err := auth.NewPolicy(auth.Leaf(token.NewFactor(store)))
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { return newFakeApp() })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	})

	h, err := NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: attach, LoginPolicy: loginPolicy,
		Issuer:         token.NewIssuer(store),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}, m, HandlerLogger(nopLogger{}), MaxPendingLogins(4),
		OnLogin(func(_ string, _ *auth.Identity, st *Stash) error {
			// A hook that defers its own work to a goroutine and returns. Wrong, and
			// the point is that being wrong cannot corrupt the accounting.
			go func() {
				<-release // ensure the hook has returned and disarm has run
				published <- st.CommitPark(func() {})
			}()
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
	close(release)

	select {
	case ok := <-published:
		if ok {
			t.Error("a goroutine published into the park after its hook had returned, " +
				"so the entry has no admission slot accounting for it")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the late CommitPark never returned")
	}
	if n := h.pending.pending(); n != 0 {
		t.Errorf("%d admission slots held after a hook that parked nothing", n)
	}
}

// The publishing capability must die with the HOOK, not with the request.
//
// `defer` in Go is function-scoped, so `defer stash.disarm()` inside the
// `if h.onLogin != nil` block retired the capability when ServeLogin returned —
// leaving it live through every path below, including the slow ones. Lector r7
// reproduced it by blocking the error path inside Issuer.Revoke: a retained Stash
// published successfully in that window, and the cleanup then released the slot,
// leaving an entry with nothing accounting for it.
//
// This reproduces the same interval. A revoking store blocks until the test lets
// it go, and the goroutine tries to publish while it is blocked — which is AFTER
// the hook returned but BEFORE ServeLogin did.
func TestStash_CapabilityDiesWithTheHookNotTheRequest(t *testing.T) {
	t.Parallel()

	revokeEntered := make(chan struct{})
	releaseRevoke := make(chan struct{})
	published := make(chan bool, 1)

	store := &blockingRevokeStore{
		MemStore: token.NewMemStore(16),
		entered:  revokeEntered,
		release:  releaseRevoke,
	}
	tracker, err := auth.NewMemTracker(16, auth.Backoff{
		Threshold: 100, Base: time.Second, Max: time.Second, Forget: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	loginPolicy, err := PasswordPolicyExample(
		claimingFactor{subject: "alice", password: "pw"}, tracker,
		contextualFactor{allow: true})
	if err != nil {
		t.Fatal(err)
	}
	attach, err := auth.NewPolicy(auth.Leaf(token.NewFactor(store)))
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { return newFakeApp() })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	})

	h, err := NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: attach, LoginPolicy: loginPolicy,
		Issuer:         token.NewIssuer(store),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}, m, HandlerLogger(nopLogger{}), MaxPendingLogins(4),
		OnLogin(func(_ string, _ *auth.Identity, st *Stash) error {
			// A hook that keeps the Stash and fails, which sends the request down
			// the revoke path.
			go func() {
				<-revokeEntered // the request is now inside Revoke
				published <- st.CommitPark(func() {})
			}()
			return errors.New("the hook refuses, so the ticket must be revoked")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	serveDone := make(chan struct{})
	go func() {
		h.ServeLogin(rec, loginPost(`{"subject":"alice","password":"pw"}`))
		close(serveDone)
	}()

	// The publish attempt happens while ServeLogin is still inside Revoke.
	select {
	case ok := <-published:
		if ok {
			t.Error("a retained Stash published after its hook returned but before " +
				"the request did, so the entry has no admission slot accounting for it")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the late CommitPark never returned")
	}
	close(releaseRevoke)
	<-serveDone

	if rec.Code == http.StatusOK {
		t.Error("a login whose hook refused returned a ticket")
	}
	if n := h.pending.pending(); n != 0 {
		t.Errorf("%d admission slots held after a refused login", n)
	}
}

// blockingRevokeStore holds the request inside Revoke, which is the window the
// function-scoped disarm left open.
type blockingRevokeStore struct {
	*token.MemStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingRevokeStore) Revoke(h token.Hash) error {
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	return s.MemStore.Revoke(h)
}
