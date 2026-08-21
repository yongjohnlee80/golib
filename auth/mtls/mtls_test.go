package mtls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
)

// --- real certificates, so "verified" means what the TLS stack means ---------

type ca struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newCA(t *testing.T, name string) ca {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return ca{cert: cert, key: key}
}

type leafOpts struct {
	cn        string
	dns       []string
	email     []string
	uri       string
	eku       []x509.ExtKeyUsage
	notBefore time.Time
	notAfter  time.Time
	selfSign  bool
}

func newLeaf(t *testing.T, issuer ca, o leafOpts) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nb, na := o.notBefore, o.notAfter
	if nb.IsZero() {
		nb = time.Now().Add(-time.Hour)
	}
	if na.IsZero() {
		na = time.Now().Add(time.Hour)
	}
	eku := o.eku
	if eku == nil {
		eku = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	tmpl := &x509.Certificate{
		SerialNumber:   big.NewInt(time.Now().UnixNano()),
		Subject:        pkix.Name{CommonName: o.cn},
		DNSNames:       o.dns,
		EmailAddresses: o.email,
		NotBefore:      nb,
		NotAfter:       na,
		ExtKeyUsage:    eku,
		KeyUsage:       x509.KeyUsageDigitalSignature,
	}
	if o.uri != "" {
		u, err := url.Parse(o.uri)
		if err != nil {
			t.Fatal(err)
		}
		tmpl.URIs = []*url.URL{u}
	}
	parent, signer := issuer.cert, issuer.key
	if o.selfSign {
		parent, signer = tmpl, key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// verifyAsServerWould runs the same x509 verification a TLS server performs for
// a client certificate, and returns the ConnectionState the handshake would
// leave behind. This is what makes the tests meaningful: VerifiedChains is
// populated by real verification, not by hand.
func verifyAsServerWould(t *testing.T, roots *x509.CertPool, leaf *x509.Certificate) *tls.ConnectionState {
	t.Helper()
	cs := &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}
	chains, err := leaf.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err == nil {
		cs.VerifiedChains = chains
	}
	return cs
}

func poolOf(c *x509.Certificate) *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(c)
	return p
}

func request(cs *tls.ConnectionState) *auth.Request {
	return &auth.Request{TLS: FromConnectionState(cs)}
}

// --- the criterion-9b matrix ------------------------------------------------

func TestVerify_ChainCases(t *testing.T) {
	t.Parallel()

	issuer := newCA(t, "test-ca")
	other := newCA(t, "other-ca")
	roots := poolOf(issuer.cert)

	cases := []struct {
		name    string
		leaf    *x509.Certificate
		roots   *x509.CertPool
		wantErr error
	}{
		{
			name:  "valid chain under the configured root",
			leaf:  newLeaf(t, issuer, leafOpts{cn: "alice"}),
			roots: roots,
		},
		{
			// The case the package exists for: a certificate is presented, but
			// it verifies against nothing.
			name:    "self-signed presents fine and verifies not at all",
			leaf:    newLeaf(t, issuer, leafOpts{cn: "attacker", selfSign: true}),
			roots:   roots,
			wantErr: ErrNoVerifiedChain,
		},
		{
			name:    "issued by a different CA",
			leaf:    newLeaf(t, other, leafOpts{cn: "alice"}),
			roots:   roots,
			wantErr: ErrNoVerifiedChain,
		},
		{
			// A server-auth certificate must not authenticate a client. x509
			// verification rejects it under the client-auth key usage, so no
			// chain is produced at all.
			name:    "wrong EKU (server auth)",
			leaf:    newLeaf(t, issuer, leafOpts{cn: "alice", eku: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}),
			roots:   roots,
			wantErr: ErrNoVerifiedChain,
		},
		{
			name:    "expired",
			leaf:    newLeaf(t, issuer, leafOpts{cn: "alice", notBefore: time.Now().Add(-48 * time.Hour), notAfter: time.Now().Add(-time.Hour)}),
			roots:   roots,
			wantErr: ErrNoVerifiedChain, // x509 rejects it before we see it
		},
	}
	f := New(SubjectFromCommonName)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cs := verifyAsServerWould(t, c.roots, c.leaf)
			got, err := f.Verify(context.Background(), request(cs))
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if c.wantErr == nil {
				if got.Subject != "alice" {
					t.Errorf("Subject = %q, want alice", got.Subject)
				}
				if !got.ExpiresAt.Equal(c.leaf.NotAfter) {
					t.Errorf("ExpiresAt = %v, want the leaf's NotAfter %v", got.ExpiresAt, c.leaf.NotAfter)
				}
			}
		})
	}
}

