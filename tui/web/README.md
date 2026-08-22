# tui/web

Render an existing golib TUI in a browser. Implements `tui.Backend` over a
server-side cell grid: the browser displays cells and reports input.

```go
backend := web.New()
app := tui.NewApp(root, tui.WithBackend(backend))
```

No `Component`, layout, or widget code changes. That is the whole point — and
§2.10's seam report below is the honest accounting of whether it was true.

## What this is for

A user who can already reach a CLI server gets its TUI in a browser without a
second UI being written. It is deliberately **not** a web application: an app that
wants a web front end should use a real front-end framework, and this would be the
wrong foundation for one.

## Deployment

The documented primary deployment is an **SSH local-forward against a loopback
bind** — no open port, and the SSH hop is already authenticated and encrypted:

```
ssh -L 8080:127.0.0.1:8080 host
```

A **non-loopback bind without TLS refuses to start.** That is an error, never a
warning: a warning in a log is not a control. An unspecified address (`""`,
`:8080`, `0.0.0.0:8080`, `[::]:8080`) is *not* loopback — treating an empty host
as local is exactly how a terminal ends up on the internet.

```go
mgr, _ := web.NewManager(func(b *web.Backend) web.Runner {
    return tui.NewApp(buildRoot(), tui.WithBackend(b))
})
h, err := web.NewHandler(web.Config{
    Addr:           "127.0.0.1:8080",
    Policy:         attachPolicy,               // required
    AllowedOrigins: []string{"https://tui.example.com"},
    ExpectedHost:   "tui.example.com",          // required, never inferred
}, mgr)
if err != nil {
    return err // a misconfigured WebTUI must not start
}
return h.Serve(ctx)
```

`Serve` is the path where the transport guarantees hold, because it builds the
listener from the config that was validated. `Mount` composes into a server you
built, and then the bind is yours.

## Authentication

Mandatory. There is no unauthenticated mode, not even on loopback.

| Mechanism | How it attaches |
|---|---|
| Ticket minted over the user's SSH session | default; single-use |
| mTLS | attaches directly on a verified chain |
| SSH signature (`ssh-keygen -Y sign`) | attaches directly; challenge binds session + origin |
| Password | `POST /login` → single-use ticket → attach |

**A reusable secret is never an attach credential.** The attach protocol carries
no password fields at all, so a password cannot be presented there — it converts
to a ticket at the login route. mTLS and the SSH challenge attach on their own;
the ticket is not the universal shape, it is the shape a *password* converts into.

The ticket is **single-use with a 30-second lifetime, accepted behind a
separately-enforced `Origin` allowlist**. It is not origin-bound: `auth/token`
carries no origin, so the protection is composed of two independent controls
rather than carried by the credential.

Password auth is permitted and is the weakest option. `web.PasswordPolicyExample`
requires an `auth.Throttle` and a *contextual* constraint, and refuses an identity
factor in that position.

## Things that are easy to get wrong, and what this does

- **The pending frame is cumulative, by construction.** Two grids are kept —
  current server truth and the last baseline the client *acknowledged* — and each
  frame is their difference, computed at send time. A frame that merely *replaces*
  a pending one loses data permanently: frames carry only dirty cells, so if the
  frame holding row A is dropped and the next change touches only row B, a
  replacement carries B alone and row A never arrives again.
- **Sending is not acknowledging.** The baseline advances only on the ack for the
  exact revision in flight. A stale or forged ack is ignored, because the baseline
  decides what the next diff *omits*.
- **`Flush` never blocks on a client.** A vanished browser cannot stall the App
  loop.
- **A wide grapheme is contained geometrically, not typographically.** A
  `Width: 2` head spans exactly two grid tracks; a `Width: 0` continuation emits
  no element at all. Cell boxes clip their own overflow, so a font that renders a
  glyph wider than its box clips it rather than shifting the row. The width probe
  informs the `UnicodeCore` capability; the boxes are the guarantee.
- **The capture element is drained on every emission.** Without that, every
  character ever typed — passwords included — would sit in the DOM of a
  network-reachable page. No claim is made that the text is erased from memory;
  only that it is no longer in the DOM or held by this code.
- **Handshake controls run before the upgrade.** `Origin` and `Host` are
  credential-free checks, so a cross-origin probe never reaches the auth
  machinery. `Origin` matching is exact — no suffix matching, because
  `*.example.com` matches `evil.example.com.attacker.test` — and an *absent*
  `Origin` is denied, since an attacker can omit it as easily as a non-browser
  client can.
