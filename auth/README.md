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
| `auth/sshkey` | `authorized_keys` + signed challenge | identity | `x/crypto/ssh` |
| `auth/mtls` | verified client-cert chain | identity | stdlib |
| `auth/password` | Argon2id | identity | `x/crypto` *(planned)* |

The core imports **no third-party module**.

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

## Never

Ask a user to paste a private key into a browser or a form. `auth/sshkey` will
verify a **signature** over a server nonce, or a ticket minted over an already
authenticated SSH channel — never key material in transit.

## License

See the repository [LICENSE](../LICENSE).