// No TLS at all, and a peer certificate that never verified, must be
// indistinguishable from the factor's point of view.
func TestVerify_NoVerifiedChain(t *testing.T) {
	t.Parallel()

	f := New(SubjectFromCommonName)
	issuer := newCA(t, "ca")
	leaf := newLeaf(t, issuer, leafOpts{cn: "alice"})

	for name, r := range map[string]*auth.Request{
		"nil request":  nil,
		"no TLS":       {},
		"nil state":    {TLS: nil},
		"empty chains": {TLS: &auth.TLSState{}},
		"peer certificate only, unverified": {TLS: FromConnectionState(
			&tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}})},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.Verify(context.Background(), r); !errors.Is(err, ErrNoVerifiedChain) {
				t.Errorf("err = %v, want ErrNoVerifiedChain", err)
			}
		})
	}
}

// The EKU check inside the factor is a second gate, exercised directly: a
// verified chain whose leaf lacks client-auth must still be refused.
func TestVerify_EKUCheckedIndependently(t *testing.T) {
	t.Parallel()

	f := New(SubjectFromCommonName)
	r := &auth.Request{TLS: &auth.TLSState{VerifiedChains: [][]auth.Certificate{{
		{CommonName: "alice", NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsClientAuth: false},
	}}}}
	if _, err := f.Verify(context.Background(), r); !errors.Is(err, ErrNotClientAuth) {
		t.Errorf("err = %v, want ErrNotClientAuth", err)
	}
}

func TestVerify_ValidityWindow(t *testing.T) {
	t.Parallel()

	base := time.Now()
	valid := auth.Certificate{
		CommonName: "alice", IsClientAuth: true,
		NotBefore: base.Add(-time.Hour), NotAfter: base.Add(time.Hour),
	}
	r := &auth.Request{TLS: &auth.TLSState{VerifiedChains: [][]auth.Certificate{{valid}}}}

	t.Run("not yet valid", func(t *testing.T) {
		f := New(SubjectFromCommonName, Clock(func() time.Time { return base.Add(-2 * time.Hour) }))
		if _, err := f.Verify(context.Background(), r); !errors.Is(err, ErrNotYetValid) {
			t.Errorf("err = %v, want ErrNotYetValid", err)
		}
	})
	t.Run("expired", func(t *testing.T) {
		f := New(SubjectFromCommonName, Clock(func() time.Time { return base.Add(2 * time.Hour) }))
		if _, err := f.Verify(context.Background(), r); !errors.Is(err, ErrExpired) {
			t.Errorf("err = %v, want ErrExpired", err)
		}
	})
	t.Run("inside the window", func(t *testing.T) {
		f := New(SubjectFromCommonName, Clock(func() time.Time { return base }))
		if _, err := f.Verify(context.Background(), r); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})
}

