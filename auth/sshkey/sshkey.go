// Package sshkey authenticates a client by an SSH signature over a
// server-issued challenge (ADR-0001 §2.5).
//
// # Why this shape
//
// A browser cannot read ~/.ssh, so "authenticate with an SSH key" has to mean
// something concrete. Here it means: the server issues a single-use, expiring,
// domain-separated challenge; the user signs it locally — `ssh-keygen -Y sign`,
// or an agent — and submits the SIGNATURE. Key material never moves.
//
// NEVER ask a user to paste a private key into a browser, a form, or a prompt.
// A signature over a nonce proves possession without transmitting the secret,
// which is the entire point.
//
// # Who checks the signature
//
// Verification sits behind [Verifier], with two implementations:
//
//   - [OpenSSH] shells out to `ssh-keygen -Y verify`. This is the DEFAULT
//     recommendation. SSHSIG is an OpenSSH format with no Go implementation in
//     x/crypto, so the alternative is our own parser competing with the
//     reference one; delegating also inherits allowed_signers semantics —
//     principals, validity windows, cert-authority lines — that we would
//     otherwise have to reimplement to match.
//   - [PureGo] parses the envelope in-process, for images with no ssh-keygen.
//
// The two are interchangeable in meaning: the client claims an identity, and
// the signature must verify under a key that identity holds. They differ only
// in reach — PureGo understands a plain allowed-key set and nothing more.
//
// The envelope parser backing PureGo is held to a fixed shape, checked field by
// field, and tested against tampered, truncated, and wrong-namespace inputs.
// Cross-implementation tests assert both verifiers agree on the same inputs.
package sshkey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/yongjohnlee80/golib/auth"
)

// Internal errors; auth.Policy maps every failure to auth.ErrUnauthenticated.
var (
	ErrMalformed        = errors.New("sshkey: malformed signature envelope")
	ErrBadSignature     = errors.New("sshkey: signature does not verify")
	ErrNamespace        = errors.New("sshkey: signature namespace does not match")
	ErrHashAlgorithm    = errors.New("sshkey: unsupported signature hash algorithm")
	ErrNoChallenge      = errors.New("sshkey: no challenge presented")
	ErrChallengeUnknown = errors.New("sshkey: unknown or already-consumed challenge")
	ErrChallengeExpired = errors.New("sshkey: challenge expired")
	ErrBinding          = errors.New("sshkey: challenge binding does not match the request")
	ErrNoAllowedKeys    = errors.New("sshkey: empty allowed-key set denies")
	ErrNoIdentity       = errors.New("sshkey: no identity claimed")
)

// Allowed is one entry of the allowed-key set: a public key and the principal it
// proves.
type Allowed struct {
	Key     ssh.PublicKey
	Subject string
}

// ParseAuthorizedKeys parses an authorized_keys file. The subject for each key
// comes from subjectOf; pass SubjectFromComment for the common case of using
// the trailing comment as the principal.
//
// A line that fails to parse is an error, not a skip: silently ignoring an
// unparsable entry in an allowlist means silently denying someone who should
// have access, and the operator would never know.
func ParseAuthorizedKeys(b []byte, subjectOf func(comment string, key ssh.PublicKey) (string, error)) ([]Allowed, error) {
	if subjectOf == nil {
		return nil, errors.New("sshkey: a subject mapping is required")
	}
	var out []Allowed
	rest := b
	for line := 1; ; line++ {
		key, comment, _, remaining, err := ssh.ParseAuthorizedKey(rest)
		if err != nil {
			if onlyBlank(rest) {
				return out, nil
			}
			return nil, fmt.Errorf("sshkey: authorized_keys line %d: %w", line, err)
		}
		subject, err := subjectOf(comment, key)
		if err != nil {
			return nil, fmt.Errorf("sshkey: authorized_keys line %d: %w", line, err)
		}
		if subject == "" {
			return nil, fmt.Errorf("sshkey: authorized_keys line %d: empty subject", line)
		}
		out = append(out, Allowed{Key: key, Subject: subject})
		rest = remaining
	}
}

// SubjectFromComment uses the key's trailing comment as the principal.
func SubjectFromComment(comment string, _ ssh.PublicKey) (string, error) {
	c := strings.TrimSpace(comment)
	if c == "" {
		return "", errors.New("key has no comment to use as a subject")
	}
	return c, nil
}

