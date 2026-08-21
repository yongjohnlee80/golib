# ADR-0001 — `golib/auth`: composable authentication

- **Status:** **Proposed (rev 1)** (2026-08-21 — authored by jarvis; lector
  design r1 `change_requested` folded, including undefined identity composition,
  an unenforceable factor rule, and a reversed password-KDF decision. See Review
  history. Lands on `auth-pkg`.)
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

// Identity is the outcome: who, proved how (possibly by several factors), and
// until when. Proofs ACCUMULATE — a scalar Method cannot describe All(...).
type Identity struct {
	Subject   string    // the authenticated principal
	Proofs    []Proof   // every factor that contributed, in evaluation order
	IssuedAt  time.Time // earliest contributing proof
	ExpiresAt time.Time // MINIMUM across contributing proofs (§2.2)
}

// Proof is one factor's contribution: which method, and whether it carried an
// identity or merely constrained one (§2.2).
type Proof struct {
	Method string
	Kind   FactorKind // Identity or Contextual
}
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
  that is an oracle. One uniform `ErrUnauthenticated` outward; a **private,
  structured audit record** carrying a specific reason code and a
  correlation/attempt ID goes to the log, so an operator can diagnose a real
  user report from the attempt ID without the client learning anything.

### 2.2.1 Identity merging is defined, not incidental (r1 must-fix 1)

Control flow alone is not a specification: rev 0 said what `All` *evaluates* but
not what `Identity` it *returns*, which left `All(password, totp)` free to
combine two different people. Normative rules:

- **Every subject-bearing factor in an `All` must agree on `Subject`.** A
  disagreement is a **failure**, not a merge — it means the policy proved two
  different principals.
- **A top-level success must contain at least one identity-bearing proof**
  (§2.2.2). A policy satisfied entirely by contextual factors denies.
- **Proofs accumulate** in evaluation order; there is no scalar "the method".
- **`ExpiresAt` is the MINIMUM** across contributing proofs; `IssuedAt` is the
  earliest. A short-lived factor bounds the whole identity.
- **Claim conflicts are deterministic**: first-writer-wins in declaration order,
  and any conflict is recorded in the audit record.
- **Invariant:** a non-nil error means a nil `*Identity`, and a nil error means a
  non-nil `*Identity` with a non-empty `Subject`. Never both, never neither.

### 2.2.2 Contextual factors cannot authenticate alone — enforced, not documented (r1 must-fix 2)

Rev 0 relied on a doc comment saying `ipallow` "may only appear inside
`All(...)`". The generic interface cannot enforce prose: `Any(ipallow, sshkey)`
would happily succeed on address alone. So classification is part of the type:

```go
type FactorKind uint8

const (
	FactorContextual FactorKind = iota // constrains an identity; proves none
	FactorIdentity                     // carries a Subject
)

// Every authenticator declares what it is; the policy tree is VALIDATED at
// construction so a top-level success can never rest on contextual factors
// alone.
type Factor interface {
	Authenticator
	Kind() FactorKind
}
```

`ipallow` is `FactorContextual`; `sshkey`, `mtls`, `password`+`totp`, and a
consumed `token` are `FactorIdentity`. Constructing
`Any(ipallow, sshkey)` at top level is a **construction error** (panic, house
rule) because one branch could satisfy it contextually. This is what makes the
WebTUI policy in ADR-0009 §2.8 expressible and safe.

### 2.3 Methods, and where each dependency lives

Per the tightened dependency rule: stdlib is checked first, and any third-party
import is isolated in a leaf subpackage with its justification recorded here.

| Package | Method | Dependency | Justification |
| --- | --- | --- | --- |
| `auth/ipallow` | CIDR allowlist | **stdlib** (`net/netip`) | — |
| `auth/token` | opaque session tokens: issue / verify / revoke / expire / single-use | **stdlib** (`crypto/rand`, `crypto/sha256`, `crypto/subtle`) | — |
| `auth/password` | hashed password verification | `golang.org/x/crypto/argon2` | §2.4 — the module is already present for `sshkey`, so this is free at module granularity |
| ~~`auth/totp`~~ | **DEFERRED (r1)** — no concrete caller; see §3 | — | — |
| `auth/mtls` | client-certificate **chain verification** (§2.6a) | **stdlib** (`crypto/tls`, `crypto/x509`) | — |
| `auth/sshkey` | `authorized_keys` parsing + SSH signature verification | `golang.org/x/crypto/ssh` | §2.5 |

