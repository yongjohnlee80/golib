# ADR-0009 — `golib/tui`: the web Backend (remote TUI over HTTP)

- **Status:** **Proposed** (2026-08-21 — authored by jarvis from Johno's request
  to reach a CLI's existing TUI from a browser. Awaiting design review; lands on
  `tui-web`.)
- **Date:** 2026-08-21
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none — purely additive. `tui.Backend`, `Component`, `Surface`,
  `Cell` and every widget are unchanged.
- **Related:** ADR-0001 §2.4 #5 and ADR-0002 (the Backend seam and capability
  model this implements a second time), ADR-0003 (cell buffer, diff, the
  one-write rule), ADR-0006 (style tokens → CSS), the `golib` convention's
  tightened dependency rule (2026-08-21), and the sibling `golib/auth` ADR-0001
  (this backend consumes its `Authenticator`, it does not roll its own).

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
ADR** (§2.9), not a side effect.

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

- the backend holds **at most one pending frame**; a newer frame **replaces**
  the older (latest-wins coalescing), so a stalled viewer costs one frame of
  memory, not the application;
- a frame reaches the client **atomically** — never a half-painted screen;
- a dropped frame is **logged** (§2.8): a silently dropped frame is a debugging
  trap.

### 2.5 Transport, and encryption by default

Built on golib's own `server/http` (and `server/ws` if WebSocket wins).

**Transport decision (open — see §7 Q1):** WebSocket versus SSE + POST. Note the
dependency asymmetry the tightened `golib` rule cares about: `server/ws` already
carries `github.com/coder/websocket` in `go.mod`, so choosing WebSocket adds
**no new module** to the repo — only to this package's import graph. SSE + POST
is stdlib-only but pays input latency on every keystroke.

**Encryption is on by default** (Johno, 2026-08-21: *"we should also have
encryption by default"*). `wss://` is not a separate crypto layer — it is the WS
handshake over TLS, the same TLS 1.3 as HTTPS. Required:

- A **non-loopback bind without TLS is a startup error**, never a warning. No
  silent plaintext, ever.
- Plaintext is permitted only for a loopback bind, where the kernel or the
  tunnel is the boundary. Whether even loopback should get an auto-generated
  ephemeral self-signed certificate (fingerprint printed) is §7 Q2.
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

- **Bound a mismatch to its own cell.** Per-cell layout with explicit advance
  (a CSS grid or per-cell `ch` sizing), so a font disagreement cannot cascade
  along the row. A wrong glyph width must be ugly, never desynchronizing.
- **Honor `Cell.Width` exactly**, including `0` as a continuation cell: a
  continuation emits no glyph and no box.
- **Pin the font stack** in the served CSS and document it.
- Report `UnicodeCore` only when the client confirms agreement on a probe
  string.

### 2.7 Rendering

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
or memory leak) on disconnect, eviction and process shutdown. Reconnect policy
must be stated, not implied.

**Authentication is mandatory. There is no unauthenticated mode, not even on
loopback** (Johno, 2026-08-21: *"ideally we only want users to connect to WebTUI
via ssh keys or other secured manners only"*):

- Accepted mechanisms are **SSH-key proof, mTLS client certificate, or a
  short-lived token minted over an already-authenticated channel**.
  **Password auth is not accepted for WebTUI**, even though `golib/auth`
  supports it for other callers.
- An IP allowlist may be an **additional** factor (AND), never the only one — an
  address is not an identity.
- This backend consumes `golib/auth`'s `Authenticator` interface. If that
  package is not ready, `tui/web` still takes the *interface* from day one so
  the implementation drops in without touching transport or session code.
- A browser cannot read `~/.ssh`, so "SSH key" needs a mechanism: §7 Q3.

**WebSocket hijacking defenses (mandatory).** The WS handshake is an HTTP request
that carries cookies but is **not** subject to the Same-Origin Policy, and
**CORS does not apply to it** — so a cookie-authenticated socket is directly
open to **CSWSH**: an attacker page opens a socket in the victim's browser and
the browser attaches the credential, yielding a keyboard on the CLI. Therefore:

- **validate `Origin` against an allowlist, deny by default** (necessary but not
  sufficient — `Origin` is forgeable by a non-browser client);
- **never authenticate the socket with an ambient cookie**;
- **no long-lived token in the URL query string** (it leaks into access logs,
  `Referer`, history, proxies) — use a **one-time ticket** exchanged and
  invalidated at handshake, or `Sec-WebSocket-Protocol`;
- bind the session to the client and revoke on disconnect: no resurrection by
  replaying a URL;
- **mTLS is the strongest available answer** and kills the CSWSH class outright,
  because an attacker's page cannot present a client certificate;
- connection caps, idle timeouts, per-connection memory limits.

### 2.9 The seam report is a deliverable

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
4. With the client's reader blocked: `Flush` returns without blocking, older
   pending frames coalesce to one, the app loop keeps running, and the drop is
   logged.
5. A client resize changes `Size()` and the next frame matches the new grid; a
   mid-frame resize does not tear.
6. A wide grapheme (one CJK, one emoji) occupies exactly two columns in the DOM,
   a `Width: 0` continuation emits no glyph, and a deliberately mismatched font
   does not shift the remainder of the row.
7. Every row of the documented input-mapping table has a test asserting the
   emitted `tui.Event`; an unmapped browser key is dropped explicitly, with a
   test proving no phantom event.
8. Session cap and idle eviction are enforced; after disconnect and after
   eviction, `Stop` has run, goroutines have exited (leak check), and the
   credential no longer attaches.
9. A non-loopback bind without TLS **fails to start**, with a test asserting the
   error; a plaintext loopback bind is permitted only per §7 Q2's resolution.
10. `Origin` validation denies by default; a cross-origin handshake is refused;
    a one-time ticket cannot be redeemed twice.
11. `Capabilities()` reports `KittyKeyboard: false`, and no capability is
    `TriYes` without a verifiable basis.
12. The §2.9 seam report exists and is specific: each finding names the method or
    type, what the terminal-shaped assumption cost, and what a future
    non-terminal backend should do — or states explicitly that the seam needed
    nothing, which is itself the result.
13. `go vet` clean, race-clean, `doc.go` + `README.md` present, every exported
    symbol documented, tests stdlib-only.

## 7. Open questions for review

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

- **r1 (pending)** — design review requested 2026-08-21.
