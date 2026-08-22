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
	"github.com/yongjohnlee80/golib/auth/sshkey"
	"github.com/yongjohnlee80/golib/auth/token"
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
	mu       sync.Mutex
	built    []*e2eUpstream
	released []*e2eUpstream
}

func (r *ssoRig) record(u *e2eUpstream) {
	r.mu.Lock()
	r.built = append(r.built, u)
	r.mu.Unlock()
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
func newSSORig(t *testing.T) *ssoRig {
	t.Helper()
	rig := &ssoRig{store: token.NewMemStore(64)}

	sso, err := NewSSO(SSOConfig[*e2eUpstream]{
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
	})
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
	loginPolicy, err := PasswordPolicyExample(e2eLoginFactor{rig: rig}, tracker, allow)
	if err != nil {
		t.Fatal(err)
	}

	// --- the consumer's whole app wiring: one build function ----------------
	m, err := NewManager(sso.Factory(func(_ *Backend, id *auth.Identity, up *e2eUpstream) Runner {
		if up == nil {
			t.Error("the build function received no upstream state")
			return newFakeApp()
		}
		if id == nil || id.Subject != up.subject {
			t.Errorf("identity %v does not match the upstream state for %q", id, up.subject)
		}
		rig.record(up)
		return newFakeApp()
	}), mOpt)
	if err != nil {
		t.Fatal(err)
	}
	rig.m = m
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	})

	h, err := NewHandler(Config{
		Addr: "127.0.0.1:8080", Policy: attach, LoginPolicy: loginPolicy,
		Issuer:         token.NewIssuer(rig.store),
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

	built := rig.sawBuilt()
	if len(built) != 1 {
		t.Fatalf("%d apps built, want 1", len(built))
	}
	if built[0].origin != "login" {
		t.Errorf("the app got %s state — a password login must be CLAIMED, not "+
			"re-provisioned, or the login's own allocation leaks", built[0].origin)
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

	built := rig.sawBuilt()
	if len(built) != 1 {
		t.Fatalf("%d apps built, want 1", len(built))
	}
	if built[0].origin != "provision" || built[0].subject != "ticket-user" {
		t.Errorf("app got %+v, want provisioned state for ticket-user", built[0])
	}
}

// auth/mtls: a verified chain, no ticket at all.
func TestSSO_EndToEnd_MTLSProvisions(t *testing.T) {
	t.Parallel()
	rig := newSSORig(t)

	_, cancel := rig.attach(t, helloMsg(), rig.tlsReq())
	defer cancel()

	built := rig.sawBuilt()
	if len(built) != 1 {
		t.Fatalf("%d apps built, want 1", len(built))
	}
	if built[0].origin != "provision" || built[0].subject != "mtls-user" {
		t.Errorf("app got %+v, want provisioned state for mtls-user", built[0])
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

	built := rig.sawBuilt()
	if len(built) != 1 {
		t.Fatalf("%d apps built, want 1", len(built))
	}
	if built[0].origin != "provision" || built[0].subject != "ssh-user" {
		t.Errorf("app got %+v, want provisioned state for ssh-user", built[0])
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