Four of five are dependency-free at the import level, and **exactly one module**
(`golang.org/x/crypto`) is added in total — shared by `sshkey` and `password`.

### 2.4 Password hashing: Argon2id (REVERSED in r1)

**Rev 0 chose stdlib PBKDF2 to avoid a module. That reasoning does not survive
contact with this same ADR:** `auth/sshkey` already introduces
`golang.org/x/crypto` (§2.5), so PBKDF2 **avoids no dependency at all** — the
module is in the graph either way. The dependency rule compares *modules*, and
at module granularity the choice was free; I had evaluated the two subpackages
independently, which is the wrong unit.

With the cost gone, the security argument decides: **Argon2id is the default**,
with versioned parameters and upgrade-on-successful-verify. Nor are password
databases "lower stakes" merely because the *first* caller excludes passwords —
a stored credential outlives the caller that created it (RFC 9106; OWASP
Password Storage Cheat Sheet).

`crypto/pbkdf2` remains available as an **explicitly selected FIPS-oriented
leaf**, chosen only when a caller has that requirement — never as the default.

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

**The ticket belongs to `auth/token` plus a trusted SSH/CLI minter, not to
`auth/sshkey`** (r1). `auth/sshkey` verifies *signatures*; minting a ticket over
an already-authenticated SSH channel is a token concern, and conflating them
would put a bearer-credential lifecycle inside a signature verifier.

**Challenge hygiene (r1 should-fix 5):** an `ssh-keygen -Y sign` challenge must
carry a **domain-separated namespace** (so a signature for one purpose cannot be
replayed as another), be **bound to subject, session and origin**, carry an
explicit **expiry**, and have its nonce **atomically consumed** exactly once.
Acceptance exercises `ssh-keygen -Y sign`'s identity/namespace semantics
directly.

**Never acceptable, and stated in the package docs: asking a user to paste a
private key into a browser or a form.**

### 2.6 Hostile-input rules that are easy to get wrong

- **No user enumeration — across every path, not just hashing (r1 must-fix 4).**
  A dummy hash covers one lookup and leaves the lockout machinery as an oracle.
  The **known, unknown, wrong, locked and tracker-full** paths must all perform
  equivalent per-subject *and* per-address counter reads, writes and backoff
  decisions, so timing cannot distinguish them. One uniform
  `ErrUnauthenticated` outward, always.
- **Constant time, on fixed-length inputs.** Decode proofs to a **fixed length
  first**, then compare: `subtle.ConstantTimeCompare` itself **returns early on
  a length mismatch**, so handing it variable-length material reintroduces the
  leak it exists to prevent.
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
- **Tokens (r1 must-fix 3):** CSPRNG with a documented entropy floor, parsed as
  a **fixed-length** value and **stored hashed** (never plaintext at rest) with
  the hash as the index key. Redemption of a single-use ticket is **one atomic
  store operation** — `Consume(hash, now)` — never verify-then-delete, which
  races two concurrent redeemers into both succeeding. ("Constant-time lookup" in
  rev 0 was not a meaningful store property; constant-time *comparison of
  fixed-length proofs* is.) Mandatory expiry with a safe default, explicit
  revocation, and single-use tickets as a distinct mode — ADR-0009 §2.8 needs
  exactly that for the WS handshake.

### 2.6a mTLS verifies a chain, not a certificate (r1 must-fix 6)

`auth/mtls` accepts **only** a chain present in
`tls.ConnectionState.VerifiedChains`, produced under **configured client-auth
roots** and the expected EKU — `PeerCertificates` alone proves nothing, since any
self-signed certificate lands there. On top of that: an explicit
certificate-subject → `Identity.Subject` mapping, and the certificate's own
expiry bounding `Identity.ExpiresAt`. Acceptance covers self-signed, unverified,
wrong-root, wrong-EKU and valid-chain cases.

