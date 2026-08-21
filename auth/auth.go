package auth

import (
	"context"
	"errors"
	"net/netip"
	"time"
)

// Sentinels callers branch on. Everything a caller can see about a failed
// authentication is ErrUnauthenticated: a more specific outward error would be
// an oracle telling an attacker which factor failed (ADR-0001 §2.2). The detail
// goes to the audit record instead.
var (
	// ErrUnauthenticated is the ONLY outward failure of Policy.Authenticate.
	ErrUnauthenticated = errors.New("auth: unauthenticated")

	// ErrNoIdentityProof is returned by NewPolicy when a tree could be
	// satisfied without proving an identity — a policy that admits on
	// contextual factors alone (ADR-0001 §2.2.2).
	ErrNoIdentityProof = errors.New("auth: policy has no identity-bearing proof")

	// ErrEmptyPolicy is returned by NewPolicy for a nil root.
	ErrEmptyPolicy = errors.New("auth: policy has no root")
)

// FactorKind says what a factor proves. A contextual factor constrains an
// identity; it never establishes one.
type FactorKind uint8

const (
	// FactorContextual proves something about the request, not about who made
	// it: an address allowlist, a time window. Its Contribution MUST carry no
	// Subject, and a policy can never be satisfied by these alone.
	FactorContextual FactorKind = iota

	// FactorIdentity establishes a principal. Its Contribution MUST carry a
	// non-empty Subject.
	FactorIdentity
)

func (k FactorKind) String() string {
	if k == FactorIdentity {
		return "identity"
	}
	return "contextual"
}

// Request is the transport-agnostic material a factor may inspect. It is
// deliberately not net/http: an RPC server, a WebSocket handshake and a CLI
// prompt all construct one (ADR-0001 §2.1, §2.8).
type Request struct {
	// Peer is the immediate peer address — the transport's own view, never a
	// header. auth/ipallow decides whether any forwarded header may override
	// it, and only with a configured trusted-proxy set.
	Peer netip.AddrPort

	// Metadata is transport metadata: HTTP headers, RPC metadata. Keys should
	// be canonicalized by the adapter.
	Metadata map[string][]string

	// Credentials are the presented secrets, keyed by scheme ("ticket",
	// "password", …). Values are Secret so they cannot be printed by accident.
	Credentials map[string]Secret

	// TLS is the completed handshake state, or nil. auth/mtls requires a
	// verified chain here — presence of a peer certificate is not enough.
	TLS *TLSState
}

// TLSState is the projection of tls.ConnectionState that authentication needs.
// The core keeps its own view so a caller who never uses mTLS does not pull
// crypto/tls into their build; auth/mtls supplies the adapter.
type TLSState struct {
	// VerifiedChains is non-empty ONLY when the peer certificate chained to a
	// configured client-auth root. auth/mtls accepts nothing else — a peer
	// certificate on its own proves nothing, since any self-signed certificate
	// is presented exactly the same way (ADR-0001 §2.6a).
	VerifiedChains [][]Certificate
}

// Certificate is the certificate view auth/mtls needs. It carries every field a
// subject mapping might reasonably use, so choosing that mapping stays the
// factor's decision and never the adapter's.
type Certificate struct {
	CommonName     string
	DNSNames       []string
	EmailAddresses []string
	URIs           []string
	SerialNumber   string
	NotBefore      time.Time
	NotAfter       time.Time

	// IsClientAuth reports whether the certificate carries the client-auth
	// extended key usage. A certificate issued for something else must not
	// authenticate a client.
	IsClientAuth bool
}

// Contribution is what one factor proved. Subject is required when the factor
// declares FactorIdentity and MUST be empty when it declares FactorContextual;
// Policy validates that at evaluation time, so the classification a tree was
// built with cannot drift from the one it runs with (ADR-0001 §2.1).
//
// It carries no Kind of its own — duplicating the classification is exactly
// that drift.
type Contribution struct {
	Method    string
	Subject   string
	IssuedAt  time.Time
	ExpiresAt time.Time // zero: this proof imposes no post-authentication bound
}

// Factor is one leaf check. Verify returns a Contribution rather than an
// Identity because a contextual factor has no identity to return.
//
// INVARIANT: a non-nil error means the ZERO Contribution.
type Factor interface {
	Verify(ctx context.Context, r *Request) (Contribution, error)
	Kind() FactorKind
}

// Proof records one contributing factor in the final Identity.
type Proof struct {
	Method string
	Kind   FactorKind
}

// Identity is what a validated Policy concludes.
type Identity struct {
	Subject   string
	Proofs    []Proof
	IssuedAt  time.Time // LATEST non-zero contributing value
	ExpiresAt time.Time // MINIMUM finite non-zero value; zero means unbounded
}

// Policy is a validated tree and the only thing callers invoke.
//
// INVARIANT: a non-nil error means a nil *Identity; a nil error means a
// non-nil *Identity with a non-empty Subject.
type Policy interface {
	Authenticate(ctx context.Context, r *Request) (*Identity, error)
}
