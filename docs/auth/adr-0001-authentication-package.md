# ADR-0001 — `golib/auth`: composable authentication

- **Status:** **Accepted (rev 8)** (2026-08-22 — authored by jarvis; lector
  design r1-r3 `change_requested` folded, r4 **approved**, and **accepted by
  Johno 2026-08-21** — implementation in progress. **Rev 5 changed a mechanism
  mid-implementation**: §2.5 SSHSIG verification is delegated to
  `ssh-keygen -Y verify` instead of the hand-rolled parser, which makes the
  client claim an identity. Lector r5 and r6 both `change_requested`, six
  must-fixes total, all folded. Lector r7 **approved the SSH work in substance**
  and raised five more against the newly added `Throttle`/`MemTracker`, folded in
  **rev 8**, the current text. Four were shipped bugs: cancellation did not reap
  descendants (r5); the post-`Run` group kill could signal an unrelated process
  group after pid reuse (r6); a locked attempt destroyed single-use credentials,
  and eviction stopped enforcing its cap after 2038 (r7). See Review history.
  Lands on `auth-pkg`.)
- **Date:** 2026-08-21
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none — a new subsystem.
- **Related:** `golib/tui` ADR-0009 §2.8 (the first consumer, which takes this
  package's `Policy`), the `golib` convention's philosophy #6
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
// Factor is ONE leaf check. It returns a Contribution, not an Identity —
// r2 must-fix 1: a contextual factor such as ipallow carries no Subject, so it
// cannot legally satisfy an interface whose success must produce a non-empty
// Subject. Leaves contribute; only a validated Policy concludes.
type Factor interface {
	Verify(ctx context.Context, r *Request) (Contribution, error)
	Kind() FactorKind
}

// Contribution is what one leaf proved. Subject is REQUIRED when the Factor
// declares FactorIdentity and MUST be empty when it declares FactorContextual —
// validated against Factor.Kind() at evaluation time so the classification the
// tree was validated with cannot drift from the one it runs with.
//
// Contribution carries no Kind of its own: duplicating it would be exactly that
// drift (r3 must-fix 2).
type Contribution struct {
	Method    string
	Subject   string    // identity-bearing leaves only; empty for contextual
	IssuedAt  time.Time
	ExpiresAt time.Time // zero = this proof imposes no post-auth bound (§2.2.1)
}

// Node is one position in a policy tree. The interface is CLOSED (an
// unexported method), so the only nodes are the three constructors below and
// external factors enter through Leaf — that closure is what lets NewPolicy
// reason about the whole tree.
type Node interface{ isNode() }

func Leaf(f Factor) Node    // an external factor becomes a node here
func All(ns ...Node) Node   // every child must pass
func Any(ns ...Node) Node   // the first passing branch wins

// Policy is a VALIDATED tree and the only thing callers invoke. NewPolicy
// computes each node's kind (§2.2.2) and rejects a root that is not
// identity-bearing; it returns an error rather than panicking, because a policy
// is often assembled from configuration.
type Policy interface {
	Authenticate(ctx context.Context, r *Request) (*Identity, error)
}

func NewPolicy(root Node) (Policy, error)

// Request is the transport-agnostic material an authenticator may inspect:
// the peer address, metadata (headers or RPC metadata), presented credentials,
// and TLS state when there is one. Deliberately NOT net/http, so an RPC server,
// a WebSocket handshake and a CLI prompt can all present one.
type Request struct { /* RemoteAddr netip.AddrPort; Metadata; Credentials; TLS */ }

// Identity is what a validated Policy concludes: who, proved how (possibly by
// several factors), and the validity interval. Proofs ACCUMULATE — a scalar
// Method cannot describe All(...).
type Identity struct {
	Subject   string    // the authenticated principal
	Proofs    []Proof   // every factor that contributed, in evaluation order
	IssuedAt  time.Time // LATEST non-zero contributing value (§2.2.1)
	ExpiresAt time.Time // MINIMUM FINITE NON-ZERO contributing value; zero = unbounded
}

// Proof records one contributing factor.
type Proof struct {
	Method string
	Kind   FactorKind // Identity or Contextual
}
```

The core package imports **stdlib + golib only** — no `net/http` (§2.7 provides
adapters).

### 2.2 Composition is a first-class feature

```go
func All(ns ...Node) Node // every child must pass (AND)
func Any(ns ...Node) Node // the first passing branch wins (OR)
```

(Declared with the rest of the graph in §2.1; repeated here for the rules that
follow.)

Rules, all normative:

- **Deny by default, with the two cases kept distinct** (r2 should-fix 3):
  an **empty factor node** — `All()` / `Any()` with no children — is a node that
  **denies**; an **empty or contextual-only final policy** — `NewPolicy(nil)`, or
  a root that is not identity-bearing — is a **construction error**. A node can
  legitimately be empty mid-construction; a finished policy cannot.
  An empty allowlist inside `ipallow` denies.
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
- **The validity interval is the INTERSECTION of the contributing proofs**
  (r2 must-fix 3): `IssuedAt` is the **latest** non-zero contributing value and
  `ExpiresAt` the **minimum finite non-zero** one. A **zero `ExpiresAt` means
  that proof imposes no post-authentication bound** and does not shorten the
  interval.
- **Which factors bound the interval:** a *continuing finite* assertion does
  (if it stops being true, the policy should not outlive it), while a *static
  observation* such as an `ipallow` match contributes **no** expiry.
- **A ticket's redemption deadline is NOT the session lifetime.** It bounds how
  long the ticket may be redeemed; once atomically consumed it is a
  point-in-time proof. `tui/web` owns session expiry (ADR-0009 §2.8), and
  conflating the two would expire a live session at the ticket's deadline.
- **Invariants, split by level (r3 must-fix 2):**
  - `Factor.Verify` — a non-nil error means the **zero `Contribution`**; a nil
    error means a `Contribution` whose `Subject` is non-empty iff the factor
    declares `FactorIdentity`.
  - `Policy.Authenticate` — a non-nil error means a **nil `*Identity`**; a nil
    error means a non-nil `*Identity` with a non-empty `Subject`.

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
```

`ipallow` is `FactorContextual`. `sshkey`, `mtls`, a consumed `token` and
`password` are `FactorIdentity`.

**The kind of a composite is computed, not declared** (r2 must-fix 2 — rev 1
left `All`/`Any` typed as plain authenticators, so nothing could enforce or
even observe it):

| Node | Identity-bearing when |
| --- | --- |
| leaf | its `Kind() == FactorIdentity` |
| `All(children…)` | **any** child is identity-bearing (the others constrain it) |
| `Any(children…)` | **every** branch is identity-bearing (a single contextual branch would be a way in) |

`NewPolicy` validates the **finished tree** — not a node in isolation, which is
why rev 1's construction check could not work: an `Any` has no idea whether it is
the root. A policy whose root is not identity-bearing is a construction error.

Consequences, all three of which are acceptance cases:

```go
// Each argument is a Node; external factors enter through Leaf.
p, err := NewPolicy(Any(Leaf(mtls), All(Leaf(ipallow), Leaf(sshChallenge))))
// VALID: both branches are identity-bearing.

_, err = NewPolicy(Any(Leaf(ipallow), Leaf(sshkey)))
// INVALID root: the ipallow branch alone would admit. err != nil.

p, err = NewPolicy(All(Leaf(ipallow), Any(Leaf(ticket), Leaf(mtls))))
// VALID: the All is identity-bearing via its Any child.
```

Note a contextual leaf remains legal **below** an `All` that always requires
another identity proof — that is the whole point of `All(ipallow, …)`.

### 2.3 Methods, and where each dependency lives

**Five method subpackages** (TOTP deferred, §3). Per the tightened dependency
rule, stdlib is checked first and any third-party import is isolated in a leaf
subpackage with its justification recorded here.

| Package | Method | Dependency | Justification |
| --- | --- | --- | --- |
| `auth/ipallow` | CIDR allowlist | **stdlib** (`net/netip`) | — |
| `auth/token` | opaque session tokens: issue / verify / revoke / expire / single-use | **stdlib** (`crypto/rand`, `crypto/sha256`, `crypto/subtle`) | — |
| `auth/password` | hashed password verification | `golang.org/x/crypto/argon2` | §2.4 — the module is already present for `sshkey`, so this is free at module granularity |
| ~~`auth/totp`~~ | **DEFERRED (r1)** — no concrete caller; see §3 | — | — |
| `auth/mtls` | client-certificate **chain verification** (§2.6a) | **stdlib** (`crypto/tls`, `crypto/x509`) | — |
| `auth/sshkey` | `authorized_keys` parsing; SSHSIG verification **delegated to `ssh-keygen -Y verify`**, in-process parser as fallback | `golang.org/x/crypto/ssh` + `os/exec` | §2.5 |

**Three of five** leaves are third-party-import-free (`ipallow`, `token`,
`mtls`); `sshkey` and `password` share **exactly one module**
(`golang.org/x/crypto`), and the core itself imports none.

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

### 2.5 SSH keys: verification is DELEGATED to `ssh-keygen -Y verify` (r5)

**Rev 4 said `x/crypto/ssh` covered the SSHSIG parsing. It does not — it ships
no `sshsig` package at all (checked at v0.53.0), so rev 4's implementation
hand-rolled the envelope parser.** That parser worked and passed real-OpenSSH
interop, but "we wrote our own parser for a security-critical wire format" is
precisely what the dependency rule's hazard carve-out exists to avoid, so it is
no longer the default.

**DECIDED (r5): verification sits behind a `Verifier` seam with two
implementations, and the default is the reference implementation itself.**

```go
type Verifier interface {
    VerifySignature(ctx context.Context, message, armoredSig []byte,
        namespace, identity string) error   // nil == valid AND allowed
}

type OpenSSH struct {                  // DEFAULT
    AllowedSigners string              // an OpenSSH allowed_signers file
    Binary         string              // "" -> look up ssh-keygen on PATH
    Timeout        time.Duration       // 0 -> 5s
}

type PureGo struct { Allowed []Allowed }   // rev 4's parser, kept as fallback
```

**Why delegate rather than depend, or keep the parser:**

| Option | Verdict |
| --- | --- |
| `ssh-keygen -Y verify` (chosen) | The format's reference implementation, audited by the people who define it, already present anywhere `ssh-keygen -Y sign` produced the signature. Also inherits the `allowed_signers` semantics we do **not** model: principal patterns, `valid-after`/`valid-before` windows, `cert-authority` lines. |
| Hand-rolled parser as default (rev 4) | Correct today, but it is our code competing with the reference on a hostile-input format, and it silently under-implements `allowed_signers`. Demoted to fallback. |
| `github.com/hiddeco/sshsig` | **Rejected.** v0.2.0, two releases ever, one maintainer, last published 2025-04-12, and it pulls `testify` + `yaml` into the graph. Worse provenance than the OpenSSH binary AND worse than our own parser, while costing a module. |

`PureGo` remains for images with no OpenSSH — a scratch or distroless
container. It is not a lesser-security option in what it *checks*; it is a
lesser option in what it *knows*, since it understands a plain allowed-key set
and nothing more. That difference is documented at the type.

**The two implementations MUST agree on who gets in.** They are permitted to
differ in error detail — `ssh-keygen` returns one exit status for every failure
— but never in the accept/reject decision for the same inputs. Acceptance
enforces this with a shared-fixture agreement table (§5 criterion 8).

**Consequence: the client must CLAIM an identity, and the claim is what gets
checked.** `ssh-keygen -Y verify` takes `-I <identity>` and answers "did *this
principal* sign this?", not "who signed this?". So the subject is no longer
derived from whichever allowed key happens to match; the request carries an
`ssh-identity` credential, and verification either proves the claim or fails.
`PureGo` adopts the same semantics for substitutability. This is a **security
improvement independent of the delegation**: it closes a case rev 4's design
admitted, where a signature valid under one principal's key was accepted as that
principal even though the request never asserted who it was — the claim and the
proof were never compared because there was no claim.

The claimed identity is **hostile input until the verifier returns nil**, and it
reaches an `argv` slot, so it is screened first: non-empty, ≤256 bytes, no
control characters, and **no leading `-`** so a value can never read as a flag.
There is no shell anywhere in the path — `argv` only — so quoting is not a
concern; the screen exists for the flag case and for defence in depth. Both
verifiers apply the identical screen, or `PureGo` would accept claims `OpenSSH`
refuses to even ask about.

**Construction, per golib's binding contract (r5 must-fix 3).** Both verifiers
are built by constructor — `NewOpenSSH(allowedSigners string, ...OpenSSHOption)`
and `NewPureGo([]Allowed)` — with **unexported fields** and functional options,
because a verifier whose configuration is only discovered to be wrong at the
first login is a misconfiguration that surfaces during an incident instead of at
startup. `NewOpenSSH` resolves `ssh-keygen` **once** (PATH is not re-consulted
per attempt) and proves both the binary and the policy file readable;
`NewPureGo` **clones** the allowed set, so a caller mutating their slice later
cannot silently change who may log in, and **rejects an empty set** — a verifier
that can never admit anyone is a misconfiguration, not a runtime denial. A
zero-value `OpenSSH{}`/`PureGo{}` obtained by bypassing the constructors denies.

`Binary(path)` is **recommended for hardened deployments**: PATH is
deployment-supplied trust. Go already refuses a current-directory-relative
lookup result, and an absolute path removes the remaining question.

**Subprocess hygiene, since a signature verification now forks:**

- The **message goes on stdin**, never on `argv` and never through a file.
- The signature must be a file (`-s` takes a path), so it is written `0600`
  inside a `0700` `MkdirTemp` directory, removed on return.
- **Cancellation kills the process GROUP, not just `ssh-keygen` (r5 must-fix
  1).** The child is started with `Setpgid`, and `Cancel` sends `SIGKILL` to
  `-pgid`; a second group kill runs after `Wait` returns. This is a shipped-bug
  fix, not a precaution: `CommandContext`'s default cancellation kills only
  `cmd.Process` and `WaitDelay` only closes our end of the pipes, so **neither
  reaps a descendant** — the first cut returned in ~1.1s while its grandchild
  kept running, and repeated timeouts would have accumulated processes
  indefinitely. Regression test asserts the **grandchild is gone**, not merely
  that the call returned; with the fix reverted it fails on a surviving pid.
  `WaitDelay` (1s) stays as the final I/O bound **after** whole-tree
  cancellation, never as a substitute for it.
  **The group kill happens ONLY from `Cancel`, never after `Run` (r6 must-fix
  1).** `Run` includes `Wait`, so by then the child is reaped and its pid is
  released — the kernel may reuse that integer as an unrelated process-group
  leader, and `kill(-oldpid)` would kill a stranger's group. `Cancel` is safe
  precisely because the unreaped child still holds the pgid. Rev 6's
  "belt-and-braces" post-`Run` kill was removed; it also protected nothing, since
  a clean `ssh-keygen -Y verify` spawns no descendants.
  **On non-unix platforms `NewOpenSSH` REFUSES TO CONSTRUCT (r6 must-fix 3)**,
  returning `ErrUnsupportedPlatform`. A no-op with a private comment was
  capability dishonesty: the README promised whole-tree cleanup while the exact
  leak stayed reachable, and the constructor gave no signal. The alternative
  considered was an untested Windows Job Object implementation guarding a
  security property, which is worse than an honest refusal. Those callers use
  `NewPureGo`, which forks nothing and so has nothing to leak.
- **`allowed_signers` readability is proved by OPENING it, not by `Stat`
  (r5 must-fix 2).** `os.Stat` succeeds on a mode-000 file and on a directory,
  so a `Stat`-only preflight let an unreadable-but-present file through; the
  failure then arrived as a nonzero `ssh-keygen` exit, i.e. as
  `ErrBadSignature` — an outage reported as a bad credential. The check opens
  the file, and requires a regular file, at construction **and again per call**
  (the file can be replaced under a long-running process).
  **The promise is narrowed accordingly:** `ErrVerifierUnavailable` is
  guaranteed for a policy file that is unreadable *when the check runs*. A
  file that breaks inside the gap between that check and `exec` surfaces as a
  rejection; the window cannot be closed from user space, and the next attempt
  classifies it correctly.
- **The BINARY is validated as executable, not readable (r6 must-fix 2).** Rev 6
  ran it through the same open-based check as the policy file, which accepts a
  mode-0600 text file — reproduced: it constructed successfully, and the truth
  (`EACCES`/`ENOEXEC`) waited for someone's first login — and conversely rejects
  a valid execute-only binary. Resolution goes through `exec.LookPath`, including
  for an explicit slash-containing path, because executability is the question
  actually being asked. A **negative** `VerifyTimeout` is a construction error;
  only zero means "use the default".
- **Misconfiguration is a distinct error class.** A missing `ssh-keygen`, an
  unreadable `allowed_signers`, a cancelled context, or a subprocess that had to
  be killed returns `ErrVerifierUnavailable`, never `ErrBadSignature`. An
  operator must be able to tell "I broke the deployment" from "that credential
  was refused", and a caller must never see an infrastructure fault as a rejected
  user. `auth.Policy` still collapses both to `auth.ErrUnauthenticated` at the
  boundary (§2.6).
- **Captured stderr is bounded** (4 KiB, first-bytes-kept). `ssh-keygen` emits
  one line; an unbounded buffer would let a wedged or wrong binary spool into
  memory.
- Measured exit codes (OpenSSH 10.3): **0** valid; **255** for every failure —
  wrong namespace, tampered message, unknown identity, empty `allowed_signers`.
  The decision is exit-status driven; no output is parsed.
- `ssh-keygen -Y verify` reads **no implicit configuration file**. That is the
  whole of the claim (r6 should-fix): the verdict is NOT purely a function of
  argv plus the policy file, because `allowed_signers` validity windows are
  evaluated against the **system clock**, and a non-`Z` timestamp in that file is
  interpreted in the **system timezone**. Clock skew and `TZ` are therefore part
  of this verifier's trust base, and rev 6 overclaimed by omitting them.

**Both verifiers honor `ctx` (r5 should-fix 1).** An already-cancelled context is
refused before any work, and `PureGo` re-checks after its derivation. Otherwise
one implementation would admit a cancelled call that the other refuses — a
disagreement about *admission*, the one thing they may never disagree about.

**Timing, stated precisely (r5 should-fix 4).** The unavailable paths return
**before** forking, so overall verifier health is observable by timing. That is
accepted: it is **claim-independent** — it says the deployment is broken, not
which principals exist — and it enumerates nobody. **No dummy fork is added to
disguise an outage.** This is the opposite of `auth/password`'s dummy hash,
which is mandatory precisely because that timing difference *is*
claim-dependent (§2.6).

**`x/crypto` stays in the graph regardless, so this changes no dependency
accounting.** `ParseAuthorizedKeys` still uses `ssh.ParseAuthorizedKey`,
`PureGo` still uses `ssh.PublicKey.Verify`, and §2.4's Argon2id needs
`x/crypto/argon2`. **Argon2id therefore stands** — confirmed by Johno
2026-08-22 — and §2.4's "free at module granularity" argument is unaffected.

> **Version pinned to x/crypto v0.53.0 deliberately:** it requires exactly the
> `x/sys` and `x/term` versions golib already has, so adding it disturbs neither
> — v0.55.0 would have bumped both, and `tui/term` imports `x/term` directly.
> Only two indirect deps (`x/sync`, `x/text`) moved.

**A browser cannot read `~/.ssh`, so "authenticate with an SSH key" needs a
mechanism.** **DECIDED (r1): the SSH-channel-minted single-use ticket is the
default, with the signed challenge available as an option.** Both are specified
because ADR-0009 admits either:

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

**Implementation notes (2026-08-22, rev 8 — folding lector r7).** Rev 7 shipped
`Throttle` + `MemTracker` and lector found five blockers in them. Each one
changed the design, so they are recorded here rather than as a diff summary.

**Every `Tracker` method takes a `context.Context` and returns an `error`
(r7 must-fix 5).** Without them the seam could not support the implementation it
exists to preserve: a Redis or SQL tracker cannot honor the caller's deadline
and cannot say "I could not answer", so `Throttle` could neither make nor audit
an outage decision. The outage policy is therefore explicit:
**fail-CLOSED by default** — a tracker that cannot answer cannot protect
anything, and continuing means running with brute-force protection silently
switched off — with `FailOpen()` as a deliberate availability-over-security
choice and `OnTrackerError` so an outage is never invisible. A tracker write
failure on the SUCCESS path never fails the authentication; the credential was
correct and the stale counter expires. Client-visible failure stays uniform.

**Per-subject throttling requires an explicit, determined subject source
(r7 must-fix 1).** Rev 7 guessed: it looked for `subject` then `ssh-identity`
and fell back to `""`. Every factor without one of those keys — a `token`
request carries `ticket`, and `mtls` has no subject until the chain verifies —
therefore shared **one global counter**, so any single attacker could lock out
every client of that factor while their own address counter stayed clean.
Now a new `Claimant` interface (`Claim(*Request) string`, side-effect-free,
verifies nothing) declares the capability; `NewThrottle` requires that the
wrapped factor implement it, OR `SubjectClaim(fn)`, OR `AddressOnly()`, and
returns a **construction error naming all three** otherwise. `auth/password` and
`auth/sshkey` implement `Claimant`. An attempt that names nobody is counted
against **its address** in the subject slot, never a shared key.

**Permitted side effects on a locked attempt are now specified (r7 must-fix 1,
second half).** Rev 7 ran the wrapped factor even when locked — correct for the
enumeration reason below — but that meant a single-use token factor **atomically
consumed a valid ticket and discarded the proof** on a denied attempt. So the
two modes differ deliberately:

- **Subject mode runs the factor even when locked** and discards the verdict.
  Short-circuiting would return in microseconds against tens of milliseconds for
  a wrong password, and since only an EXISTING account can be locked out,
  timing lockout enumerates users — handing back the oracle §2.6's dummy hash
  closes. A wrapped factor here MAY consume state the client presented in *this*
  attempt (an SSH challenge nonce is spent on presentation by design) but MUST
  NOT consume a credential the client holds across attempts.
- **Address-only mode short-circuits.** There is no principal to enumerate — the
  credential *is* the identity — an address learning about its own backoff
  reveals nothing, and short-circuiting is what keeps a single-use credential
  from being destroyed on a denied attempt.

The CPU cost of subject mode is stated at the type: it defends **credentials,
not CPU**, and bounding unauthenticated request volume is the transport's job.

**Tracker keys are hashed to fixed width (r7 must-fix 2).** An entry cap bounds
the *number* of records; it bounds *memory* only if the keys are bounded too, and
a claimed subject is attacker-controlled and unbounded — 16,384 arbitrarily long
strings sit inside any count-based cap. Keys are now
`namespace + ":" + sha256(namespace || 0x00 || value)`, with the value truncated
at 4 KiB before hashing so the per-attempt hashing cost is not chosen by the
attacker either. Truncation can make two long subjects share a counter, which
only makes lockout *stronger*. `auth/password` additionally caps a claimed
subject at 256 bytes.

**Eviction is bounded work, and its sentinel was a 2038 bug (r7 must-fixes 3
and 4).** Rev 7 scanned every record to sweep and to pick a victim — O(cap) work
an attacker triggers on **every miss at capacity**, measured at ~79 ms per 100
inserts against ~43 µs when not full at the 16k default. That is both a DoS
amplifier and a timing signal isolating "new key at capacity" from every other
path. Making room now examines at most **8** records (Go randomizes map
iteration, so the first N entries are an effectively random sample), drops any
expired ones it sees, and otherwise evicts the least-recently-touched of the
sample. Separately, rev 7 seeded its "oldest so far" with
`time.Unix(math.MaxInt32, 0)`; with records dated past 2038 nothing was ever
selected, the insert happened anyway, and **the cap silently stopped being
enforced** — reproduced through the public API. It is an explicit
`haveOldest bool` now, with a post-2038 regression test.

**`NewMemTracker` returns an error and does not normalize (r7 should-fix).**
Silently repairing a lockout threshold gives an operator who typo'd it a working
system with behavior they did not write, and no way to find out. Zero size and a
zero `Backoff` remain the documented shorthands for the defaults; everything
else invalid is refused.

### 2.7 Secrets never leak, structurally

Redaction is a **type**, not a convention: a wrapper whose `String`, `Format`
and `MarshalJSON` all return a placeholder, so `%v`, `%+v`, `log.Printf` and
JSON encoding cannot print a credential by accident. No credential material in
errors, panics, or logs (RULES.md #1). Logging is `logger.Logger` (default
`Nop`): attempt, success, failure, lockout — with subject and method, never the
secret.

**Implementation notes (2026-08-22).** Rev 4's implementation built the `Audit`
record and **threw it away** — nothing emitted the correlation ID, so this
ADR's "an operator diagnoses from the attempt ID" promise was not true of the
code. `NewPolicy` now takes `Log(logger.Logger)` and `Observe(func(Attempt))`,
and emits exactly one `Attempt` per authentication.

- **Severity by outcome: success Info, failure Notice.** A failed login is the
  system working; logging it at Error trains operators to ignore the level that
  matters.
- **A backoff refusal logs as `throttled`, not `failure`** — the caller still
  receives the same `ErrUnauthenticated`, but a flood of one is a different
  operational fact from a flood of the other.
- **`Subject` appears only on success**, where it has been proven. An unverified
  claim does not belong in a field that reads like an established fact; it stays
  in the factor's reasons.
- **Control characters are stripped and fields truncated** in the rendering.
  `Subject` and `Peer` derive from request data, and a newline in a logged field
  is how a log file gets forged entries.

### 2.8 Adapters, not framework coupling

The core stays free of `net/http`. Thin adapters live in subpackages: a
`net/http` middleware, and a helper usable from `server/rpc` / `server/ws`, so
one policy value serves a web handler, an RPC server and a CLI prompt.

**Implementation notes (2026-08-22).** `auth/authhttp` provides
`Middleware(policy)`, `FromRequest` and `IdentityFrom(ctx)`; `auth.FromConn`
covers the non-HTTP transports and imports only `net`.

Three decisions in the adapter are security-relevant rather than mechanical:

- **Headers are copied by ALLOWLIST, not in bulk.** A bulk copy puts `Cookie`
  and `Authorization` into `Metadata`, a plain `map[string][]string` that
  `Secret` does not protect — the first `%+v` that touches the request prints
  them. The default list is only what a factor consults (`Origin`,
  `User-Agent`, the forwarded-address headers, the WS subprotocol).
- **`Peer` is `RemoteAddr`, never a forwarded header**, and an unparsable value
  yields the **zero** `AddrPort` rather than a guess. Every address-keyed control
  must read that as "no address"; a plausible-looking fallback would be an
  allowlist bypass. The same rule governs `FromConn`, including for a unix
  socket, which has no network address at all — authenticate a unix peer by
  credential.
- **TLS is projected through `mtls.FromConnectionState`**, which refuses to carry
  `PeerCertificates` (§2.6a). `FromConn` does not touch TLS at all rather than
  reimplement that refusal.

`Middleware(nil)` **panics at construction**: a middleware that silently stops
authenticating is the worst failure mode available to this package. The
context key is unexported, so nothing outside the adapter can plant an identity
a downstream handler would trust.

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
- **`Identity.Attributes` (an open mutable map)** — proposed in rev 0 on the
  grounds that adding it later would be a breaking change. That reasoning is
  backwards: an exported mutable map **is itself** a public compatibility
  contract, created now for a consumer that does not exist, and with `Attributes`
  deferred there are no claims to merge, so §2.2.1 defines no conflict rule
  either. Typed claims arrive with an authorization ADR, together with their
  merge semantics.

Each deferred item above is a protocol surface plus its dependencies, and each
can arrive later as its own ADR and leaf subpackage. **This package answers one
question — *does this request carry a valid credential?* — and returns an
`Identity` or an error.**

## 4. Alternatives considered

1. **A bag of unrelated helper functions** (`CheckPassword`, `CheckIP`, …) with
   composition left to the caller. Rejected: every consumer would re-implement
   the AND/OR logic, and the security-critical parts (deny-by-default, uniform
   error, no oracle) are exactly what gets skipped when hand-rolled per call
   site.
2. **`net/http`-shaped middleware only.** Rejected: it would exclude
   `server/rpc`, the WS handshake and CLI prompts, and would drag `net/http`
   into the core.
3. **~~`x/crypto` argon2id for passwords~~ — this was rejected in rev 0 and is
   now the CHOSEN default (§2.4).** Kept here only to record the reversal: the
   rejection rested on avoiding a module that `auth/sshkey` was adding anyway.
   The live alternative is the reverse — stdlib PBKDF2 — retained as an
   explicitly selected FIPS-oriented leaf, never the default.
4. **Rolling our own SSH signature parsing to stay dependency-free.** Rejected —
   §2.5. This is the case the dependency rule's exception exists for.
5. **Delegating everything to a reverse proxy** (oauth2-proxy, Tailscale or
   Cloudflare Access) and shipping no auth package. A legitimate production
   deployment and worth recommending in the README, but it cannot be the only
   answer: it does not help a self-contained CLI binary that a user runs on a
   box they already SSH into, which is precisely ADR-0009's case.

## 5. Files / acceptance

New: `auth/**` (core + the five method subpackages + adapters), `auth/doc.go`,
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
   `Proofs` contain both in order. A `Contribution` from a contextual leaf never
   supplies a `Subject`, and the nil/error invariant holds in every branch:
   non-nil error ⇒ nil `*Identity`, nil error ⇒ non-empty `Subject`.
2c. **Tree validation (§2.2.2)** — all three trees are acceptance cases, written
   in the real graph so they compile:
   `NewPolicy(Any(Leaf(mtls), All(Leaf(ipallow), Leaf(sshChallenge))))` **is
   valid**; `NewPolicy(Any(Leaf(ipallow), Leaf(sshkey)))` returns a
   **construction error**;
   `NewPolicy(All(Leaf(ipallow), Any(Leaf(ticket), Leaf(mtls))))` **is valid**.
   A contextual leaf below an identity-requiring `All` stays legal, and no policy
   satisfiable by contextual factors alone can be constructed.
2d. **Validity interval (§2.2.1):** `IssuedAt` is the latest contributing value,
   `ExpiresAt` the minimum finite non-zero one, a zero expiry imposes no bound, a
   static `ipallow` match contributes no expiry, and a consumed ticket's
   redemption deadline does **not** become `Identity.ExpiresAt`.
3. `NewPolicy(nil)` and a contextual-only root return an **error** (not a
   panic — a policy is often assembled from configuration); an empty `All()` /
   `Any()` node is a node that denies at evaluation. The two cases are
   distinguished by test.
4. Timing symmetry across **all five** paths — known, unknown, wrong, locked and
   tracker-full — asserted structurally (identical tracker operation *sequences*,
   recorded per attempt, plus the wrapped factor invoked exactly once) AND with a
   tolerant wall-clock check that survives CI noise. The wall-clock check is
   **negative-controlled**: it also times a deliberate O(cap) scan over the same
   table and **fails if that control is not at least 10x a normal insert**, since
   otherwise the measurement lacks the resolution to detect the regression it
   exists to catch and the assertion would be vacuous. Measured: at-capacity
   insert 1.5x a normal insert, O(cap) control 168x. A separate test proves
   proofs are decoded to a fixed length *before*
   `subtle.ConstantTimeCompare`.
4b. **Subject-source contract (§2.6b):** a factor that implements neither
   `Claimant` nor supplies `SubjectClaim` nor chooses `AddressOnly` fails
   **construction**, with an error naming all three; `AddressOnly` +
   `SubjectClaim` together is refused. An attempt naming no principal is
   isolated to its own address, proven by showing a second address and a named
   subject both remain admissible after the first address locks.
4c. **Locked-path side effects (§2.6b):** in address-only mode a locked attempt
   does **not** invoke the wrapped factor — asserted by a factor that counts
   credential consumption — so a valid single-use ticket is never destroyed on a
   denied attempt. It is still counted, so hammering a locked address cannot
   reset it.
4d. **Tracker outage (§2.6b):** the default denies with
   `ErrTrackerUnavailable` and does **not** invoke the wrapped factor;
   `FailOpen()` admits a correct credential; a failed `Reset` on the success path
   does not fail the authentication; and every case reaches `OnTrackerError`. A
   cancelled context reaches the tracker and produces the outage path, proving
   the seam can honor a deadline.
5. Lockout: N failures trigger backoff, success resets, and under a flood of
   distinct subjects and addresses the tracker stays **bounded** (no unbounded
   growth). Tracker keys are **fixed width** for values from 0 to 5 MiB, so the
   entry cap bounds memory and not merely a count; namespaces provably do not
   collide. The bound still holds for records dated **2040**. Invalid
   `Backoff` values and a negative size are **refused**, not normalized.
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
   challenge all fail. Signatures are produced by the **real `ssh-keygen -Y
   sign`**, and every OpenSSH-dependent case **skips** (never fails) where the
   binary is absent.
8b. **Verifier agreement (§2.5):** one fixture set — two real keys, one
   `allowed_signers` file and the in-process allowed set built from the *same*
   source so a divergence can only come from the verifiers — is run through
   **both** `OpenSSH` and `PureGo`, which must reach the same accept/reject
   decision on: valid; tampered message; signed under a different namespace;
   signer absent from the allowlist; an allowlisted principal claimed with a
   stranger's key; an allowlisted key claimed under an unknown principal; and
   garbage armor. Error *sentinels* are explicitly **not** required to match.
8c. **Identity claim (§2.5):** a request with no `ssh-identity` fails; a valid
   signature presented under a **foreign identity claim** fails (the case rev 4
   admitted); every rejected identity shape — empty, leading `-`, embedded
   newline, NUL, DEL, >256 bytes — is refused by **both** verifiers before any
   work; ordinary principals (`alice@host`, `alice+ci@example.com`) are accepted.
8d. **Delegation failure modes (§2.5):** an unset path, a missing file, a
   **directory**, and an **unreadable (mode-000) but present** `allowed_signers`,
   plus a missing binary and a binary that is a directory, each fail at
   **construction** with `ErrVerifierUnavailable` and **never**
   `ErrBadSignature`. A policy file deleted *after* construction fails the same
   way at call time. An empty-but-readable `allowed_signers` constructs and then
   **denies**. Zero-value `OpenSSH{}`/`PureGo{}` deny. `NewPureGo` rejects an
   empty set, an entry with no key, and an entry with no subject, and **clones**
   its input — a test mutates the caller's slice afterwards and asserts the
   allowlist did not change. `New` **panics** on a nil `Verifier` or nil store
   rather than choosing one silently.
8e. **Whole-tree cancellation (§2.5, r5 must-fix 1):** a stub that spawns a
   grandchild and waits on it is killed by timeout, and the test asserts **the
   grandchild's pid is gone** — polling `kill(pid, 0)` for ESRCH — not merely
   that the call returned. The wall-clock bound is `timeout + WaitDelay` plus CI
   slack, not a loose multi-second ceiling. A separate test asserts the child
   lands in **its own** process group and specifically not this process's, since
   a group kill against the wrong group would signal the test binary. The
   regression is verified in both directions: with `isolateProcessGroup`
   disabled the test fails on a surviving pid. The liveness probe requires
   **`ESRCH` specifically** — treating any `kill(pid,0)` error as "gone" would
   let `EPERM` on a recycled pid read as success — and a missing grandchild pid
   **fails** the test rather than skipping it, since a green run that never
   performed the assertion is a false pass.
8g. **Executability and platform capability (§2.5, r6):** a readable-but-not-
   executable binary fails construction and the same file constructs once
   `chmod +x`; a negative `VerifyTimeout` fails construction while zero yields
   the documented default; caller cancellation and the configured timeout produce
   *distinguishable* messages while both remaining `ErrVerifierUnavailable`.
   `auth/sshkey` cross-compiles for `windows/amd64` and `darwin/arm64`, and on a
   platform without POSIX process groups `NewOpenSSH` returns
   `ErrUnsupportedPlatform`.
8f. **Cancellation agreement (§2.5, r5 should-fix 1):** the same inputs that
   verify under a live context are refused by **both** verifiers under an
   already-cancelled one, with `ErrVerifierUnavailable`.
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
11. The **corrected** ADR-0009 WebTUI policy compiles and is exercised, both
    forms written out in full:
    `NewPolicy(Any(Leaf(ticket), Leaf(mtls), Leaf(sshChallenge)))` and
    `NewPolicy(All(Leaf(ipallow), Any(Leaf(ticket), Leaf(mtls), Leaf(sshChallenge))))`
    — with IP never satisfying either alone, and password rejected as a mechanism
    for that consumer.
12. `go vet` clean, race-clean, `doc.go` + `README.md` present, every exported
    symbol documented, tests stdlib-only.

## 6. Sequencing

The `Factor`/`Contribution`/`Node`/`Policy`/`Identity` graph and the composition
semantics (§2.1, §2.2) should be settled **first**, because ADR-0009's backend
depends on `Policy` and can proceed against it while the individual methods land
incrementally. Suggested order: core + `ipallow` + `token` (stdlib only, and enough for
ADR-0009's ticket flow) → `sshkey` + `mtls` (the strong mechanisms WebTUI
accepts) → `password` (other callers). `totp` is deferred entirely (§3).

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

- **r7 (2026-08-22, lector — `change_requested`; all five must-fixes and both
  notes folded in rev 8).** The r6 SSH work was **approved in substance**, as was
  running the full password verifier while locked. All five blockers were in the
  `Throttle`/`MemTracker` surface rev 7 added, and every one of them changed the
  design rather than a line:
  1. **The generic wrapper created a global lockout.** Guessing the subject key
     from two credential names meant every factor without one — `token` carries
     `ticket`, `mtls` has no subject until the chain verifies — shared a single
     `s:` counter, so one attacker could lock out everybody. Worse, on that
     locked path the token factor **atomically consumed a valid single-use
     ticket and discarded the proof**. Now an explicit `Claimant` capability with
     `AddressOnly` for opaque factors, a construction error when neither is
     available, and specified locked-path side effects (see §2.6b).
  2. **The entry cap did not bound memory** — the keys were attacker-controlled
     and unbounded. Hashed to fixed width, value truncated before hashing, and a
     256-byte subject cap in `auth/password`.
  3. **Eviction broke after 2038.** An absolute-time sentinel meant no candidate
     was ever selected once records were dated 2040, the insert happened anyway,
     and a cap-2 tracker grew 1, 2, 3 — reproduced through the public API.
  4. **Tracker-full work was O(cap) on every attacker-triggered miss** (~79 ms
     per 100 inserts versus ~43 µs) and criterion 4's wall-clock test was
     missing; my "identical shape" test compared only outer method names. Now
     bounded sampling, and a negative-controlled timing assertion.
  5. **The published seam could not support a multi-replica tracker** without an
     interface change — no context, no error. Added, with an explicit
     fail-closed-by-default outage policy.
  Notes: invalid `Backoff`/size values are refused rather than normalized; and
  the OpenSSH construction-success tests now skip on platforms where
  `NewOpenSSH` correctly refuses, which is what was failing the Windows test
  binary.

- **r6 (2026-08-22, lector — `change_requested`; all three must-fixes and all
  three notes folded in rev 7).** Rev 6 closed every r5 blocker, and
  `NewPureGo` rejecting an empty configured set was confirmed as the right
  fail-at-construction call. Three new blockers, all in the subprocess and
  construction paths I had just rewritten:
  1. **The post-`Run` group kill was itself unsafe.** `Run` includes `Wait`, so
     the child is already reaped and its pid released; that integer can be
     reused as an unrelated process-group leader, and `kill(-oldpid, SIGKILL)`
     would then kill a stranger's group. My comment proved only that it could
     not hit *our* group — not that it could not hit anyone's. Removed. The
     `Cancel`-path kill is the safe one because the unreaped child still holds
     the pgid, and a clean verify spawns nothing to clean up anyway.
  2. **The binary was validated as readable, not executable.** Lector
     reproduced a mode-0600 text file constructing successfully. Fixed via
     `exec.LookPath` for explicit paths too; a negative `VerifyTimeout` is now
     also a construction error.
  3. **Whole-tree cleanup silently vanished on non-unix**, while the README
     promised it — the r5 leak stayed reachable on Windows with no capability
     error. `NewOpenSSH` now refuses to construct there. I chose the typed
     refusal over an untested Job Object implementation guarding a security
     property.
  Notes: `ESRCH` specifically rather than any error; a missing grandchild pid
  fails rather than skips; the "nothing outside argv and the policy file affects
  the verdict" claim narrowed — validity windows depend on the system clock and
  non-`Z` timestamps on `TZ`, so both are in the trust base; and caller
  cancellation now reads differently from the configured timeout.

- **r5 (2026-08-22, lector — `change_requested`; all three must-fixes and all
  four should-fixes folded in rev 6).** The identity claim, the sentinel
  divergence, the dependency accounting and the `hiddeco/sshsig` rejection were
  all **approved** — and Argon2id is explicitly not reopened. The must-fixes were
  about the subprocess, not the design:
  1. **Cancellation leaked descendants — a live bug, reproduced.** `WaitDelay`
     closes pipes but kills nothing; `CommandContext` kills only the direct
     child. Lector's repro: the timeout fixture returned in ~1.1s and `pgrep`
     still showed its `sleep 30`. Fixed with an owned process group killed as a
     group, `WaitDelay` retained as the final I/O bound. The regression test now
     asserts the grandchild is **gone**, and fails on a surviving pid when the
     fix is reverted — I verified both directions rather than trusting the
     green run.
  2. **An unreadable-but-present `allowed_signers` was misclassified.** The
     preflight only `Stat`ed the path, which succeeds on mode-000 and on a
     directory, so the failure arrived as a nonzero exit and was mapped to
     `ErrBadSignature` — precisely the "outage reported as a bad credential"
     the error classing exists to prevent. Now the file is **opened**, at
     construction and per call, and the ADR's promise is narrowed for the
     unavoidable check-to-`exec` window.
  3. **The API violated golib's binding construction contract.** Public mutable
     fields deferred missing-path and missing-binary failures to the first
     authentication. Now `NewOpenSSH`/`NewPureGo` with unexported fields and
     functional options, PATH resolved once, policy readability validated at
     construction, and `PureGo`'s allowed set cloned.
  Should-fixes: `ctx` honored by both verifiers (an already-cancelled call was
  admitted by `PureGo` and refused by `OpenSSH` — an admission disagreement);
  stderr capped at 4 KiB; the timeout assertion tightened to
  `timeout + WaitDelay`; and the timing note stated precisely — pre-fork returns
  make verifier *health* observable, which is claim-independent and enumerates
  nobody, so no dummy fork is added to disguise an outage.

- **rev 5 (2026-08-22, jarvis — mechanism change).**
  Rev 4 justified `x/crypto/ssh` partly on not hand-rolling a signature parser;
  implementation then found `x/crypto` ships **no `sshsig` package**, so the
  envelope parser was ours after all — recorded at the time as a correction, but
  a correction that left our code as the default on a hostile-input security
  format. Rev 5 puts verification behind a `Verifier` seam and makes
  **`ssh-keygen -Y verify` the default**, keeping the rev 4 parser as `PureGo`
  for images without OpenSSH. `github.com/hiddeco/sshsig` was evaluated and
  rejected (two releases, one maintainer, pulls `testify` + `yaml`).
  Two consequences worth flagging to review: `-I` forces the client to **claim
  an identity**, which closes a real hole rev 4 admitted (a valid signature was
  accepted as its key's owner without the request ever asserting who it was);
  and forking a subprocess per verification brought its own hazards, one of
  which was a live bug — a cancelled context did **not** bound `cmd.Run`,
  because a descendant holding the stderr pipe kept `Wait` blocked for the
  full 30s of a hung stub. `WaitDelay` fixes it; the timeout test asserts it.
  `x/crypto` remains in the graph (allowed-key parsing, `PureGo`, Argon2id), so
  **§2.4's Argon2id decision stands** — Johno confirmed 2026-08-22 — and the
  dependency accounting in §2.3 is unchanged.

- **r4 (2026-08-21, lector — `approved_with_amendments`; both applied in rev 4).**
  The r3 structural work was accepted: one coherent closed
  `Factor` → `Leaf`/`Node` → `Policy` graph with the invariants split by level.
  Two amendments: acceptance criteria 2c and 11 (and the cross-ADR summary) still
  passed `Factor` values straight to `All`/`Any` while claiming the trees
  compiled — they now go through `Leaf(...)` and `NewPolicy(...)`, matching the
  §2.2.2 examples; and the deferred-`Attributes` bullet had swallowed the
  "each is a protocol surface plus dependencies" sentence, which belonged to the
  deferred-protocol list and had no plural antecedent where it sat.

- **r3 (2026-08-21, lector — `change_requested`, folded in rev 3).**
  **Must-fix 1: the type graph was incomplete** — rev 2 wrote
  `NewPolicy(root Node)` without ever defining `Node`, while `All`/`Any` still
  had the signature of the `Authenticator` interface rev 2 had just removed, so
  the north-star policy could not type-check. One graph now: a **closed** `Node`
  (unexported method), `Leaf(Factor) Node` as the only way an external factor
  enters, `All`/`Any` over `Node`, and `NewPolicy(Node) (Policy, error)` — and
  all three example trees are written in compiling form. **Must-fix 2:** the
  invariants were stated at one level for two different types; they are now
  split — `Factor.Verify` error ⇒ **zero `Contribution`**,
  `Policy.Authenticate` error ⇒ **nil `*Identity`** — acceptance criterion 3 no
  longer demands a panic (`NewPolicy` returns an error, because policies come
  from configuration), and `Contribution.Kind` is **removed** rather than
  duplicated, with `Subject` presence validated against `Factor.Kind()` at
  evaluation time so the classification used for validation cannot drift from the
  one used at runtime. **Should-fixes:** the `Identity` field comments now say
  *latest* non-zero `IssuedAt` and *minimum finite non-zero* `ExpiresAt`;
  remaining `Authenticator` references in Related and Sequencing replaced with
  `Policy`; "six method subpackages" corrected to five after the TOTP deferral;
  and the malformed deferred-`Attributes` paragraph repaired.

- **r2 (2026-08-21, lector — `change_requested`, folded in rev 2).** The r1
  substance was accepted; these were contradictions I introduced. **Must-fix 1
  was a type contradiction:** rev 1 gave leaves `Kind()` *and* embedded an
  `Authenticator` returning `*Identity`, while the invariant demanded every
  success carry a non-empty `Subject` — so `ipallow` could never legally succeed
  at all. Leaves are now `Factor`s returning a `Contribution` (contextual
  contributions carry no subject), and only a validated `Policy` concludes an
  `Identity`. **Must-fix 2:** `All`/`Any` still returned plain authenticators, so
  the composite kind was invisible and an `Any` could not know it was the root —
  rev 1's construction check was therefore unimplementable. The kind is now
  *computed* (`All` bears identity if **any** child does; `Any` only if **every**
  branch does) and `NewPolicy` validates the **finished tree**, which also makes
  `Any(mtls, All(ipallow, sshChallenge))` legal — I had feared rev 1 banned it,
  and lector confirmed the nested form must be valid. **Must-fix 3:** the
  validity interval is an intersection — **latest** `IssuedAt`, **minimum finite
  non-zero** `ExpiresAt`, zero meaning no bound — a static `ipallow` match
  contributes no expiry while a continuing finite assertion does, and a ticket's
  redemption deadline must never become the session lifetime (`tui/web` owns
  that). **Should-fixes:** claim-conflict semantics removed while `Attributes` is
  deferred; the stale counts and statements cleaned (3/5 leaves import-free, not
  4/5; argon2id recorded as the reversal rather than still "rejected"; the SSH
  flow marked decided; `totp` dropped from sequencing); and an empty factor node
  (denies) separated from an empty or contextual-only policy (construction
  error).

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
  `NewPolicy(Any(Leaf(ticket), Leaf(mtls), Leaf(sshChallenge)))`, optionally
  wrapped in `NewPolicy(All(Leaf(ipallow), Any(...)))` — IP optional and never
  identity-bearing.
  Review doc: `$KB_ROOT/agents/lector/reviews/2026-08-21-golib-tui-web-auth-coupled-design-review.md`