- **The pre-auth waiting room is bounded.** `MaxSessions` bounds only what exists
  after a successful hello, so `MaxPending` plus a hello deadline bound what does
  not.
- **Session takeover is refused.** One connection per session; a second live
  attach is `ErrSessionBusy`. Two browsers sharing a grid, a cursor and an event
  stream is a different product.

## Reserved browser shortcuts

These are **not** forwarded and **not** `preventDefault`ed, so an app cannot see
them: `Ctrl`/`Cmd`+`T`/`N`/`W`/`L`/`R`, `Ctrl+Tab`, `F5`, `F11`, `F12`, `Cmd+Q`.
A user who cannot open a tab, reload, or reach devtools has lost control of their
own browser. `Ctrl+Q` is deliberately *not* reserved.

## Capabilities

`ColorProfile: TrueColor`, `SyncOutput: true`, `BracketedPaste: true`,
`InBandResize: true`. `Mouse` is `TriYes` only for a client reporting a pointer.
`KittyKeyboard` is **always false** — there is no browser analogue, and an
optimistic request is never reported as support. `UnicodeCore` only on the
client's font agreement, and even then the probe informs the bit rather than
proving it.

`KeyEvent.Base`/`Shifted` are always `0`. `KeyboardEvent.code` is a *physical key*
identifier and the DOM exposes no base-layout or shifted codepoint; in `tui/term`
those fields are populated only on the kitty CSI path, so `0` is exactly what a
non-kitty terminal produces.

## §2.10 — the seam report

ADR-0009's second reason for existing was to test whether `tui.Backend` was drawn
in the right place, by implementing something that is not a terminal. This is the
result. It is specific because a vague version would be worthless.

**The seam held. Nothing under `tui/` changed outside this package and the
examples** — mechanically verified:

```
git diff --name-only main -- tui/ | grep -vc '^tui/web/\|^tui/examples/'   # 0
```

The example paths are the demo extraction (criterion 1), which criterion 2
explicitly permits. An earlier version of this line excluded only `tui/web/` and
claimed the result was empty; it was not, and the command I actually ran to check
excluded both. But four things cost something, and two were actively good.

### Findings with real cost

**0. `AppFactory` could not see who the session was for — and the fix was not the
one-liner it looked like.**

`AppFactory func(*Backend) Runner` never received the authenticated identity,
though `Manager.Create` held it and called the factory on the next line. A factory
blind to identity cannot build a per-user `App`, so single sign-on was not
implementable by any consumer.

*What it cost:* it blocked autodb's web gateway outright. Worse, the obvious fix —
add the identity to the signature — was **necessary but not sufficient**: the
login route allocates upstream state before the hello reveals whether the attach
will Create or Reattach, and the factory runs only on Create. So a login used to
reattach allocated state nothing could claim, at a full cap the accounting
deadlocked, and a subject-keyed workaround could not separate concurrent logins by
one user. ADR-0009 §2.12 is the actual fix.

*What a future non-terminal backend should do:* assume any consumer with more than
one user needs identity **and** a per-login correlation with a named release path
on every outcome. **This is the finding this report could not have produced from
inside the package** — nothing in a single-user demo exercises it, which is worth
noting about seam reports generally: the first two entries came from writing the
backend, and the one that mattered most came from the first consumer that did not
look like the demo.


**1. `Err() error` has one slot, and a reconnectable backend needs two.**

The contract says "the terminal error that stopped the reader". For a terminal
those are the same event: the device failed, the backend is dead. For a network
backend a dropped connection is *not* a dead backend — the session should survive
so a reconnect can resync. Implementing `Err` literally meant a read error called
`Fail`, which stopped the `Backend`, which killed the `App`, which made ADR-0009
§2.8's detach window unreachable: a laptop lid closing destroyed the user's work.

*What it cost:* a real defect, found in review, not by me.

*What a future non-terminal backend should do:* distinguish **transport failure**
from **backend failure** from the start. An IoT or mobile backend will have the
same shape — a link drops, the surface is still there. The seam would be better
with either a second method or a documented rule that `Err` means only
*unrecoverable*.

**2. `Flush` says nothing about delivery, so every backend invents its own.**