func TestSubjectMappings(t *testing.T) {
	t.Parallel()

	issuer := newCA(t, "ca")
	roots := poolOf(issuer.cert)
	leaf := newLeaf(t, issuer, leafOpts{
		cn:    "alice",
		dns:   []string{"alice.internal"},
		email: []string{"alice@example.com"},
		uri:   "spiffe://example.com/alice",
	})
	r := request(verifyAsServerWould(t, roots, leaf))

	cases := map[string]struct {
		fn   SubjectFunc
		want string
	}{
		"common name": {SubjectFromCommonName, "alice"},
		"dns san":     {SubjectFromDNSName, "alice.internal"},
		"email san":   {SubjectFromEmail, "alice@example.com"},
		"uri san":     {SubjectFromURI, "spiffe://example.com/alice"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := New(c.fn).Verify(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if got.Subject != c.want {
				t.Errorf("Subject = %q, want %q", got.Subject, c.want)
			}
		})
	}

	t.Run("a mapping that finds nothing fails", func(t *testing.T) {
		bare := newLeaf(t, issuer, leafOpts{cn: "no-sans"})
		br := request(verifyAsServerWould(t, roots, bare))
		if _, err := New(SubjectFromDNSName).Verify(context.Background(), br); !errors.Is(err, ErrNoSubject) {
			t.Errorf("err = %v, want ErrNoSubject", err)
		}
	})

	t.Run("a custom mapping returning empty fails", func(t *testing.T) {
		empty := func(auth.Certificate) (string, error) { return "", nil }
		if _, err := New(empty).Verify(context.Background(), r); !errors.Is(err, ErrNoSubject) {
			t.Errorf("err = %v, want ErrNoSubject", err)
		}
	})
}

// The identity mapping must be explicit — there is no default.
func TestNew_RequiresSubjectFunc(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("New(nil) must panic: the identity mapping cannot be implicit")
		}
	}()
	_ = New(nil)
}

// The adapter must not carry unverified certificates across, so no future
// reader can authenticate from PeerCertificates.
func TestFromConnectionState_CarriesOnlyVerifiedChains(t *testing.T) {
	t.Parallel()

	issuer := newCA(t, "ca")
	leaf := newLeaf(t, issuer, leafOpts{cn: "alice"})
	st := FromConnectionState(&tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}})
	if st == nil {
		t.Fatal("nil state")
	}
	if len(st.VerifiedChains) != 0 {
		t.Error("an unverified peer certificate must not appear in the projection")
	}
	if FromConnectionState(nil) != nil {
		t.Error("nil ConnectionState must project to nil")
	}
}

func TestFromX509_ProjectsFields(t *testing.T) {
	t.Parallel()

	issuer := newCA(t, "ca")
	leaf := newLeaf(t, issuer, leafOpts{
		cn: "alice", dns: []string{"a.internal"}, email: []string{"a@example.com"},
		uri: "spiffe://example.com/a",
	})
	got := FromX509(leaf)
	if got.CommonName != "alice" || len(got.DNSNames) != 1 || len(got.EmailAddresses) != 1 || len(got.URIs) != 1 {
		t.Errorf("projection lost fields: %+v", got)
	}
	if !got.IsClientAuth {
		t.Error("client-auth EKU not projected")
	}
	if got.SerialNumber == "" {
		t.Error("serial not projected")
	}
	// auth.Certificate holds slices, so it is not comparable with == — a
	// compile error rather than a runtime panic, which is the right way round.
	if z := FromX509(nil); z.CommonName != "" || z.SerialNumber != "" ||
		len(z.DNSNames) != 0 || len(z.EmailAddresses) != 0 || len(z.URIs) != 0 ||
		!z.NotBefore.IsZero() || !z.NotAfter.IsZero() || z.IsClientAuth {
		t.Errorf("nil certificate must project to the zero value, got %+v", z)
	}
}

// mTLS is identity-bearing, so it can stand alone as a policy root — unlike
// ipallow.
func TestFactor_IsIdentityBearing(t *testing.T) {
	t.Parallel()

	f := New(SubjectFromCommonName)
	if f.Kind() != auth.FactorIdentity {
		t.Fatal("mtls must be identity-bearing")
	}
	if _, err := auth.NewPolicy(auth.Leaf(f)); err != nil {
		t.Errorf("mtls alone must form a valid policy: %v", err)
	}
}
