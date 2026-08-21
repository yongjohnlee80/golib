// Package mtls authenticates a client by its TLS certificate — but only a
// certificate that VERIFIED against configured client-auth roots (ADR-0001
// §2.6a).
//
// The distinction is the whole package: any peer can present a certificate, and
// a self-signed one arrives looking exactly like a real one. Only
// tls.ConnectionState.VerifiedChains — populated by the TLS stack when the chain
// validated under the roots the server configured — means anything.
package mtls

import (
	"context"
	"time"

	"github.com/yongjohnlee80/golib/auth"
)

// Internal errors; auth.Policy maps every failure to auth.ErrUnauthenticated.
var (
	// ErrNoVerifiedChain covers both "no TLS" and the dangerous case: a peer
	// certificate that did not verify.
	ErrNoVerifiedChain = auth.Reason("mtls: no verified client-certificate chain")
	ErrNotClientAuth   = auth.Reason("mtls: leaf certificate lacks client-auth extended key usage")
	ErrNotYetValid     = auth.Reason("mtls: certificate not yet valid")
	ErrExpired         = auth.Reason("mtls: certificate expired")
	ErrNoSubject       = auth.Reason("mtls: subject mapping produced no subject")
)

// SubjectFunc maps a verified leaf certificate to a principal.
//
// There is deliberately NO default. Defaulting to the Common Name would bake in
// a choice that is wrong for most modern PKI — CN has been deprecated for
// identity for years — and a silent identity mapping is precisely the kind of
// decision that should be visible at the call site.
type SubjectFunc func(auth.Certificate) (string, error)

// SubjectFromCommonName maps the leaf's CN. Convenient for a small internal CA;
// prefer a SAN-based mapping where the PKI provides one.
func SubjectFromCommonName(c auth.Certificate) (string, error) {
	if c.CommonName == "" {
		return "", ErrNoSubject
	}
	return c.CommonName, nil
}

// SubjectFromDNSName maps the leaf's first DNS SAN.
func SubjectFromDNSName(c auth.Certificate) (string, error) {
	if len(c.DNSNames) == 0 {
		return "", ErrNoSubject
	}
	return c.DNSNames[0], nil
}

// SubjectFromEmail maps the leaf's first email SAN.
func SubjectFromEmail(c auth.Certificate) (string, error) {
	if len(c.EmailAddresses) == 0 {
		return "", ErrNoSubject
	}
	return c.EmailAddresses[0], nil
}

// SubjectFromURI maps the leaf's first URI SAN — the SPIFFE-style shape.
func SubjectFromURI(c auth.Certificate) (string, error) {
	if len(c.URIs) == 0 {
		return "", ErrNoSubject
	}
	return c.URIs[0], nil
}

// Factor authenticates by verified client certificate. It is identity-bearing.
type Factor struct {
	subject SubjectFunc
	now     func() time.Time
}

// Option configures a Factor.
type Option func(*Factor)

// Clock overrides the time source, for tests.
func Clock(fn func() time.Time) Option { return func(f *Factor) { f.now = fn } }

// New builds the factor. subject is REQUIRED: see SubjectFunc for why there is
// no default.
func New(subject SubjectFunc, opts ...Option) *Factor {
	if subject == nil {
		panic("mtls.New: a SubjectFunc is required — the identity mapping must be explicit")
	}
	f := &Factor{subject: subject, now: time.Now}
	for _, o := range opts {
		if o != nil {
			o(f)
		}
	}
	return f
}

// Kind reports auth.FactorIdentity.
func (f *Factor) Kind() auth.FactorKind { return auth.FactorIdentity }

// Verify requires a verified chain, the client-auth EKU, a currently-valid
// leaf, and a non-empty mapped subject.
//
// The Contribution's ExpiresAt is the leaf's NotAfter: an identity proved by a
// certificate cannot outlive it (ADR-0001 §2.6a).
func (f *Factor) Verify(_ context.Context, r *auth.Request) (auth.Contribution, error) {
	if r == nil || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
		return auth.Contribution{}, ErrNoVerifiedChain
	}
	chain := r.TLS.VerifiedChains[0]
	if len(chain) == 0 {
		return auth.Contribution{}, ErrNoVerifiedChain
	}
	leaf := chain[0]
	if !leaf.IsClientAuth {
		return auth.Contribution{}, ErrNotClientAuth
	}
	now := f.now()
	if !leaf.NotBefore.IsZero() && now.Before(leaf.NotBefore) {
		return auth.Contribution{}, ErrNotYetValid
	}
	if !leaf.NotAfter.IsZero() && !now.Before(leaf.NotAfter) {
		return auth.Contribution{}, ErrExpired
	}
	subject, err := f.subject(leaf)
	if err != nil {
		return auth.Contribution{}, err
	}
	if subject == "" {
		return auth.Contribution{}, ErrNoSubject
	}
	return auth.Contribution{
		Method:    "mtls",
		Subject:   subject,
		IssuedAt:  leaf.NotBefore,
		ExpiresAt: leaf.NotAfter,
	}, nil
}
