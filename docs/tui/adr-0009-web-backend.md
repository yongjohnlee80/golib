# ADR-0009 — `golib/tui`: the web Backend (remote TUI over HTTP)

- **Status:** **Accepted (rev 20)** (2026-08-21 — authored by jarvis; lector
  design r1-r8 folded; lector's final verdict **approved**, and **accepted by
  Johno 2026-08-21**; r8's three amendments applied
  (r8: the capture buffer DRAINS, so no typed history lingers in the DOM) — a correctness defect in rev
  0's frame coalescing, a wrong security claim about mTLS, and r2's internal
  contradictions. **Rev 9 (2026-08-22) reverses one decision on Johno's
  instruction:** §2.8's refusal of password auth becomes "permitted, documented
  as the weakest option, with the throttle and allowlist stated as
  requirements". See Review history. Lands on `tui-web`.)
- **Date:** 2026-08-21
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none — purely additive. `tui.Backend`, `Component`, `Surface`,
  `Cell` and every widget are unchanged.
- **Related:** ADR-0001 §2.4 #5 and ADR-0002 (the Backend seam and capability
  model this implements a second time), ADR-0003 (cell buffer, diff, the
  one-write rule), ADR-0006 (style tokens → CSS), the `golib` convention's
  tightened dependency rule (2026-08-21), and the sibling `golib/auth` ADR-0001
  (this backend consumes its `Policy`, it does not roll its own).

## 1. Context

### 1.1 What is being asked for

A CLI tool that already has a golib TUI should be reachable from a browser, so a
user can operate it remotely without the tool growing a web UI. Johno's framing:
*"the goal this tui.web engine is just to allow users to remotely connect to a
CLI server where TUI is already available without extra costs. It won't be a
fancy webapp but more likely a way to access the CLI tool which already has a
TUI."*

### 1.2 Why this is cheap — the seam already exists

Verified in the code, 2026-08-21:

- **`tui.Backend` is the driver seam and it is cell-shaped, not ANSI-shaped:**
  `Start`/`Stop`, `Size`, `Flush([]CellUpdate)`, latched cursor calls,
  `Capabilities`, `Events() <-chan Event`, `Err`.
- **`WithBackend` is REQUIRED.** `NewApp` panics without it, and `tui/options.go`
  records why: the core cannot construct a terminal driver because `tui/term`
  imports `tui`, never the reverse.
- **Core `tui` imports only** `golib/logger`, `tui/internal/grapheme`,
  `tui/style`. No `os`, no `syscall`, no `x/term`.
- **A non-terminal Backend already exists and works:** `tui.NewTestBackend`.
- `Cell{Content string /* one grapheme cluster */, Width uint8, Attrs
  CellAttrs{FG, BG CellColor; Mask AttrMask}}` maps to a styled span almost
  mechanically.

So an existing TUI renders in a browser with **zero changes** to widgets,
layout, the runtime, or the application.

### 1.3 The second, strategic reason (why this ADR exists at all)

Johno: *"the reason why I chose (A) is the feasibility test and how it will fan
out since in future, we may be able to implement the backend for other ways too,
such as IoT or in the mobile environments or other handheld platforms."*

`tui/web` would be the **first backend that is neither a terminal nor a test
double** — hence the first honest test of whether `tui.Backend` generalizes to
arbitrary render targets. Whatever friction it hits is the friction an embedded
display or a handheld target will hit. **That evidence is a deliverable of this
ADR** (§2.10), not a side effect.

## 2. Decision

### 2.1 Approach (A): the backend owns the grid, the server renders HTML

`tui/web` implements `tui.Backend`. It keeps a server-side cell grid, applies
each `Flush` diff to it, and emits a character grid as HTML. The browser is a
dumb surface: it displays HTML and reports input.

Rejected alternatives are in §5; the two that matter are **(B)** semantic
per-widget HTML (rejected outright) and **(C)** PTY + xterm.js (kept documented
as the fallback, and as the right answer to a *different* question).

### 2.2 Backend contract — implemented to the letter

| Method | Web implementation |
| --- | --- |
| `Start(ctx)` | begin the session; no device to acquire; resolve the capability profile from the client hello; start the input decoder |
| `Stop()` | idempotent (`sync.Once`), closes `Events()`, tears the session down; safe from deferred panic-recovery |
| `Size()` | last client-reported cols×rows (§2.6) |
| `Flush(diff)` | apply to the server grid, publish one frame; **must not block** (§2.4) |
| `ShowCursor`/`HideCursor`/`SetCursor`/`SetCursorShape` | latched, emitted with the next frame — never immediate |
| `Capabilities()` | constant after `Start`; honest (§2.3) |
| `Events()` | single, ordered, **un-coalesced** channel; closed by `Stop`; all coalescing stays the App's intake concern |
| `Err()` | the transport error that ended the session; nil after a clean `Stop` |

### 2.3 Capability honesty

A browser is not a terminal, and the profile must say so rather than flatter
itself: `ColorProfile: ProfileTrueColor`; `SyncOutput: true` (the backend owns
frame commit, so synchronized output is trivially true); `BracketedPaste: true`;
`Mouse: TriYes` only once a client actually reports pointer support;
`InBandResize: true`; **`KittyKeyboard: false`** (no browser analogue — an
optimistic request is never reported as support, ADR-0002 §2.2);
`UnicodeCore` **only** if the client confirms font agreement (§2.6);
`DarkBackground` from the client's `prefers-color-scheme`.

### 2.4 `Flush` must never block on network I/O

The App loop calls `Flush` once per frame and ADR-0003's one-write rule assumes
it is fast. A slow or vanished browser must not stall the UI. Therefore:

- the backend holds **at most one pending frame**, and that frame is
  **CUMULATIVE**: it is the **diff of the current server grid against the last
  baseline the client has ACKNOWLEDGED** — not merely the unsent rows. A row
  that was transmitted but not yet acknowledged must stay in the aggregate,
  because an unacknowledged send may never have landed;
- a frame reaches the client **atomically** — never a half-painted screen;
- every frame carries a **monotonically increasing revision**, and the client
  acknowledges; a new connection (or any gap) gets a **full resync snapshot**
  rather than a diff;
- a coalesced-away *publication* is logged (§2.11): silently swallowing one is a
  debugging trap.

**Why cumulative, and why rev 0 was wrong.** Rev 0 said a newer frame *replaces*
the older (latest-wins). That is a correctness bug, not an optimisation, because
frames carry only **dirty rows**: if the frame containing row A is dropped and
the next frame changes only row B, a replacement carries B alone and **row A
never reaches the client — permanently.** The client diverges from the server
grid with no mechanism to notice. Latest-wins is only safe when each frame is
self-contained; with row-level diffs the pending frame must accumulate. With a
cumulative aggregate, a queue is unnecessary: one slot is sufficient and
bounded.

### 2.5 Transport, and encryption by default

Built on golib's own `server/http` and `server/ws`.

**Transport: WebSocket (DECIDED, r1).** `server/ws` already carries
`github.com/coder/websocket` in `go.mod`, so this adds **no new module** to the
repo — only to this package's import graph — and ordered full-duplex framing
avoids an HTTP request per keystroke. SSE + POST stays recorded as the
stdlib-only alternative in §5.