### 2.6b Lockout state has an injected, atomic seam (r1 should-fix 4)

The counter store is a **minimal injected interface** whose operations
(increment, read-with-backoff-decision, reset) are **atomic**, with a
**bounded in-memory default** for the single-process deployment. Exposing the
seam now means the multi-replica case does not require an interface change
later; shipping only the in-memory map would.

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
*management* (registration, password reset, roles/RBAC).

**Deferred in r1, with reasons:**

- **`auth/totp`** — specifying it properly means last-accepted-timestep replay
  prevention, a skew window, rate limits, secret storage/redaction and
  same-subject binding. There is no concrete caller (WebTUI does not accept
  passwords, so it needs no second factor), and a half-specified TOTP is worse
  than none. It returns when a caller exists.
- **`Identity.Attributes` (an open mutable map)** — rev 0 added it "so a later
  authorization layer does not force a breaking change". That is backwards: an
  exported mutable map **is** a public compatibility contract, created now for a
  consumer that does not exist, with no defined merge semantics under §2.2.1.
  Typed claims arrive with an authorization ADR. Each is a protocol
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

1. `go list -deps ./auth` shows **no third-party module**; exactly one module
   (`golang.org/x/crypto`) appears across the method subpackages, and the core
   compiles without it.
2. `All`/`Any` are table-tested for deny-by-default (including the empty case),
   declaration-order evaluation, short-circuit, and a combined policy where any
   failed required factor rejects — with the **client-visible error identical**
   across all failure causes.
2b. **Identity merging (§2.2.1):** `All` over two identity-bearing factors with
   *disagreeing* subjects **fails**; agreeing subjects yield one `Identity` whose
   `Proofs` contain both in order, whose `ExpiresAt` is the **minimum** and whose
   `IssuedAt` is the earliest; a claim conflict resolves first-writer-wins and is
   recorded in the audit record. The nil/error invariant holds in every branch.
2c. **Factor classification (§2.2.2):** constructing a top-level
   `Any(ipallow, sshkey)` is a **construction error**; `All(ipallow, sshkey)`
   constructs and requires both; a policy satisfiable by contextual factors alone
   can never be built.
3. A construction with no authenticator panics at construction, not at first
   request.
4. Timing symmetry across **all five** paths — known, unknown, wrong, locked and
   tracker-full — asserted structurally (equivalent counter reads/writes and
   backoff decisions, plus dummy hashing) with a tolerant wall-clock check that
   survives CI noise. A separate test proves proofs are decoded to a fixed length
   *before* `subtle.ConstantTimeCompare`.
5. Lockout: N failures trigger backoff, success resets, and under a flood of
   distinct subjects and addresses the tracker stays **bounded** (no unbounded
   growth).
6. `auth/token`: verify, expire, revoke; the stored form is a hash, proven by
   inspecting the store.
6b. **Atomic consume under concurrency:** many goroutines redeem the same
   single-use ticket simultaneously and **exactly one** succeeds — a race test,
   run under `-race`.
7. `auth/ipallow`: a spoofed `X-Forwarded-For` is ignored with no trusted-proxy
   set; with one configured, the correct hop is selected; an empty allowlist
   denies.
8. `auth/sshkey`: a valid `ssh-keygen -Y sign` signature over the server nonce
   verifies; a tampered payload, a wrong key, a replayed nonce and an expired
   challenge all fail.
9. `auth/password`: Argon2id with versioned parameters, constant-time
   verification, parameters round-tripping with the hash, and a successful verify
   against outdated parameters **rewriting** the stored hash. A PBKDF2 credential
   verifies only when that leaf is explicitly selected.
9b. `auth/mtls`: a valid configured chain authenticates; self-signed,
   unverified, wrong-root and wrong-EKU all fail; the subject mapping and the
   certificate-expiry bound on `ExpiresAt` are asserted.
10. Redaction across `String`, every relevant `fmt` verb, JSON **and** text
    marshaling, pointers, nested requests, wrapped errors and audit records: a
    captured sink contains no secret material in any of them.