ADR-0003's one-write rule assumes a synchronous device write, and the seam
inherits that assumption silently. `Flush(diff) error` returning `nil` means
"accepted", and for a terminal that is very nearly "delivered". Over a network it
means neither. Everything in `framer.go` — the acknowledged baseline, the
cumulative aggregate, the revision numbering, the resync-on-reconnect — exists to
fill a gap the seam does not mention.

*What it cost:* the single largest piece of design work in the package, and the
rev-0 ADR got it wrong (latest-wins coalescing, which loses rows permanently).

*What a future backend should do:* assume `Flush` is fire-and-forget and that
delivery is entirely the backend's problem. Do not assume a dropped frame can be
recovered by waiting for the next one; with dirty-cell diffs it cannot.

### Findings that are vocabulary, not structure

**3. `Capabilities` cannot say "not applicable", only "no".**

`KittyKeyboard`, `SyncOutput`, `InBandResize`, `UnicodeCore`, `BracketedPaste`,
`Undercurl` are `bool`. For a browser, `Undercurl: false` is honest but misleading
— CSS `text-decoration-style` exists and is simply not the same feature, so the
answer is "the question does not apply" rather than "no". `Mouse` gets this right
with `Tri`; the booleans do not.

*What it cost:* nothing functional. Some doc comments explaining why a `false` is
not a "no".

*What a future backend should do:* expect to answer terminal questions. If the
capability set grows, `Tri` is the right shape for anything a non-terminal cannot
meaningfully answer.

**4. `CellColor` presumes a palette owner.**

`CellColorANSI`/`ANSI256` carry an index, which resolves against *someone's*
palette — a terminal's. A browser has none, so this package emits
`var(--aN)` and ships the palette in its own stylesheet. Reverse video on
default colors also needed a "theme default" concept `CellColor` has no kind for,
so this package defines a rendering-only marker deliberately outside
`CellColorKind`'s range. That is a smell, and it is documented as one.

*What it cost:* about thirty lines and one uncomfortable constant.

*What a future backend should do:* if it has no palette, plan to supply one. The
alternative — a `CellColorKind` for "theme token" — would be a `tui` change, and
this package deliberately made none.

### What was actively well-shaped

**5. Latched cursor state.** Four void methods whose effect is deferred to the
next `Flush` is exactly right for a backend that batches. Nothing needed
adapting; the frame carries the cursor and that is that. A design that returned
errors or wrote immediately would have forced this package to buffer them itself.

**6. `Cell{Content, Width, Attrs}` and the `Width: 0` continuation.** The grapheme
cell translated to HTML with no impedance at all — `Width: 2` becomes a two-track
span and `Width: 0` becomes *no element*, which is precisely the behaviour needed
to keep a row aligned. Whoever chose to make continuation explicit rather than
implied saved this package the entire class of off-by-one row-drift bug.

### The honest summary

Two costs, both in the *contract's silence* rather than its shape: `Err` conflates
transport with backend, and `Flush` does not say what delivery means. Neither
required a change to `tui`, and both are documentable. The data types needed
nothing. That is a better result than I expected before starting, and the two
findings are specific enough to act on when the IoT or mobile backend arrives —
which was the point of doing this at all.

## The demo, and criterion 1

`tui/examples/webdemo` runs **the same component tree** as the terminal demo, with
`web.New()` in place of `term.Open()`. That is the only difference between the two
`main` functions.

To make the claim literal rather than approximate, the tree moved out of
`package main` into `tui/examples/demoapp` — a pure move, with the component logic
untouched; only the package clause and the constructor's visibility changed,
because `package main` cannot be imported. Its own interaction script (which runs
against `tui.TestBackend`, no PTY) moved with it and still passes.

`TestCriterion1_SameTreeOnTheWebBackend` asserts the property mechanically rather
than by inspection: the demo tree renders a non-blank full frame at the client's
measured size, repaints on a keystroke, follows a resize, and exits cleanly —
through `web.Backend`.

```
go run ./tui/examples/webdemo
# open the printed http://127.0.0.1:8080/#t=... in a browser
```

The ticket is in the URL **fragment**, which browsers never send to a server, and
the client scrubs it from the address bar before connecting. It is single-use.

## The browser matrix