**Encryption is on by default** (Johno, 2026-08-21: *"we should also have
encryption by default"*). `wss://` is not a separate crypto layer — it is the WS
handshake over TLS, the same TLS 1.3 as HTTPS. Required:

- A **non-loopback bind without TLS is a startup error**, never a warning. No
  silent plaintext, ever.
- Plaintext is permitted **only** for a loopback bind — including inside the
  documented authenticated SSH forward, where the tunnel is the boundary.
  **No auto-generated self-signed certificate (decided, r1; §7.2):** browser UX for an
  ephemeral localhost certificate is poor, and a certificate is not a substitute
  for authentication, which is mandatory anyway (§2.8).
- No default certificate is ever shipped. Operator-supplied cert/key.
- The **documented primary deployment is an SSH local-forward**
  (`ssh -L 8080:127.0.0.1:8080 host`) against a loopback bind: no open port, and
  the SSH hop is already authenticated and encrypted. This matches the premise —
  the user can already reach the box.

### 2.6 Sizing, font metrics, and the wide-grapheme hazard

**The client measures; the server never guesses.** The browser measures the
monospace cell advance and reports cols×rows; resize is reported in band.

The hazard is real and is the classic xterm.js bug class: a grapheme golib
counts as `Width: 2` (CJK, emoji) must occupy exactly two columns in the chosen
font, or the row drifts. Mitigations, all required:

- **Bound a mismatch to its own box, in measured pixels.** Grid tracks are sized
  from the **client's measured cell advance in px** — not `ch`, which is itself
  font-relative and therefore begs the question. A `Width: 2` head span occupies
  exactly **two tracks**; a `Width: 0` continuation emits **no box at all**.
  Each cell box gets explicit overflow clipping and paint containment, so a glyph
  wider than its box is **visually clipped, never desynchronizing**. Box
  containment — not the probe — is the actual safety guarantee.
- **Honor `Cell.Width` exactly.**
- **Pin the font stack** in the served CSS and document it.
- Describe `UnicodeCore` **conservatively**: a finite probe string cannot prove
  every Unicode glyph agrees with the server's width calculation, so the probe
  informs the capability report but is never presented as a proof.

### 2.7 Rendering

**Browser hardening is part of the contract:** a Content-Security-Policy
suitable for the served client, `frame-ancestors 'none'`, `Cache-Control:
no-store`, restrictive content types, and **explicitly configured `Host` and
`Origin` expectations** (never inferred from the request).

`html/template` over the server-side grid, emitting only **dirty rows**.
`CellAttrs` → inline CSS; `AttrMask` → weight, italic, underline (undercurl only
where claimed), reverse, strikethrough; `style.Theme` tokens → CSS custom
properties (ADR-0006). Cell content is HTML-escaped. The cursor is a positioned
element driven by the latched state (§2.2).

### 2.8 Sessions, authentication, and hijacking defenses

**Sessions are the real work, not rendering.** One browser session = one
`tui.App` + one `web.Backend` + its goroutines. Required: explicit
create/attach/detach, idle eviction with a configurable timeout, a hard cap on
concurrent sessions, and guaranteed teardown (`Stop`, task drain, no goroutine
or memory leak) on disconnect, eviction and process shutdown.

**Reconnect (r2 correction): the invariant is FRESH AUTHENTICATION, not a fresh
ticket.** Rev 1 said every attach needs a new ticket, which contradicts the
policy above — mTLS and the signed challenge are identity-bearing mechanisms in
their own right, and demanding a ticket would force a needless round-trip
through the CLI for a client that can simply re-authenticate.

The `App` MAY survive a short detach window so a flaky network does not destroy
work, and **every attach re-runs the completed policy from scratch**:

- the **ticket branch** consumes a *new* single-use ticket (the original is never
  reusable);
- the **mTLS / challenge branches** authenticate directly — no ticket needed —
  or, where a single code path is preferred, the server mints an **internal
  one-use attach grant** after a successful authentication and consumes it
  immediately;
- no credential of any kind is resurrected, and every attach emits an audit
  event.

The **SSH-side minter** is specified end to end: which SSH/OS subject it maps to,
the expiry, the session and origin binding, and its audit event.

**Authentication is mandatory. There is no unauthenticated mode, not even on
loopback** (Johno, 2026-08-21: *"ideally we only want users to connect to WebTUI
via ssh keys or other secured manners only"*):

**The policy, stated once and shared with `golib/auth` ADR-0001 (r1
cross-ADR fix):**

```go
// auth ADR-0001's real graph: factors enter through Leaf, NewPolicy validates
// the finished tree.
p, err := auth.NewPolicy(auth.Any(
	auth.Leaf(ticket), auth.Leaf(mtls), auth.Leaf(sshChallenge)))

// optionally constrained by context:
p, err = auth.NewPolicy(auth.All(
	auth.Leaf(ipallow),
	auth.Any(auth.Leaf(ticket), auth.Leaf(mtls), auth.Leaf(sshChallenge))))
```

- The **ticket minted over the user's existing SSH session is the default**
  mechanism; a signed `ssh-keygen -Y sign` challenge is optional for users who
  cannot run the CLI themselves. mTLS is the browser-native alternative.
- **Password auth is PERMITTED, NOT RECOMMENDED, and is a TICKET MINTER rather
  than an attach mechanism (rev 9 permitted it; rev 11 reshaped it; this is the
  normative statement).** A password factor belongs on `Config.LoginPolicy`. It
  MUST NOT appear on `Config.Policy`, where it cannot be satisfied: the attach
  protocol carries no password fields at all, so nothing can present one.

  Why it is the weakest option, specific to *this* consumer rather than general
  grumbling about passwords:

  - **What is behind the credential is a shell.** A network-reachable port whose
    only guard is a reusable secret is the classic mass-exploitation target;
    every other accepted mechanism fails closed against an attacker holding only
    a password list.
  - **No phishing resistance.** A lookalike page harvests a password and it keeps
    working. A ticket is **single-use and 30-second**, so a harvested one is worth
    at most one attach inside half a minute; mTLS and the SSHSIG challenge are
    bound to a key the page cannot exfiltrate at all.

    > **Correction (lector r5).** Earlier revisions called the ticket
    > "origin-bound". It is not. `auth/token`'s `Record` holds only
    > Subject/IssuedAt/ExpiresAt/SingleUse, `Issue` takes subject/TTL/singleUse,
    > and `Verify` consumes by token hash without consulting
    > `Request.Metadata` — so a ticket minted under one allowed origin is
    > redeemable under another, and a non-browser can present any `Origin` it
    > likes, as this ADR says elsewhere. The accurate property is **composed**: a
    > single-use 30-second bearer ticket, accepted only behind a
    > separately-enforced `Origin` allowlist. That is a real guarantee and it is
    > the one to state; "origin-bound" claimed the binding was in the credential,
    > which would survive a broken allowlist, and it would not.
    >
    > Issuance-origin binding inside `auth/token` would be a genuine improvement
    > and is recorded as a follow-up rather than done here: this package must not
    > keep its own side-table of credential validity, because §2.8 already says
    > `auth/token` owns validity and consumption and the two must not both think
    > they do.
  - **No replay resistance.** A password is long-lived and reusable by
    construction; the other three mechanisms are each spent, bound, or
    key-backed.

  **Therefore, when password is enabled, these are REQUIREMENTS and not advice:**

  1. It goes on `Config.LoginPolicy`. `POST /login` authenticates it and mints a
     **single-use, 30-second** ticket; the WebSocket then attaches with that
     ticket. The invariant this buys is narrow and worth stating precisely: **a
     reusable secret is not among the credentials the attach path will accept.**
     (It is NOT that every attach presents a ticket — mTLS and the SSH challenge
     attach on their own.)
  2. Wrap it in `auth.Throttle` with a `Tracker` (ADR-0001 §2.6b). Per-subject
     and per-source-address backoff is the only thing standing between a reusable
     secret and an online guessing attack, and it belongs on the login policy
     where the guessing happens rather than entangled with the attach policy.
  3. Constrain it: `All(Leaf(ipallow), Leaf(throttledPassword))`. An IP allowlist
     cannot authenticate — ADR-0001 §2.2.2 enforces that structurally — but it
     can narrow who is allowed to try. `web.PasswordPolicyExample` REQUIRES a
     contextual constraint and refuses an identity factor in that position.
  4. Keep the default Argon2id hashing (ADR-0001 §2.4). PBKDF2 is for an explicit
     FIPS requirement, not for saving CPU on a login path.

  The **attach** policy is composed with `web.RecommendedPolicy` from the
  mechanisms it can actually receive: tickets, mTLS chains, SSH signatures.

  `tui/web` does not *refuse* a weak policy, because the policy is the caller's to
  compose and a package that second-guessed it would be lying about where the
  decision lives. What it does refuse is to make the weak shape *reachable* where
  it does not belong: password on the attach path is unrepresentable, not merely
  discouraged.

  > **Normative correction (lector r4).** Rev 11's change was recorded only in the
  > review history, leaving this section still saying password may appear in the
  > attach policy and recommending
  > `Any(ticket, mtls, sshChallenge, throttledPassword)` on `Config.Policy`. A
  > review-history note does not supersede a standing instruction, and an ADR
  > whose normative section contradicts its own code is worse than one that is
  > merely out of date — an implementer following it would have written the thing
  > the code now forbids.

- An IP allowlist is **optional and never identity-bearing** — it may only
  constrain an identity-bearing proof, never satisfy the policy alone. ADR-0001
  §2.2 enforces that structurally rather than by documentation.
- **`auth/token` owns credential validity and atomic consumption; this package
  owns App/session attach, detach and eviction.** The two must not both think
  they own expiry.
- This backend consumes `golib/auth`'s **`Policy`** (ADR-0001 §2.1 — the
  validated tree; the `Authenticator` interface of earlier revisions no longer
  exists). **No `tui.App` is created and no input is accepted until
  `Policy.Authenticate` succeeds.** If that package is not ready, `tui/web` takes
  the `Policy` interface from day one so the implementation drops in without
  touching transport or session code.
- A browser cannot read `~/.ssh`, so "SSH key" needs a mechanism; §7.3 records
  the decision (SSH-channel-minted single-use ticket by default).

**WebSocket hijacking defenses (mandatory).** The WS handshake is an HTTP request
that carries cookies but is **not** subject to the Same-Origin Policy, and
**CORS does not apply to it** — so a cookie-authenticated socket is directly
open to **CSWSH**: an attacker page opens a socket in the victim's browser and
the browser attaches the credential, yielding a keyboard on the CLI. Therefore:

- **validate `Origin` against an allowlist, deny by default** (necessary but not
  sufficient — `Origin` is forgeable by a non-browser client);
- **never authenticate the socket with an ambient cookie**;
- **Ticket transport (r1, precise):** the ticket goes in the **URL fragment**,
  which browsers never send in the HTTP request, and client JavaScript submits
  it as the **first WebSocket application message**. Not the query string (it
  leaks into access logs, `Referer`, history and proxies) and **not
  `Sec-WebSocket-Protocol`**, which is a handshake header and not a place for
  secret material.
- **Scrub the fragment immediately.** A fragment avoids HTTP, `Referer` and
  server-log exposure, but it still sits in the address bar, the current history
  entry, and any URL the user copies. The client reads it and calls
  `history.replaceState` to remove it **before opening the socket**.
- **Admission is gated on `Policy.Authenticate`, and the atomic consume applies
  to the TICKET BRANCH ONLY.** Rev 2's blanket "consume must succeed" sentence
  contradicted direct mTLS and challenge authentication, which present no ticket.
  So: the ticket branch requires `auth/token`'s atomic `Consume`; the mTLS and
  challenge branches authenticate directly. In every case nothing is created or
  accepted before the policy succeeds, failure closes the connection, and the
  ticket is never echoed or logged.
- **`Origin` validation stays mandatory for EVERY browser mechanism, mTLS
  included** — see the correction below.
- connection caps, idle timeouts, per-connection memory limits.

> **CORRECTION (r1): mTLS does NOT eliminate CSWSH.** Rev 0 claimed it "kills
> the CSWSH class outright". That is wrong. A browser may **automatically
> re-present a previously selected client certificate** for the target origin,
> which makes mTLS an *ambient* credential in exactly the way a cookie is: it
> authenticates the TLS peer, but it does not prove the initiating page is
> trusted. mTLS remains valuable — it is a strong, phishing-resistant
> *authentication* mechanism — but deny-by-default `Origin` validation is
> mandatory alongside it, not instead of it.
> (RFC 6455 §10.2; W3C client-certificate-selection notes; OWASP WebSocket
> Security Cheat Sheet.)

### 2.9 The input contract — the table, against the real event types

r3 caught rev 2 inventing event shapes. These are `tui/events.go`'s actual
types: `KeyEvent{Kind KeyKind, Code rune, Base, Shifted rune, Mods Mods, Text
string}` (`Code` is a codepoint or a `tui.Key*` constant), `MouseEvent{Kind
MouseKind, Button MouseButton, X, Y int, Mods Mods}`, `PasteEvent{Text}`,
`FocusEvent{Gained, Terminal bool}`, `ResizeEvent{W, H int}`.

**Resolution order is normative — the first matching rule wins.** Without it the
rows overlap and, worse, layouts where **AltGraph reports as Ctrl+Alt** would
emit a command instead of the character the user typed:

1. **Reserved browser shortcuts** → not forwarded, no `preventDefault`.
2. **`isComposing`, or AltGraph** → never a chord; the character arrives through
   the `input` text path. **The only automatic AltGraph signal is
   `KeyboardEvent.getModifierState("AltGraph")`.** Ctrl+Alt is **not** inferred
   as AltGraph by default — that would silently swallow every legitimate Ctrl+Alt
   chord. Where a browser fails to report AltGraph, an explicit opt-in
   (`TreatCtrlAltAsAltGraph`) enables the heuristic and the README states the
   trade-off.
3. **Named keys** (`KeyboardEvent.key` in the named set).
4. **Text** — emitted as a **host-value delta**, either at `compositionend` for
   a composition or on an ordinary `input` (§ state machine).
5. **Modified keys** (Ctrl/Alt/Meta + printable).
6. Anything else → dropped.

| Browser event | Emitted | preventDefault |
| --- | --- | --- |
| reserved shortcuts: Ctrl/Cmd+`T`/`N`/`W`/`L`/`R`, Ctrl+Tab, `F5`, `F11`, `F12`, Cmd+`Q` | *(nothing — the browser keeps them; README says so)* | **no** |
| `keydown` with `isComposing`, or `getModifierState("AltGraph")` (or Ctrl+Alt **only** when `TreatCtrlAltAsAltGraph` is enabled) | *(nothing — the character arrives via `input`)* | no |
| `keydown`, `key` ∈ {Enter, Tab, Escape, Backspace, Delete, Insert, Arrow*, Home, End, PageUp, PageDown, F1–F12} | `KeyEvent{Kind: KeyPress, Code: tui.KeyEnter \| KeyTab \| KeyEscape \| … \| KeyF12, Mods}` | **yes** |
| same, with `repeat: true` | `KeyEvent{Kind: KeyRepeat, …}` | **yes** |
| `keyup` for a forwarded key | *(nothing — `KeyRelease` is kitty-only and is never synthesized here)* | no |
| `input` with `isComposing: true` (**composing**) | **host update only** — emit nothing, do not drain | no |
| `compositionstart` / `compositionupdate` | **state only** — emit nothing, do not drain | no |
| `compositionend` | the control is already updated by spec: **drain** the capture element — emit its value once per rune as `KeyEvent{Kind: KeyPress, Code: r, Text: string(r), Base: 0, Shifted: 0, Mods: 0}`, then synchronously clear it | no |
| `input` while **idle** (ordinary typing, or a late composition notification) | **drain**: if the capture element is non-empty, emit its value per rune and clear it; if empty — which is what a late notification for already-committed work sees — emit nothing | no |
| `keydown` with Ctrl/Alt/Meta + printable, where `key` is **exactly one Unicode scalar** (and **not** rule 2) | `KeyEvent{Kind: KeyPress, Code: <that scalar, lower-cased only when the lower-case form is also a single scalar>, Base: 0, Shifted: 0, Mods: ModCtrl \| ModAlt \| ModSuper \| ModShift}` — `Code` comes from `key`, never from a base-layout codepoint the DOM does not expose | **yes** |
| `keydown` + modifiers where `key` is `Dead`, `Unidentified`, or **not a single scalar** (multi-scalar, or a lower-casing that expands) | **dropped** — `KeyEvent.Code` holds one rune, so there is nothing faithful to put in it; any resulting character still reaches the app through the text path | no |
| `paste` | `PasteEvent{Text}` with CR/CRLF normalized to `\n` | **yes** |
| `mousedown` / `mouseup` / `mousemove` | `MouseEvent{Kind: MousePress \| MouseRelease \| MouseMotion, Button, X, Y (CELL coords per §2.6), Mods}` | **yes** in-grid |
| `wheel` | `MouseEvent{Kind: MouseWheel, Button: WheelUp \| WheelDown \| WheelLeft \| WheelRight, X, Y, Mods}` — quantized to discrete steps; there is no delta field | **yes** in-grid |
| window focus / blur | `FocusEvent{Gained: true\|false, Terminal: true}` — this is terminal-level focus, not component focus | no |
| resize or a measured-metrics change | `ResizeEvent{W: cols, H: rows}` | no |
| anything else | **dropped explicitly** — never a phantom key | no |

**`Base`/`Shifted` are always ZERO (r4 correction).** Rev 3 claimed the browser
could supply them for kitty-style layout-independent matching. It cannot:
`KeyboardEvent.code` is a **physical-key** identifier (`KeyA` is a position, not
a base rune), and the DOM exposes no base-layout or shifted codepoint. In
`tui/term` these fields are populated **only** on the kitty CSI
`unicode-key-code:shifted:base` path (`decoder.go`'s `kittyKey`); there is no
browser equivalent. They stay `0` — exactly the shape `tui/term` produces for a
non-kitty terminal — unless a future revision specifies a *separately validated*
layout mapping, which this ADR does not.

**Emission parity is per RUNE, not per grapheme cluster (r4 correction).** Rev 3
said one event per cluster "as the terminal decoder does"; `decoder.go`'s
`actPrint` emits `KeyEvent{Code: a.r, Text: string(a.r)}` — **one event per
rune**. The web backend matches that scalar sequence, so a component cannot tell
the two backends apart. A multi-codepoint emoji therefore arrives as several
events, exactly as over a terminal.

**Modifier mapping.** `ctrlKey`→`ModCtrl`, `shiftKey`→`ModShift`,
`altKey`→`ModAlt`, and **`metaKey`→`ModSuper`** — *not* `ModMeta`. `tui.Mods` is
in kitty order, where *super* is the Cmd/Windows key and *meta* is the historical
Meta (usually Alt); the obvious-looking `metaKey`→`ModMeta` mapping would
silently break Cmd chords on macOS.

**The text machine — a host-state delta, with no time-based boundary (r7).**
Three revisions converged on this. r4 emitted twice; r5 guarded by position; r6
guarded by content, which r6 review showed is not an event identity; and rev 6's
replacement leaned on *"composition-associated events are dispatched within one
task"*, which **is not a standards guarantee** — UI Events specifies synchronous
dispatch and relative ordering, and HTML assigns user input to the
user-interaction task source without making task coalescing an identity
contract. A settle timeout merely swaps one ambiguous boundary for another.

**The boundary is the host's own state, and the buffer is TRANSIENT (r8
amendment 1).** All text is captured in a dedicated **capture element** — a
focusable off-screen `textarea` — which is **drained on every committed
emission**: emit its value, then synchronously set it to empty and restore the
caret. It is a transient buffer, never a log.

Draining is not tidiness; retaining the typed history would mean **every
keystroke the user ever sent — including passwords typed into the TUI — sitting
in the DOM** of a page reachable over the network, and it would also leave
"baseline → current" ambiguous for any replacement occurring *before* the
baseline. With a drain, the baseline is always empty and the delta is simply the
element's current value, so no general string diff is ever needed.

The element is declared `autocomplete="off" autocapitalize="none"
spellcheck="false"`, carries **no `name` and no enclosing form**, and JS
references to captured strings are dropped promptly after emission. **No claim is
made that the text is erased from memory** — a JS engine offers no such
guarantee; the claim is only that it is no longer in the DOM or retained by our
code.

The machine, then:

| State | Event | Action |
| --- | --- | --- |
| any | `compositionstart` | mark composing; do not drain |
| composing | `compositionupdate` | emit nothing; do not drain |
| composing | `input` (`isComposing: true`) | emit nothing; do not drain |
| composing | `compositionend` | UI Events dispatches this **after the control is updated**, so: **emit the element's value once** (per rune), then **synchronously clear it and restore the caret**; → idle |
| idle | `input` | **emit the element's value if non-empty**, then clear it; empty ⇒ emit nothing |
| any | cancellation (the host reverted, so the element is empty) | nothing to emit, with no special case needed |

Why this is sound where the previous three attempts were not:

- **A late notification for work already committed finds the buffer empty.**
  Because `compositionend` drained synchronously, an `input` arriving afterwards —
  same task, later task, or never — sees an empty element and emits nothing.
  **Ordering stops mattering**, which is exactly what the specs decline to
  promise.
- **A separately typed identical value repopulates the buffer**, so it survives.
  The r6 failure (composed `x` then typed `x`) cannot recur: identity comes from
  *state transitions*, never from comparing content.
- **Cancellation needs no rule of its own.** A reverted host leaves the element
  empty, so "emit only what is there" already covers it — and empty
  `CompositionEvent.data` is irrelevant, since `data` is never consulted.
- **No ambiguous diffing exists to get wrong later.** Because the buffer is
  always drained, "the delta" is just "the current value" — so nobody
  implementing this can reach for a general string diff. Replacement and
  autocorrect behaviors are disabled by the element's attributes; if a platform
  applies them anyway, the ADR requires the behavior be **explicitly normalized
  and tested**, never diffed heuristically.

**The one assumption, stated as an assumption:** that a supported engine updates
the control *before* dispatching `compositionend`, per UI Events. The required
browser matrix exists to **verify** that, not to define it; if an engine violates
it, the ADR must name the measured fallback rather than guessing at one now.

*Paste* stays guard-free: `paste` is `preventDefault`ed, so the clipboard text
never enters the capture element, no delta appears, and `PasteEvent` is emitted
directly. A cancelled paste therefore cannot disturb the baseline or eat a later
keystroke.

**These behaviors are browser-specific, so the suite runs against real Chromium,
Firefox and WebKit** — synthetic dispatch is exactly what would hide them.
Skipping locally with no browser present is fine, but **a suite that skips
everywhere cannot satisfy an acceptance criterion**: the matrix is a required
gate in CI (or another named release gate), and a release with the matrix unrun
is not a release.

**Resource limits — concrete defaults, all configurable:**

| Limit | Default | On breach |
| --- | --- | --- |
| max WebSocket message | 64 KiB | close (1009 Message Too Big) |
| sustained input rate | 500 events/s | token bucket; excess backpressures |
| burst allowance | 2 000 events | token-bucket **allowance only** |
| `Events()` queue capacity | 1 024 | backpressure at capacity |
| overload before close | 2 s continuously at capacity | close (1008 Policy Violation) |

The burst allowance is a token-bucket credit, **not** a promise that 2 000 events
are absorbed: the queue holds 1 024, and anything beyond it applies backpressure
and may reach the two-second close rule. Because `Events()` is ordered and
un-coalesced (ADR-0002), the backend never grows to absorb abuse.

### 2.12 The login handoff seam (rev 19)

**Problem.** A consumer doing single sign-on must carry per-login state — an
authenticated upstream session — from the login route to the `App` created for
that login. `auth.Factor.Verify` is the only place holding the credential, so the
allocation happens there; but `ServeLogin` runs the policy **before** the
WebSocket hello says whether the attach will `Create` or `Attach`, and
`AppFactory` runs only on `Create`. So a login used to REATTACH allocates
upstream state that no factory ever claims, and there is no path on which to
release it.

Found by autodb ADR-0061, the first consumer with more than one user
(lector, 2026-08-22). It is the **third** entry in §2.10's seam report and the
first that could not have been found from inside this package: a factory blind to
identity is fine for a single-user demo and impossible for a multi-user gateway.

**The fix is not "keep side effects out of Verify."** That was my first diagnosis
and it is unachievable — the credential exists only there. The fix is to make the
allocation **recoverable**: give every outcome a named release path, and give the
caller a key that binds one login to one session.

#### 2.12.1 The handoff key is DERIVED, so this package stores nothing

```go
// HandoffID is an opaque, single-claim correlation between one successful login
// and the session that login authorizes.
func HandoffID(ticket string) string    // base64(sha256("webtui-handoff\x00" + ticket))
```

Derived from the minted ticket rather than generated and stored:

- **no state here.** A map from ticket to handoff would mean this package holding
  credential-derived keys at rest, and a second thing to expire correctly.
- **only the ticket holder can compute it.** Knowing an ID reveals nothing about
  the ticket, and anyone able to produce one already had the credential.
- **domain-separated**, so the value can never collide with the token store's own
  `sha256(ticket)` index. Two hashes of one secret for two purposes must not be
  the same hash.

#### 2.12.2 Four paths, every one named

```go
// SessionInfo is what a factory needs to build an App for a specific user.
type SessionInfo struct {
    Identity *auth.Identity // authenticated; never nil
    Handoff  string         // HandoffID of the login that created this session;
                            // "" when the attach carried no login (mTLS, SSH)
}

type AppFactory func(*Backend, *SessionInfo) Runner   // was func(*Backend) Runner

// OnLogin runs after LoginPolicy succeeds and the ticket is minted, with that
// ticket's HandoffID. This is where a caller MOVES per-login state into its own
// park, keyed by the handoff.
func OnLogin(fn func(handoff string, id *auth.Identity) error) HandlerOption

// OnHandoffUnused runs whenever a handoff will NEVER be claimed by a factory.
// A caller releases the parked state here. Called at most once per handoff.
func OnHandoffUnused(fn func(handoff string, reason HandoffReason)) HandlerOption
```

| Path | What happens | Hook |
| --- | --- | --- |
| login succeeds | ticket minted, handoff derived | `OnLogin` |
| attach → **Create** | factory receives the handoff and claims it | `AppFactory` |
| attach → **Reattach** | no factory runs; the handoff is dead | `OnHandoffUnused(ReattachedExisting)` |
| attach fails after auth | session limit, invalid hello, transport error | `OnHandoffUnused(AttachFailed)` |
| ticket never presented | client abandoned the login, or the response never arrived | **the caller's TTL sweep** |

The last row is deliberately the caller's. This package cannot know that a client
walked away — it never saw a connection — and inventing a timer here would put a
second expiry policy beside the token store's, which §2.8 already forbids for
credential validity. `OnLogin` MUST therefore park with its own TTL, and the ADR
says so rather than leaving it to be discovered.

**`OnLogin` returning an error fails the login**, before the ticket reaches the
client. That is the rollback for "the caller could not park it", and it is why the
hook returns an error at all: a login whose state could not be recorded must not
appear to have succeeded.

#### 2.12.3 The credential-to-park move is REQUEST-SCOPED, not subject-keyed

A caller's `Verify` allocates upstream state but does not yet know the handoff —
the ticket is minted afterwards. Parking by **subject** in the meantime is wrong:
two concurrent logins by one user cannot be told apart, and lector's r2 review
names that exact failure.

So the login request carries a scoped slot:

```go
// StashFromContext returns the per-request slot for state produced during
// verification. Valid only for the duration of one login request.
func StashFromContext(ctx context.Context) *Stash
```

`Verify` puts the allocated session in the stash; `OnLogin` moves it into the
caller's park under the handoff. One request, one slot, no cross-request key —
so concurrent same-user logins are deterministic. This mirrors
`auth.WithAttemptSink`, which already establishes that per-request plumbing
through the auth path works.

#### 2.12.4 Two budgets, because they bound different resources

A parked handoff and a live session are not the same cost, and counting them
against one number produces the deadlock lector identified: at a full session cap,
a reconnect must log in first, so its park needs a slot that by definition is not
available — yet reattaching consumes no new session.

| Budget | Bounds | Breach |
| --- | --- | --- |
| `MaxSessions` | live sessions, each with an `App` | `ErrSessionLimit` at Create |
| `MaxPendingLogins` (new, default 8) | parked handoffs awaiting an attach | login refused, 503 |

A reconnect at a full session cap therefore works: the login parks against the
pending budget, the attach reattaches, and `OnHandoffUnused` releases it
immediately. A *new* user's login at a full cap also parks, then fails at Create
with `ErrSessionLimit` — and the same hook releases it. Neither path leaks and
neither is deadlocked.

#### 2.12.5 What this does not solve

Stated so a consumer does not assume otherwise:

- **Owner proof across reconnect** is still `Manager.Attach`'s subject check. The
  handoff says nothing about it, and a reattach with a fresh login is authorized
  by the identity, not by the handoff.
- **The upstream's view of the peer.** A gateway's connections come from the
  gateway, so an upstream audit log and any address allowlist see the gateway's
  address, not the browser's. This package cannot fix that; a consumer must
  forward the browser's address explicitly if the upstream needs it, and should
  document that its own audit trail is the one with the browser in it.

#### 2.12.6 The seam ships a HELPER, not just a protocol (rev 20)

A four-path protocol described in prose is a protocol someone implements
three-quarters of, and the path most likely to be missed is **reattach** —
because nothing in a working deployment's happy path exercises it. The leak it
produces is invisible until an upstream runs out of connections or an audit finds
sessions nobody is using.

So `tui/web` ships [`web.SSO`], and it is the **supported** way to consume the
seam. A consumer supplies only what is genuinely theirs — how to allocate, and how
to release — and each obligation is structural rather than documented:

| Obligation | Enforcement |
| --- | --- |
| release on every path | `Release` is **required**; `NewSSO` returns an error without it |
| wire both hooks | `Options()` returns the handler and manager options **together** |
| clean up abandoned logins | the sweep is internal, and a login sweeps before parking |
| the two bounds agree | the park's `Max` **sets** `MaxPendingLogins` |
| one session per login | `Claim` deletes as it hands over |

The raw hooks stay exported for a park that must live elsewhere — a store shared
across replicas — and the README says plainly that reaching for them means taking
on all five obligations.

**Why this is worth an ADR entry rather than being an implementation detail.**
Johno's instruction (2026-08-22) was that a pattern with this much impact on the
engine should be documented *or* offered as a helper "so that it cannot be
missed". Given how much of this session was spent finding controls that were
documented and not enforced, the helper is the honest reading of that: the
difference between the two options is whether a consumer's mistake is possible,
not whether it is described.

### 2.13 Peer binding (rev 19)

**Optional, off by default.** A session may be bound to the peer address that
created it; an attach from a different address is refused and the session
terminated, forcing a fresh login (Johno, 2026-08-22).

```go
func BindPeer(on bool) ManagerOption      // default: off
```

- The address is recorded at `Create` from `Request.Peer` — the transport's own
  view, never a forwarded header. A consumer wanting the browser's address from
  behind a proxy must configure a trusted-proxy set on an `ipallow`-style factor
  and pass the result explicitly; this package will not read `X-Forwarded-For`,
  for the reason ADR-0001 §2.6 gives: an attacker who picks their own address has
  defeated every address-keyed control at once.
- The check runs at **attach**, which is the only moment it can. Within one
  WebSocket the peer cannot change — it is one TCP connection — so there is
  nothing to re-check per message, and a design that claimed to would be theatre.
- On mismatch: refuse, then **terminate the session** rather than leaving it for
  the legitimate owner. If an address changed because a credential was stolen, the
  session is what the thief is reaching for.

#### 2.13.1 What it buys, stated narrowly

It raises the cost of using a stolen ticket or resuming a hijacked session **from
a different host**. It does nothing against an attacker on the same host or behind
the same NAT, and nothing against a compromised browser.

#### 2.13.2 Two caveats a deployer must be told

Both are the kind of thing that turns a control into a false sense of security, so
they belong in the normative text and not a footnote.

1. **Under the documented SSH local-forward, this is a NO-OP.** Every connection
   arrives from `127.0.0.1`, because the forward is the peer. §2.5 makes that
   deployment the primary one, so for most users peer binding will bind to a
   constant and protect nothing. It is worth having for the TLS-on-a-real-address
   deployment and it should not be described as though it helps everywhere.
2. **It will log people out.** A laptop moving from wifi to cellular, a VPN
   reconnect, or CGNAT re-mapping changes the address, and the response is a forced
   re-login — which is exactly the detach/resume window §2.8 exists to protect,
   deliberately given up. That is a legitimate trade and it is the reason this is
   **off by default**: a consumer must choose it knowing which of its own features
   it weakens.

### 2.10 The seam report is a deliverable

The package README and this ADR's review history must record **every place the
`Backend` contract or the `Cell`/`Surface` model assumed a terminal** and made a
non-terminal target awkward — or state plainly that none did. That record is the
reusable output for a future IoT / mobile / handheld backend and the reason (A)
is being built first (§1.3). Anything needing a core change is raised, never
worked around silently.

## 3. What does not change

`tui.Backend`'s definition, `Component`, `Surface`, `Cell`, `CellAttrs`,
`style.*`, every widget, `tui/term`, `tui.TestBackend`, and every existing
application. The change is additive: a new `tui/web` package, its docs, and
optionally one example.

## 4. Non-goals

- **Not semantic HTML** (approach B). No `Table`→`<table>`, no
  `Input`→`<input>`. A real web app should be built with a mature FE framework —
  that is a decision, not a deferral.
- **Not a design system, theme editor, or responsive layout.** The browser is a
  terminal surface here.
- **Not multi-user collaboration.** One viewer per session.
- **Not accessibility-first.** A span grid is poor for screen readers; that is an
  accepted consequence of (A) and is stated in the README rather than papered
  over.

## 5. Alternatives considered

1. **(B) Semantic HTML per widget.** Rejected: it needs a second render path on
   `Component` (every widget implementing both), and it forfeits the cell-grid
   layout guarantees — flow layout is not a character grid. It is a sibling
   framework sharing widget names, not a backend.
2. **(C) PTY host + xterm.js.** Run the existing `tui/term` backend against a
   PTY, stream its bytes over a WebSocket, let xterm.js render. This is what
   every cloud provider's in-browser SSH does (AWS SSM Session Manager, GCP
   SSH-in-browser, Azure Bastion, Cloud Shell, Proxmox; open-source GoTTY /
   ttyd / wetty). Feasible today: `tui/term` already exposes
   `WithTTY(in, out *os.File)` and a PTY master/slave pair is exactly that — a
   real tty, so raw mode, `fdSize` and the probe all still work.
   - **For:** deletes §2.6 and §2.7 entirely (xterm.js has absorbed years of
     wcwidth/grapheme/advance fixes); more faithful (real mouse, bracketed
     paste, even kitty keyboard); and *any* terminal program works, not only
     golib TUIs.
   - **Against:** a vendored JS bundle plus static assets instead of
     dependency-free Go + `html/template`; a PTY dependency; and decisively —
     **it exercises no new Backend implementation at all**, so it proves nothing
     about whether the seam generalizes (§1.3). (A) answers that question; (C)
     sidesteps it.
   - **Kept as:** the documented fallback if (A)'s font-metric or latency work
     proves worse than expected, and the right answer if the goal ever becomes
     "run arbitrary terminal programs in a browser" rather than "render our TUI
     anywhere".