11. The **corrected** ADR-0009 WebTUI policy compiles and is exercised:
    `Any(singleUseSSHChannelTicket, mtls)` — optionally wrapped in
    `All(ipallow, ...)` — with IP never satisfying it alone and password rejected
    as a mechanism for that consumer.
12. `go vet` clean, race-clean, `doc.go` + `README.md` present, every exported
    symbol documented, tests stdlib-only.

## 6. Sequencing

The `Authenticator`/`Request`/`Identity` interfaces and the composition
semantics (§2.1, §2.2) should be settled **first**, because ADR-0009's backend
depends on the interface and can proceed against it while the individual methods
land incrementally. Suggested order: core + `ipallow` + `token` (all stdlib, and
enough for ADR-0009's ticket flow) → `sshkey` + `mtls` (the strong mechanisms
WebTUI accepts) → `password` + `totp` (other callers).

## 7. Resolved review questions (r1)

1. **Lockout state:** expose the atomic store seam now, ship a bounded
   in-process default (§2.6b).
2. **SSH-key browser flow:** ticket by default, signed challenge optional
   (§2.5).
3. **Token vs session ownership:** `auth/token` owns credential validity and
   consumption; ADR-0009's WebTUI owns App/session lifecycle. They must not both
   own expiry.
4. **Password default:** Argon2id with safe versioned parameters and
   upgrade-on-success; PBKDF2 only for an explicit FIPS requirement (§2.4).
5. **Authorization hints:** do **not** add an open `Attributes` map (§3).

## 8. Superseded open questions

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

- **r1 (2026-08-21, lector — `change_requested`, folded in this revision).**
  **Must-fix 1:** rev 0 specified `All`/`Any` control flow but never how
  successful identities *combine*, so `All(password, totp)` could have merged two
  different people — §2.2.1 now requires subject agreement, at least one
  identity-bearing proof at top level, accumulated `Proofs` instead of a scalar
  `Method`, minimum expiry, deterministic claim conflicts and an explicit
  nil/error invariant. **Must-fix 2:** rev 0 tried to keep `ipallow` from
  authenticating alone **with a doc comment**, which the generic interface cannot
  enforce — `Any(ipallow, sshkey)` would succeed on address alone. §2.2.2 adds a
  `FactorKind` classification and construction-time policy-tree validation.
  **Must-fix 3:** single-use redemption is now one atomic `Consume`, not
  verify-then-delete, with a concurrency race test; and "constant-time lookup"
  was replaced by fixed-length proof comparison, which is the property that
  actually exists. **Must-fix 4:** dummy hashing covered only one path — the
  lockout counters were themselves an enumeration oracle, so known/unknown/wrong/
  locked/tracker-full must now perform equivalent counter work, and proofs are
  decoded to fixed length *before* `subtle.ConstantTimeCompare`, which returns
  early on a length mismatch. Detailed reason codes move to a private audit
  channel with an attempt ID. **Must-fix 5 reversed my decision:** rev 0 chose
  stdlib PBKDF2 to avoid a module, but `auth/sshkey` already pulls
  `golang.org/x/crypto` in this same ADR — so PBKDF2 avoided nothing. I had
  compared *subpackages* when the dependency rule compares *modules*. Argon2id
  is now the default (RFC 9106, OWASP), with PBKDF2 kept only as an explicit
  FIPS-oriented leaf. **Must-fix 6:** `auth/mtls` must require a chain in
  `VerifiedChains` under configured client-auth roots and EKU — `PeerCertificates`
  alone admits any self-signed certificate. **Should-fixes:** `auth/totp`
  deferred (no caller, and a half-specified TOTP is worse than none);
  `Identity.Attributes` removed (an exported mutable map is a compatibility
  contract created for a consumer that does not exist); empty-policy versus
  construction-panic disambiguated; lockout store seam exposed with a bounded
  default; SSH challenges domain-separated, bound and atomically consumed;
  redaction coverage widened. **Cross-ADR:** the WebTUI policy is
  `Any(singleUseSSHChannelTicket, mtls[, sshChallenge])`, optionally wrapped in
  `All(ipallow, ...)` — IP optional and never identity-bearing.
  Review doc: `$KB_ROOT/agents/lector/reviews/2026-08-21-golib-tui-web-auth-coupled-design-review.md`
