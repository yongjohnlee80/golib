# auth

Authentication for golib services: pluggable methods behind one interface,
composition as a first-class feature, and secure defaults.

It answers exactly one question — **does this request carry a valid
credential?** — and returns an `Identity` or an error. Authorization, sessions
and user management are out of scope.

```bash
go get github.com/yongjohnlee80/golib
```

## The shape

```go
policy, err := auth.NewPolicy(auth.All(
    auth.Leaf(ipallow.New([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})),
    auth.Any(auth.Leaf(ticketFactor), auth.Leaf(mtlsFactor)),
))
if err != nil { /* the tree is invalid — see below */ }

id, err := policy.Authenticate(ctx, req)
if err != nil {
    // ALWAYS auth.ErrUnauthenticated. The reason is in the audit record.
}
```

- **`Factor`** — one leaf check: `Verify(ctx, *Request) (Contribution, error)`
  plus `Kind()`. A contextual factor returns no `Subject`; an identity factor
  must.
- **`Leaf` / `All` / `Any` / `NewPolicy`** — the only composition surface.
  `Node` is closed, so `NewPolicy` can validate the finished tree.
- **`Request`** — transport-agnostic (no `net/http`), so an RPC server, a
  WebSocket handshake and a CLI prompt all construct one.

## Contextual factors cannot authenticate alone

A composite's kind is computed: `All` bears identity if **any** child does;
`Any` only if **every** branch does. So this is refused at construction:

```go
auth.NewPolicy(auth.Any(auth.Leaf(ipallow), auth.Leaf(sshkey)))
// -> ErrNoIdentityProof: the ipallow branch alone would admit
```

An address is a **factor, never an identity**. The rule is enforced by the type
system rather than by a comment telling you not to.

## Methods

| Package | Method | Kind | Dependency |
|---------|--------|------|------------|
| `auth/ipallow` | CIDR allowlist | contextual | stdlib |
| `auth/token` | opaque tokens + single-use tickets | identity | stdlib |
| `auth/sshkey` | `allowed_signers` + signed challenge, verified by `ssh-keygen -Y verify` | identity | `x/crypto/ssh`, `os/exec` |
| `auth/mtls` | verified client-cert chain | identity | stdlib |
| `auth/password` | Argon2id (PBKDF2 for FIPS) | identity | `x/crypto/argon2`, stdlib `crypto/pbkdf2` |

Plus `auth.Throttle` + `auth.MemTracker` in the core: per-subject and
per-source-address failure counting with exponential backoff.

The core imports **no third-party module**.

## Adapters

The core stays free of `net/http`, so one policy value serves a web handler, an
RPC server and a CLI prompt.

| Adapter | Use |
|---------|-----|
| `auth/authhttp` | `Middleware(policy)`, `FromRequest`, `IdentityFrom(ctx)` |
| `auth.FromConn` | `server/rpc`, `server/ws`, a plain listener |

`auth/authhttp` copies an **allowlist** of headers into `Metadata` — never
`Cookie` or `Authorization`, which would land in a plain map that `Secret` does
not protect; naming one panics. Credentials go to `Credentials`, where they
redact, and the bearer projection uses `token.DefaultScheme` so the adapter and
the factor cannot drift apart. Configuration is resolved once at construction and
its inputs are cloned: an allowlist that can change after the middleware exists
is not configuration, it is a race.

## Things that are easy to get wrong, and what this does

- **Uniform failure.** Every rejection is `ErrUnauthenticated`. The specific
  reason goes to a private audit record with a correlation ID — an operator
  diagnoses from the attempt ID, an attacker learns nothing.
- **Forwarded headers are untrusted by default.** `auth/ipallow` ignores
  `X-Forwarded-For` unless `TrustedProxies` is configured, and then walks the
  chain from the **right**, skipping trusted hops. Taking the leftmost value is
  the classic spoof.
- **Single-use redemption is atomic.** `token.Store.Consume` is one operation,
  not verify-then-delete, which would race two redeemers into both succeeding —
  for a WebTUI attach ticket, that is two sessions from one credential.
- **Tokens are hashed at rest** and fixed-length, so presented material is
  length-checked and decoded before it reaches anything.
- **Secrets redact structurally.** `Secret` covers `String`, every `fmt` verb,
  JSON and text marshaling. It does **not** claim memory erasure.
- **Deny by default.** An empty allowlist denies; an empty `All()`/`Any()` node
  denies; a nil or contextual-only root is a construction error.
- **A certificate that verified, not one that was presented.** `auth/mtls`
  accepts only a chain in `VerifiedChains`; the adapter refuses to carry
  `PeerCertificates` across at all, so no later reader can authenticate from an
  unverified certificate.
- **SSH challenges are single-use even when the attempt fails**, domain-separated
  by namespace, and bound to session/origin *inside the signed message* — so a
  binding cannot be swapped after signing. A signature made for another
  namespace (a git signing flow, say) will not verify here, which is tested
  against real `ssh-keygen` output.
- **The signature check is delegated to OpenSSH, not reimplemented.**
  `sshkey.OpenSSH` runs `ssh-keygen -Y verify`, so the format's reference
  implementation decides — and `allowed_signers` validity windows and
  `cert-authority` lines work because OpenSSH, not us, is reading the file.
  `sshkey.PureGo` parses in-process for images with no `ssh-keygen`; tests assert
  the two reach the **same** accept/reject decision on a shared fixture set.
