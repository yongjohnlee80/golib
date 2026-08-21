# ADR-0001 — `golib/auth`: composable authentication

- **Status:** **Proposed** (2026-08-21 — authored by jarvis from Johno's request
  for an auth package covering ssh keys, IP allowlists, passwords "and others".
  Awaiting design review; lands on `auth-pkg`.)
- **Date:** 2026-08-21
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none — a new subsystem.
- **Related:** `golib/tui` ADR-0009 §2.8 (the first consumer, which takes this
  package's `Authenticator` interface), the `golib` convention's philosophy #6
  "fail loud, fail typed" and its **tightened dependency rule** (2026-08-21),
  and RULES.md #1 (never store or log secret values).

## 1. Context

Johno, 2026-08-21: *"we should also create a basic auth package which
authenticates via common methods such as by ssh key, whitelisted IP addresses,
password and others"*, and for the first consumer: *"ideally we only want users
to connect to WebTUI via ssh keys or other secured manners only."*

golib has no authentication surface today. Its first real need is ADR-0009's web
backend, which must not invent its own credential handling — a TUI backend with
a bespoke token scheme is exactly how a keyboard-on-your-CLI vulnerability gets
shipped. Beyond that, `server/http`, `server/rpc` and `server/ws` all lack a
shared way to say "who is this".

The methods Johno named are not alternatives to each other — they **compose**
("allowlisted address AND (ssh key OR client certificate)"). That observation
drives the design more than any individual method does.

## 2. Decision

### 2.1 Shape: one interface, transport-agnostic

```go
// Authenticator proves who is making a request, or refuses.
type Authenticator interface {
	Authenticate(ctx context.Context, r *Request) (*Identity, error)
}

// Request is the transport-agnostic material an authenticator may inspect:
// the peer address, metadata (headers or RPC metadata), presented credentials,
// and TLS state when there is one. Deliberately NOT net/http, so an RPC server,
// a WebSocket handshake and a CLI prompt can all present one.
type Request struct { /* RemoteAddr netip.AddrPort; Metadata; Credentials; TLS */ }

// Identity is the outcome: who, proved how, and until when.
type Identity struct { /* Subject; Method; IssuedAt, ExpiresAt; Attributes */ }
```

The core package imports **stdlib + golib only** — no `net/http` (§2.7 provides
adapters).

### 2.2 Composition is a first-class feature

```go
func All(a ...Authenticator) Authenticator // every one must pass (AND)
func Any(a ...Authenticator) Authenticator // the first pass wins (OR)
```

Rules, all normative:

- **Deny by default.** `All()` and `Any()` with no members **deny**; an empty
  allowlist denies; a policy with no authenticator is a **construction error**
  (panic at construction, house rule), never allow-everything.
- **Documented evaluation order and short-circuit**: `All` evaluates in
  declaration order and stops at the first failure; `Any` stops at the first
  success. Order is caller-visible because it determines cost (put the cheap
  address check first).
- **The decision carries an internal reason for audit that is never returned to
  the client.** A caller-visible error must not reveal *which* factor failed —
  that is an oracle. One uniform `ErrUnauthenticated` outward, detail to the log.

### 2.3 Methods, and where each dependency lives

Per the tightened dependency rule: stdlib is checked first, and any third-party
import is isolated in a leaf subpackage with its justification recorded here.

| Package | Method | Dependency | Justification |
| --- | --- | --- | --- |
| `auth/ipallow` | CIDR allowlist | **stdlib** (`net/netip`) | — |
| `auth/token` | opaque session tokens: issue / verify / revoke / expire / single-use | **stdlib** (`crypto/rand`, `crypto/sha256`, `crypto/subtle`) | — |
| `auth/password` | hashed password verification | **stdlib** (`crypto/pbkdf2`, Go 1.24+) | §2.4 |
| `auth/totp` | TOTP second factor | **stdlib** (`crypto/hmac`, `encoding/base32`) | — |
| `auth/mtls` | client-certificate verification | **stdlib** (`crypto/tls`, `crypto/x509`) | — |
| `auth/sshkey` | `authorized_keys` parsing + SSH signature verification | `golang.org/x/crypto/ssh` | §2.5 |

Five of six are dependency-free. Only `auth/sshkey` adds a module.

### 2.4 Password hashing: stdlib PBKDF2, not argon2id

**Decided by the dependency rule.** Go 1.24 moved `crypto/pbkdf2` into the
standard library and this module targets Go 1.25.3, so PBKDF2-HMAC-SHA256 with a
high iteration count needs **no dependency at all**. `x/crypto`'s argon2id is
genuinely better — memory-hard, materially stronger against GPU cracking — but
the demand here is weak: passwords are explicitly **not** an accepted mechanism
for the first consumer (ADR-0009 §2.8), so this exists for lower-stakes callers.
"Convenience" and "somewhat better" are not the "absolutely good reason" the rule
requires.

Required regardless of primitive: per-credential random salt, tunable cost
parameters stored **with** the hash, constant-time comparison, and
**upgrade-on-successful-verify** so stored hashes can be strengthened later
without a password reset. If a future caller genuinely needs memory-hardness,
that is a new ADR and a leaf subpackage — the interface does not change.

### 2.5 SSH keys: `x/crypto/ssh` is justified, and the browser flow must be chosen

**The dependency is justified.** Verifying an SSH key means parsing
`authorized_keys`/`allowed_signers` and the SSH signature wire format
(`ssh-keygen -Y sign` emits an armored SSH signature blob). Hand-rolling that
parser is a security hazard of exactly the kind the rule carves out an exception
for, and `x/crypto` is Go-team maintained — the same provenance as the `x/term`
already in `go.mod` for `tui/term`.

**A browser cannot read `~/.ssh`, so "authenticate with an SSH key" needs a
mechanism.** Two honest candidates; §7 Q2 decides:

1. **Signed challenge.** The server issues a nonce; the user signs it locally
   (`ssh-keygen -Y sign`, or an agent) and submits the signature; the server
   verifies against its allowed set. No key material leaves the client; works
   with hardware-backed keys. Clunkier UX.
2. **Ticket minted over the existing SSH session.** The user can already SSH to
   the box, so the CLI prints a one-time high-entropy URL over that
   authenticated channel — **the SSH hop is the authentication** (the Jupyter /
   `code tunnel` shape). Simplest, hardest to get wrong, and the exact fit for
   ADR-0009's premise.

**Never acceptable, and stated in the package docs: asking a user to paste a
private key into a browser or a form.**

### 2.6 Hostile-input rules that are easy to get wrong

- **No user enumeration.** One uniform error and indistinguishable timing
  whether or not the subject exists. Verification runs against a dummy hash on
  an unknown subject so the work is symmetric.
- **Constant time.** `crypto/subtle` for every credential comparison; no early
  return on length mismatch.
- **Brute-force resistance.** Per-subject **and** per-source-address failure
  counters with exponential backoff and a lockout threshold, both configurable,
  both defaulting to on. Success resets. The counters are **bounded and
  evicted** — otherwise they are a memory-exhaustion vector reachable by an
  unauthenticated attacker, which would be a vulnerability introduced by the
  defense.
- **Client addresses are untrusted.** `X-Forwarded-For` / `X-Real-IP` are
  ignored **unless** a trusted-proxy set is configured, and the parse then takes
  the correct hop rather than the attacker-controlled first value. An address is
  a *factor*, never an identity: `ipallow` documents that it may only appear
  inside `All(...)`.
- **Tokens:** CSPRNG with a documented entropy floor, **stored hashed** (never
  plaintext at rest), constant-time lookup, mandatory expiry with a safe
  default, explicit revocation, and **single-use tickets as a distinct mode** —
  ADR-0009 §2.8 needs exactly that for the WS handshake.

### 2.7 Secrets never leak, structurally

Redaction is a **type**, not a convention: a wrapper whose `String`, `Format`
and `MarshalJSON` all return a placeholder, so `%v`, `%+v`, `log.Printf` and
JSON encoding cannot print a credential by accident. No credential material in
errors, panics, or logs (RULES.md #1). Logging is `logger.Logger` (default
`Nop`): attempt, success, failure, lockout — with subject and method, never the
secret.

### 2.8 Adapters, not framework coupling

The core stays free of `net/http`. Thin adapters live in subpackages: a
`net/http` middleware, and a helper usable from `server/rpc` / `server/ws`, so
one policy value serves a web handler, an RPC server and a CLI prompt.

## 3. What this package is not

Out of scope, named so the design does not drift into them: **OIDC/OAuth2,
SAML, JWT issuance or validation, LDAP/AD, WebAuthn/passkeys**, and user
*management* (registration, password reset, roles/RBAC). Each is a protocol
surface plus dependencies; each can arrive later as its own ADR and leaf
subpackage. This package answers one question — *does this request carry a valid
credential* — and returns an `Identity` or an error.

## 4. Alternatives considered

1. **A bag of unrelated helper functions** (`CheckPassword`, `CheckIP`, …) with
   composition left to the caller. Rejected: every consumer would re-implement
   the AND/OR logic, and the security-critical parts (deny-by-default, uniform
   error, no oracle) are exactly what gets skipped when hand-rolled per call
   site.
2. **`net/http`-shaped middleware only.** Rejected: it would exclude
   `server/rpc`, the WS handshake and CLI prompts, and would drag `net/http`
   into the core.
3. **`x/crypto` argon2id for passwords.** Rejected for now — §2.4.
4. **Rolling our own SSH signature parsing to stay dependency-free.** Rejected —
   §2.5. This is the case the dependency rule's exception exists for.
5. **Delegating everything to a reverse proxy** (oauth2-proxy, Tailscale or
   Cloudflare Access) and shipping no auth package. A legitimate production
   deployment and worth recommending in the README, but it cannot be the only
   answer: it does not help a self-contained CLI binary that a user runs on a
   box they already SSH into, which is precisely ADR-0009's case.

## 5. Files / acceptance

New: `auth/**` (core + the six method subpackages + adapters), `auth/doc.go`,
`auth/README.md`, tests.

Acceptance criteria:

1. `go list -deps ./auth` shows **no third-party module**; each method
   subpackage declares its own, and the core compiles without any of them.
2. `All`/`Any` are table-tested for deny-by-default (including the empty case),
   declaration-order evaluation, short-circuit, and a combined policy where any
   failed required factor rejects — with the **client-visible error identical**
   across all failure causes.
3. A construction with no authenticator panics at construction, not at first
   request.
4. Timing: "unknown subject" and "wrong credential" exercise the same work
   (asserted structurally — a dummy-hash verification path — with a tolerant
   wall-clock check that survives CI noise).
5. Lockout: N failures trigger backoff, success resets, and under a flood of
   distinct subjects and addresses the tracker stays **bounded** (no unbounded
   growth).
6. `auth/token`: verify, expire, revoke; a single-use ticket cannot be redeemed
   twice; the stored form is a hash, proven by inspecting the store.
7. `auth/ipallow`: a spoofed `X-Forwarded-For` is ignored with no trusted-proxy
   set; with one configured, the correct hop is selected; an empty allowlist
   denies.
8. `auth/sshkey`: a valid `ssh-keygen -Y sign` signature over the server nonce
   verifies; a tampered payload, a wrong key, a replayed nonce and an expired
   challenge all fail.
9. `auth/password`: constant-time verification, cost parameters round-trip with
   the hash, and a successful verify against an outdated cost **rewrites** the
   stored hash.
10. Redaction: a captured log sink and a `%+v` dump of every credential-bearing
    type contain no secret material.
11. The ADR-0009 WebTUI policy compiles and is exercised: `All(ipallow, Any(
    sshkey, mtls))`, with password rejected as a mechanism for that consumer.
12. `go vet` clean, race-clean, `doc.go` + `README.md` present, every exported
    symbol documented, tests stdlib-only.

## 6. Sequencing

The `Authenticator`/`Request`/`Identity` interfaces and the composition
semantics (§2.1, §2.2) should be settled **first**, because ADR-0009's backend
depends on the interface and can proceed against it while the individual methods
land incrementally. Suggested order: core + `ipallow` + `token` (all stdlib, and
enough for ADR-0009's ticket flow) → `sshkey` + `mtls` (the strong mechanisms
WebTUI accepts) → `password` + `totp` (other callers).

## 7. Open questions for review

1. **Lockout state ownership.** In-process (simple, resets on restart, wrong
   under multiple replicas) versus a pluggable store interface (correct, but a
   second thing to configure, and an unauthenticated write path into shared
   state). Leaning in-process with a documented interface seam for later.
   Confidence 70%.
2. **SSH-key browser flow:** signed challenge, SSH-minted ticket, or both
   (§2.5)? Leaning ticket-as-default with the challenge available for users who
   cannot run the CLI themselves.
3. **Should `auth/token` own session *lifecycle*** (attach/detach, idle
   eviction) or only credential validity, leaving lifecycle to ADR-0009's
   session manager? Leaning the latter — a token is a credential, not a session
   — but ADR-0009 then owns eviction and the two must not disagree about expiry.
4. **PBKDF2 iteration default.** A number chosen today ages badly. Pin a
   conservative default and document a review cadence, or expose only an
   explicit cost with no default?
5. **Does `Identity` need to carry authorization hints** (groups/scopes) even
   though RBAC is out of scope, so a later authorization layer does not force a
   breaking change to the struct? Leaning yes: an open `Attributes` map now
   costs nothing and avoids that break.

## Review history

- **r1 (pending)** — design review requested 2026-08-21.