`tui/web/browsertest` drives real engines through Playwright against a real
`tui/web` server. §2.9's text-machine behaviours depend on things the specs
decline to promise — whether an engine updates a control *before* dispatching
`compositionend`, whether `getModifierState("AltGraph")` is reported at all — and
**synthetic dispatch is exactly what would hide a divergence.** A suite that
fabricates events tests the fabrication.

CI is wired with a single required check that fails unless every engine passes.

**Current status: all three engines run and pass in CI** (2026-08-22, PR #14 run
32575804138 onward). The gate is **satisfied by evidence**. It had been waived for
v0.3.8, when Firefox and WebKit were unrun; that waiver is spent, and
`browsertest/RESULTS.md` keeps it as history along with what closing it bought.

Three harness defects and **no product defect** across the three engines. The
divergences this suite was built to catch — a control updated before
`compositionend` dispatches, a composition-associated `input` in a later task,
`getModifierState("AltGraph")` — do not exist: every one of those cases passed on
Gecko and WebKit unchanged.

## Single sign-on: use `web.SSO`

A consumer whose users authenticate against an **upstream** service (rather than a
local password store) must carry per-login state — an authenticated upstream
session — from the login route to the `App` built for that login.

**Use `web.SSO`. It is the supported path and it exists so this cannot be got
wrong.** See `Example_singleSignOn` in the godoc for a complete wiring.

```go
sso, err := web.NewSSO(web.SSOConfig[*myUpstream]{
    Max: 8, TTL: 30 * time.Second,
    // For attaches that carried no login: ticket, mTLS, SSHSIG.
    Provision: func(ctx context.Context, id *auth.Identity) (*myUpstream, error) {
        return dial(ctx, id.Subject)
    },
    // REQUIRED. Every path that discards a value goes through here.
    // Revoke BEFORE closing: closing a transport does not revoke a credential.
    Release: func(u *myUpstream, r web.HandoffReason) { u.Logout(); u.Close() },
})
handlerOpt, managerOpt := sso.Options()      // both hooks, together

// in the login factor's Verify — the only place holding the credential:
sso.Stash(ctx, upstream)

// the app: one build function for every mechanism, release handled
mgr, err := web.NewManager(
    sso.Factory(func(b *web.Backend, id *auth.Identity, up *myUpstream) web.Runner {
        return myApp(b, id, up)
    }),
    managerOpt,
)
```

A consumer writes three things: **how to allocate for a login** (`Stash`, in
`Verify`), **how to allocate for an attach that carried no login** (`Provision`),
and **how to release** (`Release`). The helper owns everything between.

### Every `golib/auth` mechanism, one route

`golib/auth` has two kinds of mechanism, and they allocate at different moments:

| Mechanism | Authenticates at | Upstream state comes from |
|---|---|---|
| a **custom** login factor that calls `Stash` | the **login** | `Stash` → parked → **claimed** |
| `auth/password` (the stock factor) | the **login** | **`Provision`** — it verifies a hash and cannot call `Stash` |
| `auth/token` (SSH-minted or out-of-band ticket) | the **attach** | **`Provision`** |
| `auth/mtls` (verified chain) | the **attach** | **`Provision`** |
| `auth/sshkey` (SSHSIG challenge) | the **attach** | **`Provision`** |
| `auth/ipallow` (contextual) | narrows all of the above | allocates nothing |

`Claim` misses for every `Provision` row, and *that* is the signal — not an empty
handoff. Every presented ticket derives a `HandoffID`, including one minted out of
band, so the park is the authority on whether a login parked anything.

`sso.Factory` hides the difference: the build function receives a ready upstream
session whichever way the user got in. `sso_e2e_test.go` drives all of them
through the real serve path, including the stock `auth/password` factor and a real
SSHSIG signature.

### Shutdown order

**Stop the handler, shut the `Manager` down and let its sessions finish, then
`sso.Close()`.** `Close` blocks until every `Provision` already in flight has
returned and released. It collects what no session ever took — parked entries and
in-flight provisioning — and nothing else: a live App holds its own upstream
session and releases it when it ends, which is why draining the `Manager` comes
first and not after. In the other order a session that is only just starting can
provision state after the park has stopped taking responsibility for it; `Session`
refuses after `Close` precisely so that mistake fails loudly instead of leaking.

### Panics