- **The client claims an identity and the claim is checked.** `ssh-keygen -Y
  verify` answers "did *this principal* sign this?" — so a valid signature
  presented under someone else's name fails, rather than being accepted as
  whoever owns the key. The claim is screened before it reaches `argv`
  (no leading `-`, no control bytes, bounded length) and there is no shell in
  the path.
- **A broken verifier is not a rejected user.** A missing `ssh-keygen`, an
  unreadable `allowed_signers`, or a hung subprocess yields
  `ErrVerifierUnavailable`, never `ErrBadSignature`, so an operator can tell a
  misconfiguration from a denial. Most of those fail at **construction** —
  `NewOpenSSH` resolves the binary once and proves the policy file readable by
  *opening* it, because `Stat` succeeds on a mode-000 file and the failure would
  otherwise arrive later disguised as a bad signature.
- **A timed-out verification kills the whole process tree.** Cancelling an
  `exec.CommandContext` kills only the direct child and `WaitDelay` only closes
  the pipes, so a descendant survives both; the child runs in its own process
  group and the group is killed *from the cancellation path* — never after
  `Wait`, by which point the pid is released and could name a stranger's group.
  Platforms without POSIX process groups cannot make this guarantee, so
  `NewOpenSSH` **refuses to construct** there rather than quietly not doing it;
  use `NewPureGo`, which forks nothing.

- **A password credential says how to check itself.** Parameters are stored
  *with* the hash (`$argon2id$v=19$m=65536,t=3,p=4$salt$digest`), so tuning the
  cost never invalidates existing credentials, and a successful login rewrites a
  credential written under different parameters — the only moment the plaintext
  exists to rehash with. A failed rehash never fails the login.
- **An unknown user costs what a known one costs.** `auth/password` hashes
  against a dummy credential built with the *same* parameters, so "no such user"
  cannot be told from "wrong password" by timing. A corrupt stored credential or
  a broken store reports to the operator and rejects uniformly outward.
- **Per-subject lockout needs a declared capability.** `auth.Throttle` requires
  the wrapped factor to implement `auth.Claimant` — naming the principal an
  attempt targets *before* verifying — or an explicit `SubjectClaim`, or
  `AddressOnly()`. It will not guess: a factor whose identity only exists after
  verification (an opaque token, an mTLS chain) would otherwise land on one
  shared counter, and any single attacker could lock out every client of it.
- **In subject mode a locked attempt still does the work.** The wrapped factor
  runs and the verdict is discarded. Short-circuiting would make the locked path
  return in microseconds against tens of milliseconds for a wrong password — and
  since only an *existing* account can be locked out, timing lockout enumerates
  users. It defends credentials, not CPU; bound request volume at the transport.
- **In address-only mode a locked attempt does NOT.** There is no principal to
  enumerate, and running the factor would let a single-use token be consumed and
  its proof thrown away on an attempt that was already denied.
- **A tracker outage denies by default.** A failure counter that cannot answer
  cannot protect anything, so continuing would mean brute-force protection
  silently off. `FailOpen()` trades that for availability, deliberately.
- **A locked attempt counts as a failure.** Otherwise a correct guess arriving
  during backoff resets the counter and the backoff is bypassable by retrying.
- **The failure counters are bounded, and so are their keys.** An entry cap
  bounds the *number* of records; keys are hashed to fixed width so it bounds
  *memory* too — otherwise 16k attacker-chosen strings of any length sit inside
  the cap. Making room examines a bounded sample rather than scanning the table,
  because an O(cap) scan on every miss at capacity is both a DoS amplifier and a
  timing signal for "new key at capacity".
- **Every attempt gets a correlation ID, and it reaches the response.** The
  caller-visible error says nothing, so the audit record carries a random
  per-attempt ID to `logger.Logger` (default `Nop`) — success at Info, failure at
  Notice, because a failed login is the system working — and `authhttp` returns
  it as `X-Auth-Attempt` so a user can quote something an operator can find. The
  ID is captured on the *request's* context, because a policy-global observer
  cannot tell two concurrent requests from one peer apart — and nested sinks
  compose rather than shadow, so adding an adapter never silently disables an
  observer the caller installed. Every rejection's body and status are identical;
  this header is the only thing that varies, and it is random and
  outcome-independent by construction.
- **An arbitrary error's text never reaches the log.** A factor is third-party
  code, and `fmt.Errorf("bad token %q", presented)` is an ordinary thing to
  write. Only `auth.Reason` (compile-time text) or an error implementing
  `auth.SafeAuditDetail` contributes its words; anything else is recorded as
  `opaque error of type T`. Wrapping a `Reason` keeps the fixed half and drops
  the dynamic one. Every built-in sentinel across all six packages is a `Reason`,
  so a malformed token still reads differently from an expired one — asserted by
  comparing recorded reason details, not rendered log lines, which carry a random
  per-attempt ID that would make any two of them look distinct.
- **`Any` does not lose the reason.** Branch errors are joined, so a backoff
  refusal inside a fallback policy still logs `throttled` no matter which branch
  it was declared in.
- **Every rendered field is sanitized and bounded** — subject, peer, reason and
  *method* alike. All of them are factor- or request-supplied, and a newline in a
  log field is how a log gets forged entries.
- **A stored hash is untrusted data.** Cost parameters are range-checked on
  *read* as well as write: without that, anyone who can write to the credential
  store turns each login into a 4 GiB allocation.

## Never

Ask a user to paste a private key into a browser or a form. `auth/sshkey` will
verify a **signature** over a server nonce, or a ticket minted over an already
authenticated SSH channel — never key material in transit.

## License

See the repository [LICENSE](../LICENSE).