3. **Terminal-in-a-browser via a third-party JS emulator, but keeping the cell
   grid** (server sends cells, xterm.js-like client renders them). Rejected as
   the worst of both: a JS dependency *and* the font-metric problem, since the
   client would still be told column positions.
4. **Rendering to an image server-side** (headless raster, ship PNG frames).
   Rejected: bandwidth, no text selection, no copy, and it needs a font stack on
   the server.

## 6. Files / acceptance

New: `tui/web/**` (backend, session manager, transport, input decoder, HTML
renderer, static assets), `tui/web/doc.go`, `tui/web/README.md`, tests;
optionally `tui/examples/webdemo`.

Acceptance criteria:

1. The demo's component tree — **unchanged component logic** — runs in a browser
   via `tui.NewApp(root, tui.WithBackend(web.New(...)))`: renders, accepts
   keystrokes, repaints, exits cleanly.

   > **AMENDED (r7, awaiting Johno's acceptance).** This criterion said
   > "unchanged **source**". That could not be satisfied as written: the tree
   > lived in `package main`, which Go cannot import, so no second binary could
   > ever have driven it. Implementation therefore extracted it verbatim to
   > `tui/examples/demoapp` — component logic untouched, only the package clause
   > and the constructor's visibility changed — and its own interaction script
   > moved with it and still passes against `tui.TestBackend`.
   >
   > The criterion is amended to match what was done rather than left describing
   > something impossible. Flagging it explicitly because this is an ACCEPTED ADR
   > and narrowing an accepted criterion is Johno's call, not mine: if he would
   > rather the demo were untouched, the honest alternative is to drop this
   > criterion and rely on `webdemo`'s own tree plus the `TestBackend`/`web`
   > parity test.
2. `git diff --name-only main -- tui/` shows changes ONLY under `tui/web/` and
   `tui/examples/`. Mechanically checked:
   `git diff --name-only main -- tui/ | grep -vc '^tui/web/\|^tui/examples/'`
   must print `0`.

   > **CORRECTED (r7).** Earlier text said the diff excluding `tui/web/` alone was
   > empty. It is not — the extraction touches five paths under `tui/examples/`,
   > which this criterion always permitted in its parenthetical but which the
   > stated command did not exclude. The command printed those five paths while
   > the prose claimed it printed nothing, and I had run a DIFFERENT command
   > (excluding both) to verify it. Stating a check whose output contradicts the
   > claim beside it is worse than stating no check.
3. A scripted sequence of `Flush` calls renders to byte-identical HTML across
   runs (golden test), and only dirty rows are transmitted.
4. **Divergence test (the rev-0 defect, pinned).** With the client's reader
   blocked, change row A, let that publication be coalesced away, then change
   only row B, then unblock: the client's final grid must equal the server's
   grid — proving the pending frame accumulated rather than replaced. A second
   case drops the **acknowledgement** rather than the send, proving a
   transmitted-but-unacknowledged row stays in the aggregate. `Flush`
   never blocks, the app loop keeps running, and the coalesced publication is
   logged.
4b. Revisions increase monotonically; a fresh connection and a reconnect after a
   gap both receive a **full resync snapshot**, not a diff.
5. A client resize changes `Size()` and the next frame matches the new grid; a
   mid-frame resize does not tear.
6. A wide grapheme (one CJK, one emoji) occupies exactly two columns in the DOM,
   a `Width: 0` continuation emits no glyph, and a deliberately mismatched font
   does not shift the remainder of the row.
7. Every row of §2.9's table has a test asserting the emitted `tui.Event`
   against the real struct fields; an unmapped browser key is dropped with no
   phantom event. Emission is **per rune**, matching `tui/term`'s `actPrint` —
   proven by comparing the event stream for the same text against the terminal
   decoder — and `Base`/`Shifted` are `0` throughout.
7a. **Text-machine sequences, each asserting the exact insertion count:**
   (i) composition with `input` **before** `compositionend`; (ii) composition
   with `input` **after** it; (iii) `compositionend` with **empty `data`** but a
   non-empty staged value → **commits**; (iv) genuine cancellation after
   non-empty updates (host value back to baseline) → **emits nothing**;
   (v) a composed `x` followed immediately by a **separately typed `x`** →
   **two** insertions, under both orderings; (vi) cancelled paste followed
   immediately by ordinary typing → the typed character **survives**;
   (vii) paste with no trailing `input` → exactly one `PasteEvent`;
   (viii) **a composition-associated `input` delivered in a LATER task**, after
   `compositionend` already committed → **no duplicate**, and it is not mistaken
   for a new identical user action (the drained buffer is empty).
7b. Pinned cases: a non-US layout, a dead-key sequence, a multi-codepoint emoji,
   a `key` of `Dead`/`Unidentified` (dropped, no phantom), and a browser
   reporting no AltGraph state. The Chromium/Firefox/WebKit matrix is a required
   CI gate, not a locally-skippable extra.
7c. Resource limits: an oversized message is rejected, a sustained event flood
   closes the connection instead of growing the queue, and the queue bound holds
   under load.
7d. **The capture buffer never becomes a log:** its value and baseline are
   **empty after every path** — ordinary input, composition commit, cancellation
   and paste; a long typing stream leaves the DOM value size constant rather than
   growing; password-like text is **absent from the element after emission**; and
   replacement/autocorrect is either disabled by the element's attributes or
   explicitly normalized, so no implementer can substitute an ambiguous general
   string diff.
8. Session cap and idle eviction are enforced; after disconnect and after
   eviction, `Stop` has run, goroutines have exited (leak check), and the
   credential no longer attaches.
9. A non-loopback bind without TLS **fails to start**, with a test asserting the
   error; a plaintext loopback bind is permitted per §7.2.
10. Every attach re-runs the completed policy: a replayed ticket is refused, a
    fresh ticket succeeds, and an mTLS client re-attaches **without** a ticket.
    Every branch waits for `Policy.Authenticate`; only the **ticket branch**
    performs an atomic consume, since mTLS and the challenge have nothing to
    consume.
10a. `Origin` validation denies by default and is enforced **even when mTLS is
    in use**; a cross-origin handshake is refused; a ticket cannot be redeemed
    twice (atomic consume). **No `App` is created and no input accepted before
    `Policy.Authenticate` succeeds — on every branch — and, on the ticket branch
    only, before the atomic consume succeeds.**
10b. The ticket never appears in a request URL, an access log, or any server log;
    a test asserts the fragment-plus-first-message flow **and** that the client
    scrubs the fragment via `history.replaceState` before opening the socket.
10c. Responses carry the §2.7 hardening headers (CSP with `frame-ancestors
    'none'`, `Cache-Control: no-store`, restrictive content type), and `Host`/
    `Origin` expectations come from configuration, never inference.
11. `Capabilities()` reports `KittyKeyboard: false`, and no capability is
    `TriYes` without a verifiable basis.
12. The §2.10 seam report exists and is specific: each finding names the method or
    type, what the terminal-shaped assumption cost, and what a future
    non-terminal backend should do — or states explicitly that the seam needed
    nothing, which is itself the result.
12a. **The login handoff (§2.12).** `HandoffID` is deterministic, domain-separated
    from the token store's index, and reveals nothing about the ticket. All four
    paths are exercised: a login parks; a Create claims; a **reattach** fires
    `OnHandoffUnused(ReattachedExisting)`; a Create failing on `ErrSessionLimit`
    fires `OnHandoffUnused(AttachFailed)`. An `OnLogin` returning an error **fails
    the login** and the client receives no ticket. Concurrent logins by ONE subject
    produce distinct handoffs and each factory receives its own — the case a
    subject-keyed park cannot serve. A reconnect at a full `MaxSessions` succeeds,
    proving §2.12.4's two budgets do not deadlock. Verified in both directions:
    with the release hook removed, the reattach path leaks a parked entry.
12b. **Peer binding (§2.13).** With binding on, an attach from a different peer
    address is refused and the session terminated; the same peer reattaches
    normally; and with binding off (the default) an address change is ignored. The
    loopback no-op of §2.13.2 is asserted, so nobody deploys behind an SSH forward
    believing this protects them.
13. `go vet` clean, race-clean, `doc.go` + `README.md` present, every exported
    symbol documented, tests stdlib-only.

## 7. Resolved review questions (r1)

All five open questions were answered in lector's design r1; recorded as
decisions:

1. **Transport: WebSocket** via the existing `server/ws` — no new module, and
   ordered full-duplex input avoids an HTTP request per keystroke (§2.5).
2. **Loopback encryption:** plaintext is acceptable **only** on loopback,
   including inside an authenticated SSH forward; TLS stays mandatory for any
   non-loopback bind. No ephemeral self-signed certificate — poor browser UX and
   no substitute for authentication (§2.5).
3. **SSH-key mechanism:** the **SSH-channel-minted single-use ticket is the
   default**, with the signed challenge optional. The ticket belongs to
   `auth/token` plus a trusted SSH/CLI minter — *not* to `auth/sshkey` (§2.8).
4. **Reconnect:** the `App` may survive a short detach window, but every attach
   requires **fresh authentication** — the completed policy is re-run, with the
   ticket branch consuming a new ticket and mTLS/challenge authenticating
   directly. No credential is ever resurrected (§2.8).
5. **Package scope:** keep `tui/web`; factor a render-target-agnostic layer only
   when a second backend supplies the evidence (§1.3).

## 8. Superseded open questions

1. **Transport:** WebSocket via the existing `server/ws` (no new module in
   `go.mod`, lower keystroke latency) versus SSE + POST (stdlib-only import
   graph, higher latency). Given the tightened dependency rule treats
   "already in `go.mod`" as a lower bar, I lean **WebSocket**. Confidence 70%.
2. **Loopback encryption:** is plaintext acceptable on a loopback bind (the
   tunnel/kernel is the boundary, and TLS-to-localhost means certificate pain),
   or should the backend auto-generate an ephemeral self-signed certificate and
   print its fingerprint? "Encryption by default" argues for the latter.
3. **SSH-key mechanism:** (i) a challenge the user signs locally with
   `ssh-keygen -Y sign` and submits, or (ii) a one-time high-entropy URL minted
   by the CLI over the *existing* SSH session (the Jupyter / `code tunnel`
   shape, where the SSH hop **is** the authentication). (ii) is simpler and
   harder to get wrong; (i) works when the user cannot run the CLI themselves.
   Both? Confidence in (ii) as the default: 75%.
4. **Session reconnect:** resume the same `tui.App` on reconnect (state
   survives a flaky network, but a stolen ticket resumes a live session) or
   always start fresh (safer, loses work)? Leaning resume-with-short-TTL, bound
   to the original client.
5. **Scope check:** is `tui/web` the right package location, or should a
   render-target-agnostic layer be factored out now (`tui/remote`?) in
   anticipation of the IoT/mobile backends §1.3 is aiming at? I lean "not yet" —
   factor it out on the second backend, when the shared shape is evidence rather
   than speculation.

## Review history

- **rev 20 (2026-08-22, Johno — the seam must be unmissable).** Johno's
  instruction: a pattern with this much impact on the engine should come with
  documentation or a helper "so that it cannot be missed". §2.12.6 records the
  choice and why it went to a helper rather than prose — after a session spent
  finding controls that were documented and not enforced, the distinction that
  matters is whether a consumer's mistake is *possible*, not whether it is
  *described*.

  `web.SSO` owns all four paths and the sweep. `Release` is required so the
  leaking state cannot be constructed; `Options()` returns both hooks together so
  the login side cannot be wired without the release side; the park's capacity
  sets `MaxPendingLogins` so the two bounds cannot drift; and `Claim` removes as it
  hands over. A runnable `Example_singleSignOn` carries the wiring, since a godoc
  example is where a consumer actually looks.

- **rev 19 (2026-08-22, jarvis — the login handoff seam and peer binding, from
  autodb ADR-0061).** autodb's web gateway needed single sign-on and could not have
  it. §2.12 records the finding; the README's seam report carries it as finding 0.
  Two things about how it was found are worth keeping.

  **It came from the first consumer that did not look like the demo.** The seam
  report had two entries, both found by writing this package. This one was
  invisible from inside it: a factory that cannot see the identity is perfectly
  adequate for `webdemo` and impossible for a multi-user gateway.

  **My first fix was wrong in a specific way.** I proposed adding `*auth.Identity`
  to `AppFactory` and called autodb unblocked. Lector showed that is necessary and
  not sufficient — the login route allocates upstream state before the hello
  reveals Create-versus-Reattach, the factory runs only on Create, so a login used
  to reattach allocates state nothing claims, and at a full cap the reservation
  arithmetic deadlocks. It also told me not to tag v0.4.0 with only that change,
  which I would otherwise have done.

  Each part of the resulting design was forced by a specific objection: the handoff
  is **derived from the ticket**, so this package stores nothing and only the
  ticket holder can compute it; the credential-to-park move is **request-scoped**
  rather than subject-keyed, so concurrent logins by one user are deterministic;
  and there are **two budgets**, because counting parked handoffs against
  `MaxSessions` is what made a reconnect at full capacity impossible.

  §2.13 adds optional peer binding, with both caveats in the normative text: it is
  a **no-op under the SSH forward** this ADR recommends as the primary deployment,
  and it **trades away the detach window** by logging out anyone whose address
  changes. Off by default for those reasons.

  Breaking change to `AppFactory`, taken deliberately: golib is pre-promotion with
  a single user (Johno, 2026-08-22), `tui/web` is one release old, and its only
  in-tree consumer is `webdemo`. Lands as **v0.4.0**.

- **v0.3.8 release decisions (2026-08-22, Johno).** Two open questions from the
  r7 fold, both answered:

  1. **Criterion 1's amendment is ACCEPTED.** The criterion now reads "unchanged
     component logic" rather than "unchanged source", because the original could
     not be satisfied as written — the tree lived in `package main`, which Go
     cannot import, so no second binary could ever have driven it. The extraction
     to `tui/examples/demoapp` is a pure move.
  2. **The browser gate is WAIVED for v0.3.8**, with Chromium green and Firefox
     and WebKit unrun. Recorded in `tui/web/browsertest/RESULTS.md` with what it
     costs: the text machine is verified on one engine, and Gecko and WebKit are
     the two most likely to diverge on exactly the behaviours it depends on. The
     waiver does not remove the gate — CI still requires all three, so the next
     run reports the missing two for the first time.

  Merged to `main` by rebase and tagged **v0.3.8**, covering `golib/auth`
  (ADR-0001) and `golib/tui/web` (ADR-0009) together.

- **implementation r7 full-PR (2026-08-22, lector — `change_requested`; three
  must-fixes).** The production implementation was approved in substance. The
  blockers were about the release gate and about claims that had drifted.

  1. **The browser matrix did not merely remain unrun — it did not exist.** No
     harness, no CI config, zero check runs. I had been describing an unrun gate
     as though the gate were built, which is a materially different thing from
     what §2.9 requires. Built: `tui/web/browsertest` drives real engines through
     Playwright against a REAL `tui/web` server (a fixture whose component records
     every `tui.Event` and exposes the log, so a browser test can assert the exact
     stream an interaction produced), plus
     `.github/workflows/browser-matrix.yml` with a single required check that
     fails unless every engine passes.

     **Chromium: 16/16 passing.** Firefox and WebKit remain unrun, so **the gate
     is still not satisfied** and `browsertest/RESULTS.md` says so in those words.

     The first real-engine run immediately justified itself by finding two things
     no Go test could: an empty event log encoded as JSON `null` rather than `[]`,
     which turned every "emits nothing" assertion — the majority of the §2.9
     cases — into a TypeError instead of a pass; and `KeyEvent.Kind` being omitted
     when zero, so "absent" means `KeyPress`. Both were harness defects. It found
     no product defect in the text machine, which is evidence and not proof.

  2. **Two accepted claims had drifted from the code.** Criterion 1 said
     "unchanged **source**", which could never have been satisfied as written —
     the tree was in `package main`, which Go cannot import — so it is amended to
     "unchanged component logic", flagged as needing Johno's acceptance because
     narrowing an accepted criterion is his call. And criterion 2's stated command
     excluded only `tui/web/` while claiming an empty result; it prints five
     `tui/examples/` paths, and the command I had actually run to verify excluded
     both. Stating a check whose output contradicts the claim beside it is worse
     than stating no check.

  3. The live PR description still carried the retracted "origin-bound" wording,
     the false criterion-2 command, and work listed as outstanding that was
     already done.

- **implementation r6 amendments + criterion 1 and 12 completed (2026-08-22).**
  Lector approved the r6 delta with amendments: four stale duplicate claims, all
  cases of a retraction I had made in one file and left standing in another. A
  claim retracted in one place is not retracted. Fixed in `serve.go`,
  `login_test.go`, and two places in this history.

  **Criterion 12 — the seam report — is written**, in `tui/web/README.md`. The
  seam held: nothing under `tui/` changed outside the new package and the
  permitted `tui/examples/` paths, mechanically
  verified. Two costs, both in the contract's SILENCE rather than its shape —
  `Err()` conflates a transport failure with a backend failure (which is exactly
  how the r2 detach-window defect happened), and `Flush` says nothing about
  delivery, so `framer.go` exists entirely to fill a gap the contract does not
  mention. Two vocabulary problems: `Capabilities` cannot say "not applicable",
  only "no"; and `CellColor` presumes a palette owner, which forced a
  rendering-only marker outside `CellColorKind`'s range. Two things were actively
  well-shaped: the latched cursor needed no adaptation at all, and `Cell`'s
  EXPLICIT `Width: 0` continuation removed the whole class of row-drift bug.
  Neither cost required a change to `tui`.

  **Criterion 1 is now literal rather than approximate.** The demo's component
  tree was in `package main` and therefore not importable, so "the unchanged demo
  runs in a browser" could only ever have been argued. It moved to
  `tui/examples/demoapp` — a pure move, component logic untouched, only the
  package clause and the constructor's visibility changed — and its interaction
  script moved with it and still passes against `tui.TestBackend`.
  `tui/examples/webdemo` then differs from `tui/examples/demo` by exactly one
  expression: which backend goes to `tui.NewApp`.
  `TestCriterion1_SameTreeOnTheWebBackend` asserts it mechanically: the tree
  renders a non-blank full frame at the client's measured size, repaints on a
  keystroke, follows a resize to 100x30, and exits cleanly through
  `web.Backend`. Criterion 2 still holds — zero files changed under `tui/`
  outside `tui/web/` and `tui/examples/`, which criterion 2 explicitly permits.

  **Still outstanding, and it is a release gate:** the Chromium/Firefox/WebKit
  matrix has not been run. §2.9's text-machine behaviours are browser-specific and
  synthetic dispatch is precisely what would hide a divergence, so no claim about
  the capture-element machine is currently backed by a browser.

- **implementation r5 (2026-08-22, lector — `change_requested`, 1 blocker; folded).**
  Both r4 blockers and the should-fix were accepted. The remaining finding was a
  security overclaim I had repeated since rev 9: calling the login ticket
  **origin-bound**.

  It is not, and lector cited the code: `auth/token`'s `Record` carries only
  Subject/IssuedAt/ExpiresAt/SingleUse; `Issue` takes subject, TTL and a
  single-use flag; `Verify` consumes by token hash and never reads
  `Request.Metadata`. So a ticket minted under one allowed origin is redeemable
  under another, and a non-browser can send whatever `Origin` it likes — a point
  this ADR makes itself, two sections earlier.

  The distinction matters because I used the phrase to claim an advantage over
  passwords. "Origin-bound" locates the binding **in the credential**, which
  would survive a misconfigured or bypassed allowlist. The real property is
  **composed**: a single-use 30-second bearer ticket, accepted only behind a
  separately-enforced `Origin` allowlist. Both halves are real; neither is what I
  said. Corrected in §2.8 and in the rev-9 history entry.

  Worth separating from the SSH challenge, which genuinely *is* bound: ADR-0001
  §2.5 puts session and origin inside the signed message, so the binding is
  carried by the credential there. Reusing that language for a bearer ticket was
  the error.

  Issuance-origin binding in `auth/token` is recorded as a follow-up rather than
  built here, because §2.8 already assigns credential validity and consumption to
  `auth/token` and forbids this package keeping a second view of it.

  Should-fix wording folded: `Mount` said Guard wraps every route when `/` is
  direct (now "every sensitive route"), and `login.go` called itself the only
  unauthenticated endpoint when the page is also unauthenticated (now the only
  unauthenticated **credential-processing** endpoint).

- **implementation r4 (2026-08-22, lector — `change_requested`, 2 blockers; both
  folded).** All seven r3 production repairs accepted. The two that remained were
  both about a claim outliving the thing it described.

  1. **`ServeLogin` performed no handshake check of its own.** `Mount` wraps it
     with `Guard`, but the exported handler mounted directly was an UNGUARDED
     login endpoint — a direct call with an attacker Host and Origin minted a
     ticket for a correct password. Its own doc comment said the route "is written
     as one: Origin and Host guarded like every other route", which was true of
     `Mount` and false of the function. For an endpoint that converts a password
     into a credential, the check belongs where the handler is and not only where
     someone remembered to wrap it, so it now runs in both places. The handshake
     check deliberately precedes the method check: cheap credential-free controls
     first.
  2. **This normative section was still stale.** Rev 11's reshaping was recorded
     only in review history, so §2.8 still said password may appear in the attach
     policy and still recommended
     `Any(ticket, mtls, sshChallenge, throttledPassword)` on `Config.Policy` —
     which r4's code makes impossible. A review-history note does not supersede a
     standing instruction, and an ADR whose normative section contradicts its own
     code is worse than one merely out of date: an implementer following it would
     have written the thing the code now forbids. Rewritten above.

  Also corrected: rev 11's claim that "every attach presents a single-use ticket"
  was false — mTLS and the SSH challenge attach on their own. The true invariant
  is narrower and is the one that matters: **a reusable secret is not among the
  credentials the attach path will accept.**

  Should-fix folded, and it was another test of mine that could not fail:
  `TestRegress3_ServeReturnsShutdownFailure` called `Manager.Shutdown` directly
  rather than `Handler.Serve`, so it passed against both the broken and the fixed
  code. It now exercises `Serve`, with an injectable `ShutdownGrace` so the
  boundary is observable without a 30-second wait and without leaking a stubborn
  App goroutine for the rest of the run. Both new controls fail without their
  fixes.

- **implementation r3 (2026-08-22, lector — `change_requested`, 7 blockers; all
  folded).** The r2 repairs were accepted. The seven that remained split into two
  kinds, and both are worth naming.

  **Repairs that were still one step short:**

  1. **`Resize` serialized `Size` but not `Flush`.** An App that dequeued an
     expansion and painted at a coordinate valid in the NEW size had its cells
     applied to the old, smaller grid and silently dropped — the render was lost
     and the screen stayed blank there. `Flush` now takes the same lock, so "the
     event is visible" and "the grid can accept these coordinates" are the same
     moment. My r2 test checked only `Size`, so it could not see this.
  2. **`MaxPending` was held for the whole authenticated pump**, because the
     release was deferred to the end of `serve`. That made it a cap on LIVE
     sessions: with `MaxPending=1`, one healthy session refused every newcomer
     despite spare `MaxSessions` and nothing actually pending. Released the moment
     authentication succeeds.
  3. **`Serve` still returned nil on a failed shutdown.** `return shutdownErr`
     evaluates the variable before the deferred function assigns it, so the error
     I had just been told not to discard was discarded anyway. Named result plus
     `errors.Join`.

  **Rev 11 was true of the prose and false of the code:**

  4. **The attach protocol still carried `subject`/`pw`, and the credential
     mapping still projected them.** So a custom client could authenticate a
     password directly over the WebSocket, exactly the thing the minter split
     exists to prevent, and `Config.Policy`'s docs still invited it. The fields
     and the mapping are GONE — a client cannot express a password on the attach
     path — and `Config.Policy` now says a password factor there can never be
     satisfied. My "password is not an attach credential" test only proved its own
     fixture had chosen a token-only policy.
  5. **The login body bound was bypassable.** `LimitReader` plus one `Decode` is
     not a bound: `Decode` stops at the end of the first JSON value, so a
     correct-password object followed by 8 KiB of junk decoded fine, never hit the
     limit, and minted a ticket. Now `http.MaxBytesReader`, exactly one value, and
     EOF required.
  6. **The login form could not be used with a mouse.** An unconditional
     document-click handler refocused the capture element, so clicking the
     username or password input immediately lost focus.
  7. **`PasswordPolicyExample` promised arms it could not reach.** It accepted
     ticket/mTLS/SSH factors, but `ServeLogin` projects only a subject and a
     password, so those arms were dead by construction. The parameter is removed:
     the helper builds the LOGIN policy, and the stronger mechanisms belong on
     `Config.Policy` via `RecommendedPolicy`.

  **A note on the r3 regression for #1.** My first version waited inside the
  resize gap for the concurrent `Flush` to finish — which deadlocks, because
  `Flush` wants the lock the gap is holding. The hang was itself evidence the
  serialization works, but the test had to be restructured to observe it rather
  than participate in it. All three of lector's controls now fail without their
  fixes.

- **implementation r2 (2026-08-22, lector — `change_requested`, 8 must-fixes; all
  folded).** Most r1 repairs were confirmed sound. The remaining eight were
  mostly repairs that had not gone far enough, which is its own lesson.

  1. **The rate limiter was handler-shared, not connection-local.** Moving it onto
     `sessionLoop` made it a field every concurrent `readPump` wrote and every
     `deliver` read — a data race, and clients throttling each other even without
     one. Now created per connection and passed down.
  2. **`Manager.Create` self-deadlocked on a cancelled context**: it held `m.mu`
     and then called `m.drop`, which takes `m.mu` again. A cancelled connection
     reaches that routinely. The check moved before the lock, where there is no
     session to drop anyway.
  3. **A resize could still be lost.** The read pump logged an overflow and moved
     on, but a resize is not one of a stream of equivalent events — it is the
     *only* report of that size, and the next arrives when the user next drags
     the window. It is now retried like any other event. Separately, submit-then-
     mutate left a window where an App could dequeue the event and read the OLD
     size; `Resize` and `Size` now share a mutex so the transition is atomic to an
     observer.
  4. **`PasswordPolicyExample` checked its constraint for non-nil, not for
     `FactorContextual`.** An identity factor passed, which would satisfy the
     `Any` on its own — adding a second way in rather than narrowing the first.
  5. **The narrowed auth claim was still overstated in three other doc
     comments.** I fixed one place in r1 and left `AppFactory`, `Create` and
     `serve` still saying unauthenticated calls were impossible. All now scoped to
     the authenticated network path.
  6. **Pre-auth connections had no cap and no deadline.** `MaxSessions` bounds
     only what exists after a hello, so a responsive non-browser that forges Host
     and Origin could hold arbitrary sockets and goroutines while consuming no
     session slot. Added `Limits.MaxPending` (refused with 1013, not queued —
     queueing moves an unbounded waiting room down a level rather than removing
     it) and `Limits.HelloTimeout`.
  7. **The transport docs still overclaimed.** `Mount` said it bound the
     validated config to the listener while accepting a server whose bind it never
     inspects, and `ServeWS` still told callers to wire it straight into
     `ws.Handler` — inviting the very post-upgrade arrangement r1 flagged. Both
     are now marked caller-owned, with `Serve` named as the path where the §2.5
     guarantees actually hold.
  8. **The client cleared its credential only on the success branch.** A failed
     measurement returned early, leaving the listener holding the ticket and the
     socket open awaiting a hello that would never come.

  Should-fixes also folded: a real `auth/mtls` policy is now driven through create
  AND reattach rather than the projection being tested alone; the attempt
  correlation has a negative control asserting a reference is present and differs
  per attempt; and `Handler.Serve` no longer discards `Manager.Shutdown`'s error,
  which was hiding a failed guaranteed-teardown behind a clean return.

  **One test-quality note worth recording.** My first version of the resize
  ordering test raced an observer against the real window and passed with the
  lock removed — the window is a few instructions wide, so it was testing luck.
  It now forces the interleaving through an explicit, documented test seam
  (`Backend.resizeGap`, nil in production), and the control fails as it should.

- **rev 11 (2026-08-22, Johno — password becomes a ticket minter).** Johno asked
  whether the login form could live "outside the auth". It can, and the shape is
  better than the one rev 9 implied.

  Rev 9 permitted a password factor in the ATTACH policy. Rev 11 moves it: a
  `POST /login` route runs a separate `LoginPolicy`, and on success mints a
  **single-use, 30-second ticket** that the WebSocket then presents like any
  other client. Four reasons, all pointing the same way:

  - **A reusable secret is never an attach credential.** A password converts
    into a ticket; mTLS and the SSH challenge attach on their own. (An earlier
    version of this bullet said every attach presents a ticket, which is not true
    — see the r4 and r5 corrections below.) Mixing a reusable secret into the same
    message as a spent one would make the replay properties of an attach depend on
    which field happened to be populated.
  - **The password crosses once, to a route that does nothing else.** It never
    touches session creation, frame delivery or the event stream, so no bug in
    those paths is reachable while a password is in flight.
  - **Lockout lives where the guessing happens.** The throttle wraps the login
    policy rather than being entangled with the attach policy that mTLS and SSH
    signatures also use.
  - **A captured hello cannot contain a password.** The worst a replayed hello
    yields is a spent ticket.

  This also closes the gap lector r1 named: the shipped client now has a login
  form, so password auth is reachable end to end rather than requiring a custom
  client.

  Consequences recorded deliberately:

  - The login route is the only unauthenticated endpoint in the package that
    **processes a credential** (the page is unauthenticated too, but takes
    nothing), so it is written accordingly — `Origin`/`Host` checked in the
    handler itself as well as by `Guard`, per the r4 correction, without which
    any page the user visits could POST a guess; body bounded before buffering;
    one uniform 401 for every cause including a malformed body; and no statement
    anywhere about whether a subject exists.
  - It **404s** when unconfigured, so a deployment that did not ask for password
    auth looks like one that never had the route.
  - `LoginPolicy` and `Issuer` must be set together: a policy with no issuer
    authenticates and then cannot admit anyone, and an issuer with no policy would
    mint on request.
  - A ticket-issue failure returns **503, not 401** — the credential was correct
    and reporting our failure as theirs would send a user to reset a password
    that is fine.
  - The password field is `type="password"` with
    `autocomplete="current-password"`, deliberately UNLIKE the capture textarea.
    That element is a keystroke conduit and must not be treated as a credential
    field; this one IS a credential field, so the browser should mask it and a
    password manager should be able to fill it. A manager is a net security gain,
    and suppressing it pushes users toward weaker memorable passwords.
  - No `<form>` element, so the CSP keeps `form-action 'none'`; the credential goes
    by `fetch` with `credentials: 'omit'` and `redirect: 'error'`, so it cannot
    carry ambient credentials and cannot be redirected elsewhere.

- **implementation r1 (2026-08-22, lector — `change_requested`, 9 blockers; all
  folded).** The cumulative framer was **approved**: current/acked/in-flight
  grids preserve the exact acknowledged baseline, stale acks cannot move it, and
  reset/resize force the right full snapshots. Every blocker was outside it, and
  most were the same mistake in different places — a control that was *described*
  rather than *bound to anything*.

  1. **Transport security was not bound to the transport.** `Config.Addr`/`TLS`
     were validated and then never used to make a listener, so a config claiming
     loopback-plus-TLS could be mounted on plaintext `0.0.0.0` and pass every
     check; Origin/Host ran *after* `websocket.Accept`. Now `Handler.Serve` builds
     the listener from the validated config, `Handler.Guard` refuses before the
     upgrade with a plain 403, `ExpectedHost` is REQUIRED (an optional check is an
     inferred one with extra steps), and `AllowedOrigins` is cloned.
  2. **mTLS could never succeed.** `authRequest` omitted `r.TLS`, so a verified
     chain reached the factor as nil. Projected via `mtls.FromConnectionState`.
     A rejected attach now also returns a safe correlation ID from
     `auth.WithAttemptSink`.
  3. **My "signature-level" auth claim was false.** `auth.Identity` is an
     exported struct, so any in-process caller can forge one — the tests do. The
     claim is narrowed to what is true: the transport path cannot reach a session
     without authenticating, and the exported lifecycle API is an in-process trust
     boundary at package-import granularity.
  4. **Reconnect was unreachable and takeover was allowed.** The App's context
     came from the WebSocket, so a disconnect killed the session and §2.8's detach
     window did not exist; nothing ever called `Evict`; and a second attach to a
     live session was accepted, so two browsers shared a grid. Sessions now derive
     lifetime from the manager, `Manager.Start` schedules the sweep, a connection
     **lease** makes takeover `ErrSessionBusy`, `Detach` is lease-scoped, and a
     read/write error is a CONNECTION failure rather than a session failure.
  5. **The un-coalesced stream dropped.** Overflow retried once and then advanced
     past the event. `deliver` now retries until accepted, cancelled, or the grace
     elapses. A resize mutated the grid while ignoring a failed event submission,
     leaving App and backend disagreeing about the size. `Limits.QueueDepth` was
     dead config — documented 1024, actual 256 — and is now the single source.
  6. **Client grid size was unbounded**, so `Resize(math.MaxInt, 2)` panicked in
     makeslice and large finite values could OOM. Bounded by `MaxCols`/`MaxRows`/
     `MaxCells`, with the product compared in `int64` because the `int`
     multiplication overflows to a small positive number — which is how a bounds
     check gets passed by the value meant to trip it.
  7. **`Stop` raced `Submit`**, producing a data race and a send on a closed
     channel. Producers and the sole closer now share an `RWMutex`.
  8. **The preventDefault tables were duplicated** — reserved chords hard-coded in
     both Go and the client, while the client's comment claimed they were
     injected. One structured rule table is now injected and walked.
  9. **Audit hygiene.** An authenticated Subject was concatenated raw and a
     newline in it forged a log line; "authenticated" only means a factor vouched
     for it, and an `allowed_signers` principal is not constrained to be
     newline-free. Every rendered field is sanitized. The client also dropped its
     ticket reference after presenting it.

  **A correction to me, and it matters:** my claim that the CSP nonce bug would
  have blocked script "on roughly half of all responses" was **wrong**. Browsers
  decode HTML entities before comparing the nonce, so `script-src 'nonce-a+b'`
  matches a source nonce written `a&#43;b` — lector verified that in headless
  Chromium. The `html/template` escaping is real; the browser consequence was
  invented from reading Go output rather than testing a browser. `RawURLEncoding`
  stays because source-level identity is genuinely simpler to reason about, but it
  is a simplification and not a bug fix, and the comment and test now say so.

  Also narrowed: `PasswordPolicyExample` accepted zero contextual constraints
  while its documentation described one, so it produced a policy weaker than its
  name promised — the constraint is now required. And the shipped client has no
  login form and never sends `subject`/`password`, so **password auth is not
  reachable end to end today**; that is recorded as a gap rather than left for
  someone to find after configuring one.

- **rev 9 (2026-08-22, Johno — decision reversal, not a review finding).**
  §2.8 rev 1 said "password auth is not accepted for WebTUI". Johno's
  instruction: allow it, and document it as not recommended. Folded as a
  *permission with named requirements* rather than a bare allowance, because the
  reasons password is weaker HERE are specific and worth writing down — what sits
  behind the credential is a shell, a password has no phishing resistance where a
  single-use 30-second ticket behind an enforced Origin allowlist does, and it is
  replayable where the other three
  mechanisms are spent, bound or key-backed. So the ADR now requires
  `auth.Throttle` and an `ipallow` constraint alongside it, keeps Argon2id, and
  recommends password as the fallback arm of an `Any` rather than the front door.
  `tui/web` deliberately does **not** refuse a password-only policy: the policy is
  the caller's to compose, and a package that silently second-guesses it would be
  lying about where the decision lives.

- **r8 (2026-08-21, lector — `approved_with_amendments`; all three applied in
  rev 8).** The host-state boundary was accepted as closing the
  ordering/content/task problem. **Amendment 1 was a security finding I had
  missed:** as written, the capture `textarea` grew without bound, so **the entire
  typed history — including passwords typed into the TUI — would sit in the DOM**
  of a network-reachable page, and a replacement occurring before the baseline
  left "baseline → current" ambiguous. The element is now **drained on every
  committed emission** (emit, clear, restore caret), which also collapses "the
  delta" into "the current value" so no general string diff can ever be
  introduced; it carries `autocomplete="off" autocapitalize="none"
  spellcheck="false"`, no `name` and no form, and references are dropped promptly
  — with **no claim** of memory erasure, which a JS engine cannot provide.
  **Amendment 2:** the normative table's `input (any)` row contradicted the state
  table (which correctly held composing input until `compositionend`) — the third
  such prose/table divergence, and it happened in the very revision where I said
  I would treat the table as normative. It is now state-aware: composing input,
  idle/late input, and `compositionend` are separate rows. **Amendment 3:**
  acceptance gains `7d`, covering empty-after-every-path, non-growing DOM value
  under a long stream, absence of password-like text after emission, and
  disabled-or-normalized replacement behavior.

- **r7 (2026-08-21, lector — `change_requested`, folded in rev 7).** Lector
  answered the question rev 6 had flagged, and the answer removed rev 6's
  foundation: **"composition-associated events are dispatched within one task" is
  not a standards guarantee.** UI Events specifies synchronous dispatch and
  relative ordering; HTML assigns user input to the user-interaction task source
  without making task coalescing an identity contract; and the UI Events
  algorithms still carry compatibility notes for input-before-end versus
  input-after-end. A settle timeout would only substitute another ambiguous time
  boundary. **The boundary is now HOST STATE:** all text is captured in a
  dedicated off-screen capture element, and the backend emits **baseline →
  current deltas**, advancing the baseline synchronously at `compositionend`
  (which UI Events dispatches *after* the control is updated). A late
  notification for already-committed work therefore carries an **empty delta** and
  emits nothing regardless of which task it lands in, so **ordering stops
  mattering**; a separately typed identical value changes host state again and
  survives; and cancellation needs no rule of its own, because reverting the host
  leaves an empty delta. `CompositionEvent.data` is no longer consulted at all.
  The one remaining assumption — control-updated-before-`compositionend` — is
  labelled as an assumption the browser matrix **verifies** rather than defines.
  **Amendments:** the normative table rows still described the deleted guard
  design (the third time I updated prose and left the table behind), and are
  rewritten against the delta model; the duplicate acceptance `7b` is renumbered
  to `7c`; and a new case (viii) covers a composition-associated `input`
  delivered in a *later* task.

- **r6 (2026-08-21, lector — `change_requested`, folded in rev 6).**
  **Must-fix 1 invalidated the whole emit-then-dedup shape**, not just my
  predicate: content + `inputType` is *not* a unique event identity, so if a
  browser emits no duplicate and the user's next action inserts the same text —
  routine on mobile/IME, where no key event intervenes — the guard swallows a
  real insertion. It was still positional dedup wearing a content predicate. The
  machine now **stages the composition and commits exactly once at the task
  boundary**, folding same-task `input` by **dispatch boundary** rather than by
  content, so no post-hoc guard exists at all and a composed `x` followed by a
  typed `x` yields two insertions. **Must-fix 2:** empty `compositionend.data` is
  *not* a cancellation signal — UI Events permits it whenever the IME or device
  does not expose the string, so rev 5 could discard a valid commit. The
  **editing host's value/delta is now the authoritative staged value** with
  `data` as a hint, empty `data` plus a non-empty staged value **commits**, and
  cancellation is a distinct transition defined by the host returning to the
  pre-composition baseline. **Amendments:** acceptance 7 rewritten into seven
  explicit sequences; the browser matrix made a **required CI/release gate**,
  because a suite that skips everywhere cannot satisfy an acceptance criterion;
  and the modified-key rule now handles `key` values that are not a single
  Unicode scalar (`Dead`, `Unidentified`, multi-scalar, expanding lower-case) by
  dropping them, since `KeyEvent.Code` holds exactly one rune.

- **r5 (2026-08-21, lector — `change_requested`, folded in rev 5).** The r4
  corrections (per-rune parity, zero `Base`/`Shifted`, `ModSuper`, `Policy`,
  AltGraph) were accepted, but the state machine I had *just added* could lose
  input in two ways. **Must-fix 1:** the table `preventDefault`s `paste`, and a
  cancelled paste means the Clipboard API never queues the resulting insertion —
  so there is no paired `input`, and my unqualified suppress-next-input flag
  would have discarded the **next genuinely typed character**. `paste` now
  expects no following `input`, and any defensive dedup must be qualified by
  `inputType === "insertFromPaste"` plus matching content, scoped to that action.
  **Must-fix 2:** `compositionend` → `input` is not a portable ordering — UI
  Events/Input Events permit the composition `input` *before* the end and do not
  guarantee one after, and browsers differ — so rev 4's "drop composing input and
  wait" could lose the composed text entirely. The machine now buffers during
  composition, **emits at `compositionend` from `event.data`**, and dedups a
  trailing `input` by **content plus `inputType`** rather than by position, which
  makes both orderings safe and cannot swallow an unrelated key. Both orderings,
  cancellation, and cancelled-paste-then-typing are pinned in real Chromium,
  Firefox and WebKit rather than synthetic dispatch alone. **Amendments:** the
  table rewrite is finished — resolution step 4 no longer says
  `input`/`compositionend`, and the modified-key row now shows `Base: 0`,
  `Shifted: 0`, `ModSuper`, and `Code` derived from `KeyboardEvent.key` instead
  of a base-layout codepoint the DOM does not expose; acceptance 10a states
  policy success for every branch and consume for the ticket branch only.
  **AltGraph:** lector agreed the conservative default is right — swallowing
  legitimate Ctrl+Alt chords is the worse failure, so `getModifierState` stays
  the only automatic signal with the opt-in for layouts that misreport.

- **r4 (2026-08-21, lector — `change_requested`, folded in rev 4).**
  **Must-fix 1:** committed text had **two** emitters — an ordinary IME
  completion fires `input` *and* `compositionend`, so rev 3 would have inserted
  the text twice, and paste has the same shape (`paste` then a paired `input`).
  §2.9 now carries an explicit state machine where `input` is the sole emitter and
  composition/paste are state transitions, with a suppress-next-input flag for
  paste; acceptance replays whole sequences asserting exactly one insertion.
  **Must-fix 2 killed two claims I had made without checking the source:**
  (a) `Base`/`Shifted` cannot come from a browser — `KeyboardEvent.code` is a
  *physical-key* identifier and the DOM exposes no base/shifted codepoints, while
  `tui/term` populates those fields only on the kitty CSI path — so they are
  always `0`, and my "layout-independent matching like kitty" claim is withdrawn;
  (b) emission is per **rune**, not per grapheme cluster —
  `decoder.go`'s `actPrint` emits one `KeyEvent` per rune, and the web backend
  now matches that scalar sequence so components cannot distinguish backends.
  **Amendments:** the Related line now says `Policy`; the shared policy is spelled
  in the real `Leaf`/`NewPolicy` graph; acceptance 10 scopes the atomic consume to
  the ticket branch; AltGraph relies solely on `getModifierState`, with Ctrl+Alt
  inference behind an explicit opt-in so legitimate chords are never silently
  swallowed; and DOM `metaKey` maps to **`ModSuper`**, not `ModMeta` — `tui.Mods`
  is in kitty order, where super is Cmd/Win and meta is the historical Meta, so
  the obvious mapping would have broken macOS Cmd chords.

- **r3 (2026-08-21, lector — `change_requested`, folded in rev 3).**
  **Must-fix 1 was self-inflicted:** rev 2's input table invented event shapes
  (`KeyEvent{Key,Mods}`, `MouseEvent.Delta`, `FocusEvent{Focused}`,
  `ResizeEvent{Cols,Rows}`) instead of using `tui/events.go`'s real ones —
  `KeyEvent{Kind,Code,Base,Shifted,Mods,Text}`, wheel as
  `MouseEvent{Kind: MouseWheel, Button: WheelUp…}`, `FocusEvent{Gained,
  Terminal}`, `ResizeEvent{W,H}`. I described an API instead of reading one; the
  table is now written against the actual structs, with named keys as `tui.Key*`,
  repeat as `Kind: KeyRepeat`, `keyup` dropped (`KeyRelease` is kitty-only and
  never synthesized), and committed text setting `Code`+`Text` the way the
  terminal decoder does. **Must-fix 2:** the rows overlapped and AltGraph was
  unhandled — on layouts reporting AltGraph as Ctrl+Alt, rev 2 would have emitted
  a *command* instead of the character typed. A normative resolution ladder now
  orders reserved shortcuts → `isComposing`/AltGraph → named keys → committed
  text → modified keys → drop. **Must-fix 3:** the backend consumes
  `auth.Policy` (ADR-0001's `Authenticator` no longer exists), `App` creation and
  input are gated on `Policy.Authenticate`, and the atomic consume is scoped to
  the **ticket branch only** — rev 2's blanket wording contradicted direct mTLS
  and challenge authentication. **Should-fixes:** "if WebSocket wins" and the
  stale Q2 references removed; the 2 000-event burst clarified as a token-bucket
  allowance rather than absorption the 1 024-slot queue cannot provide.

- **r2 (2026-08-21, lector — `change_requested`, folded in rev 2).** The r1
  substance was accepted; these were internal contradictions. **Must-fix 1:**
  §2.9 still *described* an input-mapping table instead of *being* one while
  acceptance 7 asserted it existed — §2.9 is now the table itself, with concrete
  default limits (64 KiB message, 500 events/s sustained, 2 000 burst, 1 024
  queue, 2 s to close on sustained overload). **Must-fix 2:** rev 1's "every
  attach requires a fresh ticket" contradicted the policy, which admits mTLS and
  the signed challenge as identity-bearing mechanisms in their own right; the
  invariant is now **fresh authentication** — each attach re-runs the completed
  policy, the ticket branch consumes a new ticket, and mTLS/challenge
  authenticate directly or via an internal one-use attach grant.
  **Should-fixes:** the client now scrubs the fragment with
  `history.replaceState` before opening the socket (a fragment escapes HTTP and
  `Referer` but persists in history and copied URLs); the pending aggregate is
  defined against the last **acknowledged** baseline, so a transmitted-but-
  unacknowledged row stays in it; and the stale §2.9/§2.10 and Q3 references I
  introduced while renumbering are repaired.

- **r1 (2026-08-21, lector — `change_requested`, folded in this revision).**
  **Must-fix 1 was a correctness defect, not a nit:** rev 0's latest-wins frame
  coalescing is wrong precisely because frames carry only dirty rows — drop the
  frame containing row A, then change only row B, and row A never reaches the
  client, permanently. The pending frame is now a **cumulative union of unsent
  dirty rows** relative to the client's baseline, with monotonic revisions,
  acknowledgement, and full initial/reconnect resync; a queue turns out to be
  unnecessary once the single slot accumulates (§2.4, acceptance 4/4b).
  **Must-fix 2 corrected a wrong security claim:** rev 0 said mTLS "kills the
  CSWSH class outright". It does not — a browser may automatically re-present a
  previously selected client certificate, making mTLS an *ambient* credential
  like a cookie; it authenticates the TLS peer without proving the initiating
  page is trusted. `Origin` validation is now mandatory for every browser
  mechanism, mTLS included (§2.8). **Must-fix 3:** ticket transport specified end
  to end — URL **fragment** (never sent in the HTTP request) plus the first
  WebSocket application message, not the query string and not
  `Sec-WebSocket-Protocol`; atomic consume before any `App` attach or input;
  never echoed or logged (§2.8). **Must-fix 4:** acceptance criterion 7 assumed a
  documented input-mapping table that rev 0 never contained — §2.9 now defines
  `key` vs `code`, modifiers/repeat, IME/composition, paste, reserved shortcuts
  and `preventDefault`, mouse, focus/resize, unknown-event behavior, **and** the
  message-size / event-rate / bounded-queue / close-on-overload limits.
  **Should-fixes:** the font-box invariant is now measured **pixel** tracks (not
  `ch`, which is itself font-relative), a width-2 head spans two tracks, a
  width-0 continuation emits no box, and overflow clipping plus paint containment
  make box containment — not the probe — the safety guarantee, with
  `UnicodeCore` described conservatively (§2.6); all five open questions resolved
  (§7); browser hardening (CSP, `frame-ancestors 'none'`, no-store, explicit
  `Host`/`Origin`) added (§2.7). **Cross-ADR:** one shared policy —
  `Any(singleUseSSHChannelTicket, mtls[, sshChallenge])` optionally wrapped in
  `All(ipallow, ...)`, with IP optional and never identity-bearing;
  `auth/token` owns credential validity and consumption while this package owns
  session lifecycle (§2.8). Approach (A) approved in principle, on the explicit
  basis that the seam experiment is the deliverable.
  Review doc: `$KB_ROOT/agents/lector/reviews/2026-08-21-golib-tui-web-auth-coupled-design-review.md`