func onlyBlank(b []byte) bool {
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if t != "" && !strings.HasPrefix(t, "#") {
			return false
		}
	}
	return true
}

// Factor verifies an SSH signature over a challenge it previously issued. It is
// identity-bearing.
//
// The verification itself is delegated to a [Verifier] — [OpenSSH] by default
// in practice, [PureGo] where no ssh-keygen exists. Factor owns the parts that
// are ours regardless of who checks the signature: issuing and atomically
// consuming the challenge, enforcing the bindings, and building the exact bytes
// that were to be signed.
type Factor struct {
	verifier  Verifier
	namespace string
	store     ChallengeStore
	now       func() time.Time
	sigKey    string
	chalKey   string
	idKey     string
}

// Option configures a Factor or Challenger.
type Option func(*settings)

type settings struct {
	namespace string
	now       func() time.Time
	sigKey    string
	chalKey   string
	idKey     string
}

// Namespace sets the SSHSIG namespace — the domain separation that stops a
// signature made for one purpose being replayed as another. It MUST be set:
// see New.
func Namespace(ns string) Option { return func(s *settings) { s.namespace = ns } }

// Clock overrides the time source, for tests.
func Clock(fn func() time.Time) Option { return func(s *settings) { s.now = fn } }

// CredentialKeys sets the auth.Request credential keys the signature, challenge
// id, and claimed identity are read from. Defaults "ssh-signature",
// "ssh-challenge", "ssh-identity".
func CredentialKeys(signature, challenge, identity string) Option {
	return func(s *settings) { s.sigKey, s.chalKey, s.idKey = signature, challenge, identity }
}

func resolve(opts []Option) settings {
	s := settings{now: time.Now, sigKey: "ssh-signature", chalKey: "ssh-challenge", idKey: "ssh-identity"}
	for _, o := range opts {
		if o != nil {
			o(&s)
		}
	}
	return s
}

// New builds the verifying factor over v.
//
// A Namespace is REQUIRED. Without domain separation a signature produced for
// one purpose — a git commit, another service's login — could be replayed here,
// so an empty namespace is a programming error rather than a default.
//
// A nil Verifier panics rather than defaulting: "which implementation checks my
// signatures" is not a question to answer silently.
func New(v Verifier, store ChallengeStore, opts ...Option) *Factor {
	s := resolve(opts)
	if s.namespace == "" {
		panic("sshkey.New: a Namespace is required — without it a signature from another purpose can be replayed")
	}
	if v == nil {
		panic("sshkey.New: a Verifier is required — pass sshkey.OpenSSH{...} or sshkey.PureGo{...}")
	}
	if store == nil {
		panic("sshkey.New: a ChallengeStore is required")
	}
	return &Factor{verifier: v, namespace: s.namespace, store: store, now: s.now, sigKey: s.sigKey, chalKey: s.chalKey, idKey: s.idKey}
}

// Kind reports auth.FactorIdentity.
func (f *Factor) Kind() auth.FactorKind { return auth.FactorIdentity }

// Verify consumes the presented challenge, rebuilds the exact bytes that were
// to be signed, and hands them to the Verifier along with the CLAIMED identity.
//
// Order matters: the challenge is consumed ATOMICALLY first, so a replayed
// signature cannot be retried against the same nonce even in parallel.
//
// The identity arrives from the client and is therefore untrusted — right up
// until the verifier returns nil, which is exactly the proof that the claim was
// true. Nothing before that line may treat it as a fact, and nothing after it
// needs to doubt it.
func (f *Factor) Verify(ctx context.Context, r *auth.Request) (auth.Contribution, error) {
	if r == nil {
		return auth.Contribution{}, ErrMalformed
	}
	chalID, ok := r.Credentials[f.chalKey]
	if !ok || chalID.IsZero() {
		return auth.Contribution{}, ErrNoChallenge
	}
	sigArmor, ok := r.Credentials[f.sigKey]
	if !ok || sigArmor.IsZero() {
		return auth.Contribution{}, ErrMalformed
	}
	claimed, ok := r.Credentials[f.idKey]
	if !ok || claimed.IsZero() {
		return auth.Contribution{}, ErrNoIdentity
	}
	identity := claimed.Reveal()
	if err := validIdentity(identity); err != nil {
		return auth.Contribution{}, err
	}

	// Consumed before the signature is looked at: a nonce spends on
	// presentation, not on success. Otherwise a wrong guess would leave the
	// challenge live for another try.
	rec, err := f.store.Consume(chalID.Reveal(), f.now())
	if err != nil {
		return auth.Contribution{}, err
	}
	if err := rec.matches(r); err != nil {
		return auth.Contribution{}, err
	}

	if err := f.verifier.VerifySignature(ctx, rec.Message(), []byte(sigArmor.Reveal()), f.namespace, identity); err != nil {
		return auth.Contribution{}, err
	}
	return auth.Contribution{
		Method:    "sshkey",
		Subject:   identity, // proven by the line above, not before it
		IssuedAt:  rec.IssuedAt,
		ExpiresAt: time.Time{}, // a signature is a point-in-time proof: it bounds nothing
	}, nil
}

func keysEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// --- the SSHSIG envelope ----------------------------------------------------

const (
	sigMagic     = "SSHSIG"
	sigPEMType   = "SSH SIGNATURE"
	sigVersion   = 1
	maxArmorSize = 16 << 10
)

// envelope is the parsed SSHSIG blob.
type envelope struct {
	PublicKey ssh.PublicKey
	Namespace string
	HashAlgo  string
	Signature *ssh.Signature
}

// wire mirrors the SSHSIG body layout exactly:
//
//	byte[6]   MAGIC "SSHSIG"
//	uint32    version
//	string    publickey
//	string    namespace
//	string    reserved
//	string    hash_algorithm
//	string    signature
type wire struct {
	Version   uint32
	PublicKey []byte
	Namespace string
	Reserved  string
	HashAlgo  string
	Signature []byte
}

// signedData mirrors the bytes an SSHSIG signature actually covers:
//
//	MAGIC || string namespace || string reserved || string hash_algorithm
//	      || string H(message)
type signedData struct {
	Namespace string
	Reserved  string
	HashAlgo  string
	Hash      []byte
}

// parseEnvelope decodes the armored signature `ssh-keygen -Y sign` produces.
//
// Every field is checked rather than skipped: a lenient parser here would be a
// way to smuggle a signature made under different terms.
func parseEnvelope(armored []byte) (*envelope, error) {
	if len(armored) == 0 || len(armored) > maxArmorSize {
		return nil, ErrMalformed
	}
	block, _ := pem.Decode(armored)
	if block == nil || block.Type != sigPEMType {
		return nil, ErrMalformed
	}
	body := block.Bytes
	if len(body) < len(sigMagic) || string(body[:len(sigMagic)]) != sigMagic {
		return nil, ErrMalformed
	}
	var w wire
	if err := ssh.Unmarshal(body[len(sigMagic):], &w); err != nil {
		return nil, ErrMalformed
	}
	if w.Version != sigVersion {
		return nil, ErrMalformed
	}
	if w.Reserved != "" {
		// The field is reserved; a non-empty value means terms we do not
		// understand, so refuse rather than ignore.
		return nil, ErrMalformed
	}
	pub, err := ssh.ParsePublicKey(w.PublicKey)
	if err != nil {
		return nil, ErrMalformed
	}
	sig, err := parseSignatureBlob(w.Signature)
	if err != nil {
		return nil, err
	}
	return &envelope{PublicKey: pub, Namespace: w.Namespace, HashAlgo: w.HashAlgo, Signature: sig}, nil
}

// parseSignatureBlob decodes an ssh signature blob into x/crypto's type, so the
// actual verification is delegated.
func parseSignatureBlob(b []byte) (*ssh.Signature, error) {
	var s struct {
		Format string
		Blob   []byte
		Rest   []byte `ssh:"rest"`
	}
	if err := ssh.Unmarshal(b, &s); err != nil {
		return nil, ErrMalformed
	}
	if s.Format == "" || len(s.Blob) == 0 {
		return nil, ErrMalformed
	}
	return &ssh.Signature{Format: s.Format, Blob: s.Blob, Rest: s.Rest}, nil
}

