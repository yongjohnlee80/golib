# ADR-0009 — `golib/tui`: the web Backend (remote TUI over HTTP)

- **Status:** **Proposed (rev 5)** (2026-08-21 — authored by jarvis; lector
  design r1-r5 folded (r5: a canceled-paste flag that could eat a keystroke, and
  a non-portable composition ordering) — a correctness defect in rev
  0's frame coalescing, a wrong security claim about mTLS, and r2's internal
  contradictions. See Review history.
  Lands on `tui-web`.)
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
- **Password auth is not accepted for WebTUI**, though `golib/auth` supports it
  for other callers.
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
4. **Text** — a non-composing `input`, or the reconciled composition emitted at
   `compositionend` (§ state machine).
5. **Modified keys** (Ctrl/Alt/Meta + printable).
6. Anything else → dropped.

| Browser event | Emitted | preventDefault |
| --- | --- | --- |
| reserved shortcuts: Ctrl/Cmd+`T`/`N`/`W`/`L`/`R`, Ctrl+Tab, `F5`, `F11`, `F12`, Cmd+`Q` | *(nothing — the browser keeps them; README says so)* | **no** |
| `keydown` with `isComposing`, or `getModifierState("AltGraph")` (or Ctrl+Alt **only** when `TreatCtrlAltAsAltGraph` is enabled) | *(nothing — the character arrives via `input`)* | no |
| `keydown`, `key` ∈ {Enter, Tab, Escape, Backspace, Delete, Insert, Arrow*, Home, End, PageUp, PageDown, F1–F12} | `KeyEvent{Kind: KeyPress, Code: tui.KeyEnter \| KeyTab \| KeyEscape \| … \| KeyF12, Mods}` | **yes** |
| same, with `repeat: true` | `KeyEvent{Kind: KeyRepeat, …}` | **yes** |
| `keyup` for a forwarded key | *(nothing — `KeyRelease` is kitty-only and is never synthesized here)* | no |
| `input` with `isComposing: false` and no matching dedup guard | one `KeyEvent{Kind: KeyPress, Code: r, Text: string(r), Base: 0, Shifted: 0, Mods: 0}` **per rune**, matching `tui/term`'s `actPrint` exactly | no |
| `compositionstart` / `compositionupdate` | **state only** — buffer, emit nothing | no |
| `compositionend` | **emits the composed text** from `event.data`, per rune, then arms a content-qualified dedup guard (§ state machine) | no |
| `keydown` with Ctrl/Alt/Meta + printable (and **not** rule 2) | `KeyEvent{Kind: KeyPress, Code: <codepoint of `KeyboardEvent.key`, lowercased>, Base: 0, Shifted: 0, Mods: ModCtrl \| ModAlt \| ModSuper \| ModShift}` — `Code` comes from `key` (the produced character), never from a base-layout codepoint the DOM does not expose | **yes** |
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

**The text state machine — no duplication, and no lost input (r5).** Rev 4 fixed
double insertion but introduced two new ways to *lose* input. Both are corrected
below; the guiding rule is that a guard may only ever suppress an event
**positively identified as the duplicate**, never "the next one".

*Composition (r5 must-fix 2).* Rev 4 dropped every `input` with
`isComposing: true` and waited for a later non-composing `input`. That ordering
is not portable: UI Events and Input Events permit the composition `input`
**before** `compositionend` and do **not** guarantee another `input` afterwards,
and browsers have genuinely differed. So:

| State | Event | Action |
| --- | --- | --- |
| idle | `compositionstart` | → composing; clear the buffer |
| composing | `compositionupdate` | buffer `data`; emit nothing |
| composing | `input` with `isComposing: true` | buffer the value; **emit nothing yet** |
| composing | `compositionend` | **emit** `event.data` (per rune); arm a dedup guard holding that exact text; → idle |
| composing | cancellation (empty `compositionend`, or Escape) | discard the buffer; emit nothing |
| idle | `input` matching the armed guard's text with `inputType` ∈ {`insertCompositionText`, `insertText`} | consume the guard; emit nothing (it is the duplicate) |
| idle | any other `input` with `isComposing: false` | emit per-rune `KeyEvent`s |

Emitting **at `compositionend`** using its `data` is what makes both orderings
safe: an `input` that arrived *before* the end is buffered rather than lost, and
one arriving *after* is recognised by **content plus `inputType`** rather than by
position, so it can never swallow an unrelated keystroke. The guard is cleared on
the next event either way.

*Paste (r5 must-fix 1).* Rev 4 armed an unqualified suppress-next-input flag —
which was wrong twice over. Because the table calls `preventDefault` on `paste`,
the Clipboard API does **not** queue the resulting insertion, so **there is no
paired `input` to suppress**; the flag would therefore have discarded the next
genuinely typed character. Corrected: `paste` emits `PasteEvent` and expects **no
following `input`**. Any defensive dedup must be qualified by
`inputType === "insertFromPaste"` **and** matching content, scoped to that paste
action — never a positional "next input".

Acceptance replays, and asserts exactly one insertion or none as appropriate:
composition-then-`input`; `input`-then-`compositionend`; a cancelled
composition; **a cancelled paste followed immediately by ordinary typing, proving
the typed character survives**; and a paste with no trailing `input`. Because
these orderings are browser-specific, the suite is pinned in **real Chromium,
Firefox and WebKit** rather than synthetic dispatch alone (build-tagged,
skipping cleanly where no browser is available).

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

1. `tui/examples/demo` — **unchanged source** — runs in a browser via
   `tui.NewApp(root, tui.WithBackend(web.New(...)))`: renders, accepts
   keystrokes, repaints, exits cleanly.
2. `git diff --stat` shows **no modifications** under `tui/` outside the new
   `tui/web/` package (plus docs/examples). Mechanically checked.
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
   phantom event. **Whole sequences** are replayed and assert exactly one
   insertion: an IME composition (`compositionstart`→`update`×n→`end`→`input`)
   and a paste (`paste`→`input`). Emission is **per rune**, matching
   `tui/term`'s `actPrint` — proven by comparing the event stream for the same
   text against the terminal decoder — and `Base`/`Shifted` are `0` throughout.
   Pinned cases: a non-US layout, a dead-key sequence, a multi-codepoint emoji,
   and a browser reporting no AltGraph state.
7b. Resource limits: an oversized message is rejected, a sustained event flood
   closes the connection instead of growing the queue, and the queue bound holds
   under load.
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