An App panic ends **that session**, not the process: `Manager` contains it and
records it as the session's run error, the same way `server.Scaffold` contains a
connection handler's panic. That is what makes `Factory`'s deferred release
meaningful on the panic path. A panic inside your `Release` is contained too, so
it cannot replace the failure being handled — but `Release` should not panic, and
one that does is **logged as an error** (pass `SSOConfig.Logger`) and counted by
`SSO.ReleasePanics()`. A non-zero count means upstream state was abandoned
mid-cleanup, which belongs on a dashboard.

A **nil** `Provision` **refuses** an attach that parked nothing, rather than
handing the app a nil upstream — which would fail later and further from the
cause. For guest sessions, return a guest value from `Provision`, so there is one
mechanism rather than a flag.

### Why a helper rather than documentation

The raw seam has four paths — park on login, claim on create, release on
reattach, release on failed attach — plus an expiry sweep, plus the release when a
session ends and the second allocation path for attaches that never logged in.
Miss any one and upstream state leaks, and the easiest to miss is **reattach**,
because nothing in the happy path exercises it. A protocol with that many
obligations described in prose is a protocol someone implements three-quarters
of.

So the obligations are structural:

| Obligation | How it is enforced |
|---|---|
| release on every path | `Release` is **required**; `NewSSO` fails without it |
| both hooks are one value | `Options()` returns them **together**, so you never have to know the second exists (Go being Go, you can still discard one) |
| clean up abandoned logins | each parked entry carries **its own timer** |
| an expired entry is never served | `Claim` refuses and releases it |
| release before the park | `SSO.Stash` registers the cleanup **with** the value, so a later refusal, a failed ticket or a full park releases it |
| one login, one value | a second `Stash` in one request is **refused** |
| admission slots come back | the slot follows the handoff, and **the park returns it** — when the entry is claimed, released or expires, never before |
| nothing outstanding after `Close` | `Close` **waits** for in-flight `Provision` calls; one that finishes late releases its own value |
| one session per login | `Claim` removes as it hands over |
| release when the session ends | `Factory` **defers** it — panics included, because `Manager` contains an App panic |
| no path allocates twice | `Session` claims *or* provisions, never both |
| no app sees a nil upstream | a nil `Provision` refuses the session |
| nothing is allocated after shutdown | `Session` refuses once `Close` has run |

### The raw hooks

`OnLogin`, `OnHandoffUnused`, `Stash`, `HandoffID` and `MaxPendingLogins` remain
exported for a consumer whose park must live somewhere else — a shared store
across replicas, say. `SSO.Claim` and `SSO.Session` likewise remain exported for a
consumer writing their own `Runner`; if you use `Session` directly, **defer the
release it returns**. Reach for the raw hooks and you take on every obligation in
the table yourself, and the reattach path is the one to write a test for first.

Four things about this are load-bearing, and each was forced by a specific defect:

- **The handoff is derived from the ticket** (`HandoffID`), so this package stores
  nothing and only the ticket holder can compute the key. It is domain-separated
  from the token store's own `sha256(ticket)` index — two hashes of one secret for
  two purposes must not be the same hash.
- **`Stash` is per-REQUEST, not keyed by subject.** A consumer's `Verify` holds the
  credential and is the only place that can allocate, but the handoff is not known
  until the ticket is minted. A subject-keyed park cannot separate two concurrent
  logins by one user; one slot per request removes the shared key entirely.
- **`OnHandoffUnused` covers the paths no factory runs on.** A reattach resumes an
  existing App, so a reconnecting client's fresh login parks state that nothing
  claims. Without this hook that leaks on every reconnect — the defect the whole
  seam exists for.
- **Parked logins have their own budget** (`MaxPendingLogins`), separate from
  `MaxSessions`. Counting them together deadlocks a reconnect at a full session
  cap, because a reconnect must log in *before* it can reattach.

An `OnLogin` returning an error **fails the login**: a client must not receive a
ticket for state that was never recorded.

## Peer binding

`web.BindPeer(true)` refuses an attach from a different address than the one that
created the session, and terminates the session. **Off by default**, for two
reasons worth knowing before turning it on:

- **Under the SSH local-forward above it is a no-op.** Every connection arrives
  from `127.0.0.1`, so it binds to a constant.
- **It trades away the detach window.** A laptop moving from wifi to cellular
  changes address and gets logged out.

It raises the cost of using a stolen ticket *from a different host*. It does
nothing against an attacker on the same host or behind the same NAT.

## Not done

- Persisting the client session id in `sessionStorage`, so a reload reattaches
  instead of creating a second session.