// verify hashes the message under the envelope's declared algorithm and checks
// the signature. The hash algorithm is allowlisted: accepting whatever the
// signer names would let a weak digest be chosen for us.
func (e *envelope) verify(message []byte) error {
	var digest []byte
	switch e.HashAlgo {
	case "sha256":
		h := sha256.Sum256(message)
		digest = h[:]
	case "sha512":
		h := sha512.Sum512(message)
		digest = h[:]
	default:
		return ErrHashAlgorithm
	}
	blob := ssh.Marshal(signedData{
		Namespace: e.Namespace,
		Reserved:  "",
		HashAlgo:  e.HashAlgo,
		Hash:      digest,
	})
	if err := e.PublicKey.Verify(append([]byte(sigMagic), blob...), e.Signature); err != nil {
		return ErrBadSignature
	}
	return nil
}

// --- challenges -------------------------------------------------------------

// Challenge is what the server hands out. ID identifies it; Message is the
// exact bytes the user must sign.
type Challenge struct {
	ID      string
	Message []byte
	Expires time.Time
}

// Binding ties a challenge to the context it was issued for, so a signature
// obtained for one session or origin cannot be presented for another
// (ADR-0001 §2.5).
type Binding struct {
	Session string
	Origin  string
}

// ChallengeRecord is a stored challenge.
type ChallengeRecord struct {
	Nonce    []byte
	Binding  Binding
	IssuedAt time.Time
	Expires  time.Time
}

// Message returns the exact bytes to be signed: the nonce plus the bindings, so
// the signature covers them and none can be swapped after the fact.
func (c ChallengeRecord) Message() []byte {
	return ssh.Marshal(struct {
		Nonce   []byte
		Session string
		Origin  string
	}{c.Nonce, c.Binding.Session, c.Binding.Origin})
}

// matches checks the request against the bindings the challenge was issued for.
func (c ChallengeRecord) matches(r *auth.Request) error {
	if c.Binding.Origin != "" && c.Binding.Origin != originOf(r) {
		return ErrBinding
	}
	if c.Binding.Session != "" {
		presented := ""
		if s, ok := r.Credentials["session"]; ok {
			presented = s.Reveal()
		}
		if presented != c.Binding.Session {
			return ErrBinding
		}
	}
	return nil
}

func originOf(r *auth.Request) string {
	for k, v := range r.Metadata {
		if strings.EqualFold(k, "Origin") && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// ChallengeStore holds outstanding challenges.
//
// Consume MUST be ONE atomic operation — fetch, validate, remove — so a nonce
// cannot be redeemed twice even by simultaneous attempts (ADR-0001 §2.5).
type ChallengeStore interface {
	Put(id string, rec ChallengeRecord) error
	Consume(id string, now time.Time) (ChallengeRecord, error)
}

// Challenger issues challenges into a store.
type Challenger struct {
	store ChallengeStore
	now   func() time.Time
	ttl   time.Duration
}

// NewChallenger builds an issuer. ttl bounds how long a challenge may be
// redeemed; it is NOT a session lifetime.
func NewChallenger(store ChallengeStore, ttl time.Duration, opts ...Option) *Challenger {
	s := resolve(opts)
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	return &Challenger{store: store, now: s.now, ttl: ttl}
}

// Issue mints a challenge bound to b.
func (c *Challenger) Issue(b Binding) (Challenge, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Challenge{}, err
	}
	var idb [16]byte
	if _, err := rand.Read(idb[:]); err != nil {
		return Challenge{}, err
	}
	id := base64.RawURLEncoding.EncodeToString(idb[:])
	now := c.now()
	rec := ChallengeRecord{Nonce: nonce[:], Binding: b, IssuedAt: now, Expires: now.Add(c.ttl)}
	if err := c.store.Put(id, rec); err != nil {
		return Challenge{}, err
	}
	return Challenge{ID: id, Message: rec.Message(), Expires: rec.Expires}, nil
}

// Claim implements auth.Claimant: it names the principal this request claims,
// without verifying anything.
//
// The value is the same unverified claim Verify screens and then proves. An
// invalid one yields "" so a throttle counts it against the address rather than
// creating a counter keyed by junk.
func (f *Factor) Claim(r *auth.Request) string {
	if r == nil {
		return ""
	}
	c, ok := r.Credentials[f.idKey]
	if !ok || c.IsZero() {
		return ""
	}
	v := c.Reveal()
	if validIdentity(v) != nil {
		return ""
	}
	return v
}

var _ auth.Claimant = (*Factor)(nil)
