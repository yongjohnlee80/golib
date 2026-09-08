# ADR-0012 — golib/tui: client-side interaction delegation for the web backend

**Tags:** `type:adr` `status:proposed` `owner:shared` `repo:golib` `area:tui`
`kind:web-backend` `kind:render-engine` `kind:analysis`

**Abstract:** An analysis of how the web backend could give users a more
interactive experience *without modifying the TUI application*, per Johno's
brief of 2026-09-08. Four candidate render technologies were named
(`html/template`+TS, HTMX, Preact, petite-vue). The finding is that **none of
them is a render-engine decision**, because the shipped client does not build
DOM from templates at all — it mutates a pre-allocated grid of `<i>` elements
from a JSON cell diff. The decision that actually matters is a **protocol and
seam** decision: whether a component may declare that some of its interactions
are *predicted locally* and reconciled by the server. This ADR frames that
option space, scores the four named options against it, and proposes a
minimal opt-in seam. **No implementation is proposed yet** — §7 names the
measurements that should gate it.

**Read §10 and §11 before acting on §6.** §10 records that this analysis
assumes ADR-0009 §1.1's audience (remote access to a CLI TUI) and that
AutoKB's browser-primary audience inverts the recommendation. §11 evaluates
Johno's stronger alternative — full component hoisting via
`widget.AllowedOnClientSide()` — and concludes it beats §6's recommendation
for the case it targets, once narrowed from "hoist a component" to "hoist a
text buffer".

- **Status:** **Proposed** (2026-09-08) — analysis only, for Johno's decision
- **Date:** 2026-09-08
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none
- **Related:** ADR-0009 (the web backend; §2.7 rendering, §5 alternatives (B)
  and (C)), ADR-0011 (`Keyer` — the identity seam this would build on),
  ADR-0003 (cell buffer and the diff), ADR-0004 (component tree), ADR-0007
  (widget set), and autodb's ADR-0064 §2.1 (**an accepted decision refusing a
  second frontend**, with a named reopening condition — §1.3 below).

---

## 1. Context

### 1.1 What is actually shipped, measured in the tree at `fd7f55f`

The brief asks "react or golang's native http template?". Against the code,
that is a false dichotomy — **neither is the render engine today**:

| Claim | Measured |
| --- | --- |
| `html/template` renders the grid | **No.** It renders the *page shell* only (`page.go:34`, inlining CSS/JS under a CSP nonce). |
| `render.go` emits dirty rows as HTML (ADR-0009 §2.7) | **Dead code.** `renderRow`'s only caller is `render_test.go:13`; `writeCellSpan`, `inlineStyle` and `decoration` are reachable only through it. Nothing in the serving path reaches any of them (§8.2). |
| The browser is a "dumb surface" displaying server HTML | **No.** It is a 411-line vanilla-JS painter over a JSON cell-diff WebSocket (`protocol.go`, `assets/client.js`). |

So ADR-0009 §2.7 describes an implementation that was replaced during
delivery and never corrected in the prose. **That correction is a deliverable
of this ADR regardless of which option is chosen** (§8).

What the client actually does (`client.js:83-147`):

- `rebuild(w,h)` pre-allocates `w*h` `<i class="c">` elements into a CSS grid,
  once per resize. autodb's default window is **8120 spans** (ADR-0064 §1.2).
- `paint(u)` mutates one element in place — `textContent`, `style.color`,
  `style.background`, weight/italic/decoration — from a `wireCell`.
- `applyFrame(m)` applies the diff, moves the cursor, then acks **after**
  painting, so the server cannot advance its baseline past a frame the client
  never drew.

That is already close to the floor for a cell grid. There is no template to
speed up and no reconciliation to remove — the server computed the exact diff,
and the client performs `len(updates)` property writes. **A framework added
here would add work, not remove it.**

### 1.2 Client size, for the comparison that follows

Measured at `fd7f55f`: `client.js` **16,579 B raw / 6,179 B gzipped**,
`client.css` 3,880 B. Zero dependencies, zero build step. This is the baseline
any option has to justify itself against, and it is the concrete form of
requirement 4 ("lightweight and free of dependencies as much as it can").

### 1.3 Two prior decisions this engages, and their reopening conditions

**ADR-0009 §5** rejected **(B) semantic HTML per widget** — "it needs a second
render path on `Component` (every widget implementing both), and it forfeits
the cell-grid layout guarantees". It kept **(C) PTY + xterm.js** as a
documented fallback.

**autodb ADR-0064 §2.1** is stronger and more recent (accepted 2026-08-24):
*"No webapp. Keep the cell-grid frontend. [DECISION — gates the rest]"*. Its
reasoning was a measurement, and it named its own reopening condition:

> Reopen on §2.1(1)'s native-browser measurement, or on a profile showing the
> cell-grid path itself is the limit.

**That condition does not appear to have been met.** §2.1(1) was explicitly
"Johno's to run" — view the UI in a native browser rather than inside
terminal-browser — and no measurement is recorded. So *if* the motivation for
this work is latency, the gate ADR-0064 set is still open and should be closed
first; it costs one browser window.

**But the brief is not a latency brief.** It asks for "more interactive
experience", and requirement 2 asks specifically that "user input events and
handlings are accommodated in the client side". That is a different question
from "is the cell grid slow", and ADR-0064 did not answer it. It is legitimately
open.

### 1.4 The axis nobody has measured: round-trip amplification

ADR-0064's numbers are **loopback, headless** — 18 ms p50 keystroke→frame,
73 B/frame. Its own §1.2 says so.

Every interaction in the current design costs one round trip, because the
client cannot know what a keystroke will draw. On loopback that is invisible.
The cost is `RTT + server frame time`, so:

| Link | Per-keystroke cost (est. from ADR-0064's 18 ms loopback) |
| --- | --- |
| loopback | ~18 ms — imperceptible |
| LAN | ~20-25 ms — imperceptible |
| same-region WAN (~30 ms RTT) | ~50 ms — perceptible on held-key repeat |
| cross-continent (~150 ms RTT) | ~170 ms — cursor motion feels detached |

The web backend exists **specifically** to reach a CLI server *remotely*
(ADR-0009 §1.1). So the interaction cost that matters is the one that was not
measured, and it degrades linearly with distance — while held-key repeat and
scrolling multiply it by the repeat rate.

**This, not paint cost, is the honest case for client-side prediction.** It is
also a *new* reopening argument that ADR-0064 neither considered nor refused.
It should be measured (§7) before anything is built.

---

## 2. The real question, reframed

Requirement 2 asks to "separate the render from the logic processing".
Against the architecture, that separation **already exists and is total**: the
component tree does all logic and produces cells; the client does all painting
and produces input events. The seam is `Backend.Flush([]CellUpdate)`.

What the brief actually wants is the *next* separation: letting the client
**answer some interactions itself** instead of asking the server. And there the
architecture has one hard blocker:

```go
// tui/backend.go:60
type CellUpdate struct {
    X, Y int
    Cell Cell
}
```

**A `CellUpdate` carries no component identity.** The backend is handed a
rectangle of glyphs and cannot tell which widget drew any of them. So the
client cannot be told "these cells are a list, and `j`/`k` move its cursor"
without a **new, additive seam** carrying identity plus a declared interaction
contract from the tree to the backend.

ADR-0011 already added half of it: `Keyer interface { Key() any }` is
implemented on `main` (`tui/component.go:16`) and gives a component a stable
identity independent of position. The framework never consults it — containers
do. **It is the natural anchor for a delegation declaration.**

### 2.1 The safe subset — why this need not be a security question

Requirement 3 worries that "the framework can't decide what is safe to be in
the client and what must stay in the BE". The worry dissolves if the client is
never given **authority**, only permission to **guess ahead**:

- **Client-side prediction:** the client applies the change it expects, marks
  those cells provisional, and the next server frame overwrites them. The
  server is unconditionally authoritative; a wrong prediction self-corrects
  within one RTT.
- **Client-side authority:** the client decides an outcome and the server
  trusts it. **This must be refused outright**, and no directive should be able
  to opt into it.

Under prediction, "what is safe in the client" has a mechanical answer: an
interaction is delegable when it is **presentation-local and server-derivable**
— cursor movement within a visible list, scroll offset, tree expand/collapse,
character echo into a text input before submit. Every one of those is something
the server would have accepted anyway, and none of them changes application
state until a *commit* event (Enter, submit, activate) which is **never**
delegated.

The dangerous cases are excluded by construction rather than by judgement:
anything that validates, authorises, mutates, or reads data the client does not
already have on screen cannot be predicted, because the client has nothing to
predict *from*.

This is the same design as client-side prediction in networked games and local
echo in terminals — both decades-proven, both server-authoritative.

---

## 3. The option space

The four named options are not four points on one line. They sit on two
independent axes:

**Axis A — what crosses the wire for a delegated component:**
1. cells (status quo),
2. a semantic state model (JSON: items, selection, scroll),
3. HTML fragments.

**Axis B — who owns the DOM for it:**
1. the existing vanilla painter,
2. a client micro-framework,
3. the server.

The status quo is A1/B1. The four options map on as follows.

### 3.1 Option 1 — `html/template` + TypeScript

**What it is:** finish ADR-0009 §2.7 as written (server renders dirty rows as
HTML) and add TypeScript to the client for maintainability.

- **Pros:** zero new runtime dependency; TS gives the 411-line client types and
  refactoring safety, which it genuinely lacks; keeps everything in Go +
  stdlib.
- **Cons:** **it makes interactivity strictly worse.** Server-rendered rows
  send *more* bytes than the 73 B cell diff and still cost a full round trip —
  it moves in the opposite direction from requirement 2. TS also introduces the
  build step requirement 4 resists, and the `//go:embed` of a single
  hand-written `client.js` becomes an embed of a build artifact.
- **Scope:** small (revive ~50 dead lines, add a `tsc` step and CI wiring) but
  **negative value** for the stated goal.
- **Verdict:** **the TS half is worth considering on its own merits; the
  `html/template` half should be deleted, not finished** (§8).

### 3.2 Option 2 — HTMX

**What it is:** declarative attributes that issue HTTP requests on interaction
and swap returned HTML fragments into the DOM.

- **Pros:** genuinely tiny author-side surface; no build step; excellent fit
  for *form-shaped* server-driven UI.
- **Cons:** **structurally the wrong shape for this problem.** HTMX's model is
  "every interaction is a server round trip that returns HTML" — which is
  precisely the cost §1.4 identifies as the thing to remove. It would replace
  one 73 B WebSocket frame with an HTTP request/response cycle carrying markup.
  It also has no notion of a persistent cell grid, and ~14 KB gz (advertised;
  verify at pin) buys nothing the current client lacks.
- **Scope:** medium, and it would fight the existing WebSocket transport.
- **Verdict:** **reject.** Right tool, wrong problem.

### 3.3 Option 3 — Preact

**What it is:** a ~4 KB gz (advertised; verify at pin) React-compatible
renderer with hooks and a VDOM.

- **Pros:** a real component model, so a delegated widget could be a proper
  semantic island (a `<table>` with real rows, keyboard handling, native
  focus/scroll/selection, and accessibility for free — the cell grid has
  none of that). Familiar to anyone who has used React. Mature.
- **Cons:** a VDOM diff on top of a diff the server already computed exactly is
  pure overhead for the *grid*; it only pays for itself on delegated
  **islands**. Needs a build step (JSX) or the ugliness of `h()` calls.
  Introduces the first real third-party runtime dependency into a
  zero-dependency package — requirement 4's central tension, and the golib
  convention's tightened dependency rule (2026-08-21) requires the stdlib
  alternative be checked and the justification recorded.
- **Scope:** large. Per delegated widget: a JS component, a state model on the
  wire, and a server-side declaration. Plus the build pipeline.
- **Verdict:** **the right choice only if the answer to §4's question is "many
  delegated islands with rich semantics".** Overkill for two or three.

### 3.4 Option 4 — petite-vue

**What it is:** ~6 KB gz (advertised; verify at pin), Vue-flavoured
directives (`v-model`, `v-for`, `@click`) applied to **server-rendered HTML
in place** — explicitly designed for progressive enhancement rather than owning
the page.

- **Pros:** **the closest fit to the brief's own words.** Requirement 3 asks
  for "a middleware or a directive that certain components can stay fully in
  the client side" — petite-vue *is* a directive system that enhances existing
  markup and leaves everything else alone. No build step. It does not want to
  own the DOM, so the cell grid stays exactly as it is and only delegated
  islands get directives. Smallest credible framework that still gives
  declarative binding.
- **Cons:** much smaller ecosystem than Preact; still a third-party runtime
  dependency; the project is low-activity (verify maintenance status before
  pinning). Directives are strings evaluated at runtime, which needs care under
  the existing CSP (`page.go` uses a nonce; `unsafe-eval` must **not** be
  introduced).
- **Scope:** medium-small.
- **Verdict:** **best of the four named options**, if a framework is used at
  all.

### 3.5 Option 0 — the unnamed option: extend the existing client

**What it is:** no framework. Add a delegation module to `client.js`: a
declarative interaction spec per delegated component, interpreted by ~150-250
lines of vanilla JS, predicting locally and reconciling on the next frame.

- **Pros:** keeps zero dependencies and zero build step (requirement 4 fully
  satisfied); requirement 1 satisfied — no TUI application changes, and no
  changes at all for apps that never opt in; the prediction machinery is the
  *same* code for every widget kind, because it operates on cell rectangles and
  a movement rule rather than on widget semantics. Ships behind a capability so
  an old client simply never predicts.
- **Cons:** hand-written state handling, which is what TypeScript would help
  with (see §3.1); no accessibility or native-selection win, because the DOM
  stays a cell grid; the prediction rules are a new correctness surface needing
  its own tests.
- **Scope:** medium. The seam (§5) is the bulk of it; the client module is small.
- **Verdict:** **the recommendation** (§6).

---

## 4. Comparison

Scored against the four requirements. "Interactivity" = removes the round trip
from the interactions Johno named.

| | Interactivity | No TUI app change (r1) | Logic stays in BE (r3) | Dependency-free (r4) | New runtime bytes (gz) | Build step | Scope | A11y / native selection |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **Status quo** | none | ✅ | ✅ | ✅ | 0 | no | — | none |
| **1. `html/template`+TS** | **worse** | ✅ | ✅ | ✅ (TS is build-time) | 0 | **yes** | small | none |
| **2. HTMX** | worse | ✅ | ✅ | ✗ | ≈14 KB | no | medium | partial |
| **3. Preact** | ✅✅ | ✅ | ✅ w/ discipline | ✗ | ≈4 KB + app code | **yes** (JSX) | **large** | ✅✅ |
| **4. petite-vue** | ✅ | ✅ | ✅ w/ discipline | ✗ | ≈6 KB | no | medium | ✅ (on islands) |
| **0. extend client.js** | ✅ | ✅ | ✅ **by construction** | ✅ | ≈2-4 KB (own code) | no | medium | none |

Framework sizes are **advertised figures and must be verified at pin time**;
they are order-of-magnitude inputs here, not measurements.

**Benefit measured against cost.** The benefit of every option is bounded by
the same quantity: the number of interactions that currently cost a round trip
and could stop doing so.

**The delegable interactions live in `tui/widget`, not in the consuming app** —
measured, and it is the single most important fact for scoping this. autodb's
`results.go` and `search.go` contain **no** cursor-key handling at all;
`explorer.go` and `history.go` reference only `KeyEnter` (a commit, which is
never delegated). Navigation is entirely the widget layer's: `Table.HandleEvent`
forwards keys to its inner `list` (`widget/table.go:200-218`), and `list` does
the cursor arithmetic.

Widgets with cursor/scroll key handling today, by count of
`KeyDown/Up/PageDown/PageUp/Home/End` references:

| Widget | Refs | Delegation kind |
| --- | --- | --- |
| `select.go` | 7 | line cursor |
| `list.go` | 6 | line cursor |
| `tree.go` | 6 | line cursor (+ expand/collapse) |
| `textarea.go` | 6 | echo + cursor |
| `bufferview.go` | 6 | scroll |
| `editor.go` (+keymap) | 7 | echo + cursor |
| `textinput.go` | 2 | echo |
| `tabs.go` | 2 | line cursor |
| `split.go` | 2 | — (structural, not delegable) |

`table` inherits `list`'s, so it is covered by the same rule rather than being
a tenth case.

**This is what makes requirement 1 achievable in the strong sense.** If
`Delegable` is implemented on **three prediction kinds** in golib's widget set —
line cursor, scroll, echo — then *every* consumer gets client-side prediction
with **zero application changes**: autodb's 7,783 lines of TUI are untouched,
and so is every future consumer. The brief's "no op for regular TUI
application" is not an opt-in the app has to remember; it is the default,
because the widgets it already uses carry the declaration.

**Three prediction kinds across nine widgets does not justify a framework.**
It justifies a prediction rule and a way to declare it, which is Option 0.
Preact becomes the right answer if and only if the goal grows to "delegated
components should be real HTML with real accessibility" — a materially larger
ambition than the brief states, and one that re-opens ADR-0009 §5(B)
deliberately rather than incidentally.

---

## 5. Mock-up architecture

Three additions. Everything else — widgets, layout, runtime, `Flush`, the cell
diff, the terminal backend — is untouched, and an app that declares nothing
behaves exactly as it does today.

### 5.1 The declaration (`tui`, additive, optional)

```go
// Delegable is an optional capability interface, detected by assertion like
// Focusable and Keyer (ADR-0011 §2.1). A component that implements it declares
// which of its interactions a remote client MAY predict locally.
//
// Declaring costs nothing on a terminal backend: term ignores it entirely.
type Delegable interface {
    // Delegation returns the component's predictable interactions, or the
    // zero value for none. Called during Render, on the loop goroutine.
    Delegation() Delegation
}

type Delegation struct {
    // Kind is the prediction rule the client applies. A CLOSED vocabulary —
    // the client ships an implementation per kind and refuses unknown ones,
    // so a component can never smuggle behaviour to the client.
    Kind DelegationKind // DelegateLineCursor | DelegateScroll | DelegateEcho

    // Bounds is the rect, in the component's own Surface coordinates, that
    // the prediction may repaint. The framework translates to screen space.
    Bounds Rect

    // Keys the client may answer locally. Everything absent here — Enter,
    // activation, anything that commits — goes to the server as it does now.
    Keys []KeyPredicate
}
```

`Kind` being a closed enum is the load-bearing part. The client executes **its
own** code selected by kind; the server never sends behaviour. That is what
makes requirement 3's worry structural rather than a matter of review
discipline.

**`Delegable` is implemented by WIDGETS, not by applications.** Per §4, the
cursor arithmetic already lives in `tui/widget` — so `list`, `select`, `tree`,
`tabs`, `bufferview`, `textinput`, `textarea` and `editor` carry the
declaration, and `table` inherits `list`'s through the forwarding it already
does. An application implements nothing, changes nothing, and cannot get this
wrong. An application that wants to *refuse* prediction for one instance
returns the zero `Delegation` from a wrapper; that is the only app-level knob,
and it is opt-**out**.

### 5.2 The seam (`tui` → `Backend`, additive)

`CellUpdate` stays byte-identical. Delegation travels beside the diff:

```go
// OPTIONAL capability. A Backend that does not implement it is handed nothing
// and behaves exactly as today — which is every backend except web.
type DelegationSink interface {
    // SetDelegations replaces the frame's delegation set. Called immediately
    // before Flush, on the loop goroutine, with the same
    // must-not-block contract as Flush (ADR-0009 §2.4).
    SetDelegations([]ScreenDelegation)
}

type ScreenDelegation struct {
    Key    any            // from Keyer (ADR-0011) — stable across frames
    Rect   Rect           // screen coordinates
    Spec   Delegation
}
```

Collected during the existing render walk, which already visits every component
and already knows each one's placed `Rect`. No second tree traversal.

### 5.3 The protocol (`tui/web`, additive)

One new server→client field and one new client→server message:

```jsonc
// server→client, on the existing frame message
{ "t": "frame", "rev": 41, "u": [...], "cur": {...},
  "dg": [ { "k": "results", "x": 0, "y": 3, "w": 120, "h": 30,
            "kind": "linecursor", "keys": ["Down","Up","PageDown","PageUp"] } ] }

// client→server: "I predicted, here is what I assumed"
{ "t": "pred", "rev": 41, "k": "results", "n": 3 }
```

`pred` is **advisory, not authoritative**. The server applies the real key
events it also received, and its next frame is the truth. `pred` exists only so
the server can *notice* divergence and force a full repaint of that rect
(`Full` already exists per-frame; this needs a per-rect variant).

### 5.4 The client (`assets/client.js`, +≈200 lines)

```
  applyFrame(m)                       ← unchanged path
     ├─ paint(u) for each update      ← unchanged
     ├─ delegations = m.dg            ← NEW: replace the active set
     └─ clear provisional marks       ← NEW: server frame is truth

  keydown(e)
     ├─ d = delegationAt(focusRect, e.key)
     ├─ if d and d.kind is implemented:
     │     predict(d, e)              ← repaint d's rect locally, mark provisional
     │     send({t:'pred', ...})       ← advisory
     │     send({t:'key', ...})        ← STILL SENT: the server must run the real handler
     └─ else: send({t:'key', ...})    ← unchanged
```

The keystroke is **always** sent. Prediction only changes *when the user sees
something*, never whether the server processes it. That single property keeps
the server authoritative and makes a prediction bug a cosmetic flicker rather
than a state divergence.

**Reconciliation:** a predicted rect is marked provisional. The next frame
touching it overwrites it unconditionally. If `rev` advances without touching a
provisional rect within N frames, the client discards the prediction and
requests a repaint of that rect. This is the whole correctness argument, and it
is one rule.

---

## 6. Recommendation

1. **Do not adopt any of the four named options as a render engine.** The
   shipped client is not a template renderer, and the option that would make it
   one (§3.1) moves away from the goal.
2. **Adopt Option 0** — extend the existing client with prediction, declared
   through the seam in §5 — *if* §7's measurements justify it. It is the only
   option that satisfies all four requirements, and it satisfies them *by
   construction* rather than by discipline: the declaration sits on the golib
   widgets that already own cursor movement (§4), so "no op for regular TUI
   application" is not an opt-in an author has to remember — it is what happens
   when they change nothing. Three prediction kinds cover nine widgets and
   therefore every consumer.
3. **Take the TypeScript half of Option 1 as a separate, independent
   question.** A 411-line untyped client that is about to grow a prediction
   state machine is a reasonable place for types; that is a maintainability
   decision with its own trade-off (a build step in a package that currently
   has none), and it should not ride along on this one.
4. **Keep petite-vue as the documented fallback**, and Preact as the answer to
   the larger question — "should delegated components be real, accessible
   HTML?" — which is ADR-0009 §5(B) reopened deliberately, not a side effect
   of a latency fix.
5. **Reject HTMX** for this purpose.

---

## 7. What must be measured before committing

The prior decision (ADR-0064 §2.1) was made on measurement and named its
reopening condition. This one should be held to the same standard, and none of
these is expensive:

1. **Close ADR-0064's open gate.** View autodb's `--web-ui` in a **native
   browser window**, not inside terminal-browser, and re-judge. This is still
   Johno's to run and it costs one window. If it feels fine locally, §1.4 is
   the only remaining case and it is a *remote* one.
2. **Measure the RTT amplification claim** (§1.4). Same Playwright harness as
   ADR-0064, with an injected latency (`tc netem` or CDP throttling) at ~30 ms
   and ~150 ms RTT. Report keystroke→**presentation**, not keystroke→frame
   received — ADR-0064 was explicit that its own numbers stopped short of
   paint, and repeating that error would make this ADR's premise as unproven as
   the one it replaces.
3. **Confirm the three prediction kinds cover the nine widgets in §4.** The
   table there is a count of key references, not a reading of each widget's
   movement semantics. A widget whose cursor rule the client cannot reproduce
   from what is on screen (wrapped lines in `textarea`, folded rows in `tree`)
   must be excluded rather than approximated — a prediction that is *usually*
   right is worse than none, because the flicker is unpredictable.
4. **Held-key repeat.** Measure a held `Down` at the OS repeat rate against
   both latencies. If prediction helps anywhere, it helps most here, and if it
   does not help here it probably should not be built.

If (1) resolves the complaint, **the correct outcome of this ADR is to change
nothing** except the §8 corrections.

---

## 8. Corrections owed regardless of the decision

These are true now, independent of any option:

1. **ADR-0009 §2.7 describes an implementation that does not exist.** It says
   the server emits dirty rows through `html/template`; the server emits JSON
   cell updates and the client paints them. The prose should match the code.
2. **Four functions in `render.go` are dead** — `renderRow`, `writeCellSpan`,
   `inlineStyle` and `decoration`. `renderRow`'s only caller is
   `render_test.go:13`; the other three are reachable only through it. (The
   `decoration` string does appear in `backend.go:275`, but as prose inside a
   comment about `text-decoration-style`, not a call.) Either delete them and
   their test, or, if a no-JS fallback is wanted, make that an explicit
   decision and wire it up. Dead code with a passing test reads as a supported
   path — `render_test.go` asserts full `<i class="c" style="…">` markup that
   nothing serves.
3. **Do not delete `render.go`.** `cssColor` is live (`protocol.go:encodeFrame`
   calls it for every cell), and `hex2`, `cellColorToken`, `tokenFG`/`tokenBG`
   and `defaultFGToken`/`defaultBGToken` are live through it and through
   `encodeFrame`'s reverse-video swap. `Metrics` and `Metrics.valid` are used
   by `backend.go` and `protocol.go`. A deletion has to **split** the file: the
   colour/metric half stays, the HTML-emitting half goes.
4. Removing the dead half removes the **last** `template.` reference in
   `render.go` (`template.HTMLEscape`, line 74), leaving `page.go` as the only
   `html/template` importer in the package. That is worth noting because it is
   the precise sense in which "the web backend renders with `html/template`" is
   false: after the cleanup, the import is not even present outside the page
   shell.

---

## 9. Non-goals

- Running arbitrary terminal programs in a browser — that is ADR-0009 §5(C)
  (PTY + xterm.js) and stays the right answer to that different question.
- Client-side **authority** over anything (§2.1) — for the *prediction* design
  of §5, refused permanently. **§11 is a different design and does require
  authority**, deliberately and only over a free-text buffer the user authored;
  §11.2(e) and §11.5 bound where that is admissible. The two must not be
  conflated: §5 predicts and can be wrong harmlessly, §11 owns and cannot.
- A second render path per widget (ADR-0009 §5(B)). Delegation adds a
  *prediction* declaration, not a second `Render`.
- Changing `CellUpdate`, `Flush`, `Component`, or any widget.
- Any change to the terminal backend, which ignores every seam proposed here.

---

## 10. Applicability — what this analysis assumes about the audience

**This ADR reasons from ADR-0009 §1.1's premise**, and that premise is narrow:

> "the goal this tui.web engine is just to allow users to remotely connect to a
> CLI server where TUI is already available without extra costs. It won't be a
> fancy webapp but more likely a way to access the CLI tool which already has a
> TUI."

Every recommendation above — especially Option 0 — is scored against that: the
browser is a **remote-access surface for people who are already TUI users**, so
the only thing missing is latency, and prediction is the cheapest way to remove
it.

**That premise does not hold for AutoKB** (Johno, 2026-09-08). AutoKB is a
knowledge-base application offering Obsidian- and Notion-class features, hosted
server-side, with **devs *and directors* working from the browser** and
therefore "much more user interactions". There the browser is the **primary
product surface for most of its users**, not a convenience for TUI users.

For that audience the cell grid is not slow — it is **structurally unable to be
a document surface**, and no amount of prediction changes any of it:

- **No reflow, no responsive layout.** `client.js:rebuild` pins
  `repeat(cols, <cellW>px)` from a measured monospace advance. A laptop and a
  4K display get the same fixed grid.
- **No document semantics for assistive technology.** The DOM is `w*h` bare
  `<i class="c">` elements — no headings, no landmarks, no ARIA anywhere. A
  screen reader sees 8,120 anonymous spans. For a director-facing tool that is
  not a nicety.
- **No links, images, or embeds.** There is no anchor element in the client.
- **No variable-width type, and no rich-text editing.** WYSIWYG in a character
  grid is a category error, not a hard problem.
- **Selection and copy are row-major over a fixed grid** (DOM order is
  row-major), so a selection cannot follow a paragraph or a column. Exact
  find-in-page behaviour across per-grapheme elements is untested and should
  be measured rather than assumed either way.

So for AutoKB the answer is the one **ADR-0009 §5(B)** rejected — a semantic
frontend. Its rejection reasons do not transfer: "a second render path per
widget" is a real cost, but §5(B) weighed it against a *convenience* surface.

**And the structural recommendation for AutoKB is neither §5(B) nor this ADR:
do not render the TUI to the browser at all.** Make the engine/RPC API the
shared thing and treat TUI and Web as two independent clients of it.
autokb ADR-0084 §5 already defines that RPC surface, and §6.3 already leaves
the door open — *"rendered via `golib/tui`'s web backend **or dedicated HTTP/WS
endpoints**"*. TUI-first sequencing then costs nothing: building it first
hardens the API the web client consumes.

**Do not apply Option 0 to AutoKB's director-facing surface.** It is the right
answer for operating a TUI remotely, which is a different job.

---

## 11. Full component hoisting — `widget.AllowedOnClientSide()`

Johno's alternative, evaluated on its own terms (2026-09-08). The proposal is
stronger than "predict and reconcile": a widget declares itself hoistable and
the client takes over **rendering *and* event handling** for it, owning the
state, with the backend fetching the result after a deliberate delay.

```go
editor := widget.NewEditor(widget.AllowedOnClientSide())

enhancedWebUIBackend, err := enhancedWeb.Open()
tui.NewApp(app, tui.WithBackend(enhancedWebUIBackend)).Run(ctx)
```

*"with `AllowedOnClientSide()` … the js render engine can hoist the component
in the clientside for components' full business logic such as event handling on
the FE side. Of course, if the WithBackend is provided with `term.Open()` … this
option is to be ignored."*

### 11.1 Where the design is right, and it is right about several things

1. **The API shape is correct and needs no invention.** `NewEditor` is already
   `func NewEditor(opts ...EditorOption) *Editor` (`widget/editor.go:140`), so
   `widget.AllowedOnClientSide()` drops into an existing variadic option list
   with zero API friction.
2. **Per-INSTANCE opt-in beats per-type capability.** §5.1 above put the
   declaration on the widget *type* (`Delegable` asserted like `Focusable`) with
   an opt-out wrapper. Johno's is better: an app can hoist the note editor and
   leave the command palette alone, and nothing is hoisted unless someone asked.
   **§5.1 should be revised to this form** if any of this is built.
3. **"Ignored by `term.Open()`" is the right default and costs nothing.** The
   terminal backend never reads the flag; every existing consumer is unaffected
   whether or not it declares hoisting.
4. **A text editor is the one widget where client authority is genuinely
   defensible.** Text entry is the highest-frequency interaction there is, and
   the buffer is *the user's own input* — the server holds no competing opinion
   about what the user typed. Debounced sync is how every real web editor
   works. Johno's instinct about *which* widget to hoist first is correct.

### 11.2 Where it breaks down

**(a) "Full business logic" means a second IMPLEMENTATION, and that is the
cost — quantified.** The JS engine does not know what an editor *is*. golib's
editor is a **four-mode vim editor**: `ModeNormal` / `ModeInsert` /
`ModeVisual` / `ModeVisualLine` (`editor_keymap.go:12-21`), a keymap keyed by
mode class, a `jk` chord with a 300 ms timeout (`editor.go:148-149`),
grapheme-aware cursor motion, and text objects — **2,058 lines** across
`editor.go` (1,406), `editor_keymap.go` (201), `textbuffer.go` (267) and
`textutil.go` (184).

Hoisting "full business logic" means writing those 2,058 lines again in
JavaScript and keeping the two in agreement forever. ADR-0009 §5(B) rejected a
second **render** path per widget; this is a second **behaviour** path, which is
strictly worse: a render divergence is a visual bug, while a behaviour
divergence means the editor *does different things* depending on which frontend
you opened. Nothing in the option declaration prevents that drift, and no test
can span the two languages cheaply.

**(b) Rendering the hoisted widget is a fork in the road, and both branches
cost.** Either the hoisted editor still paints as cells — in which case it
keeps every §10 limitation and buys only latency — or it becomes real DOM (a
`<textarea>` / contenteditable island absolutely positioned into the rect the
TUI's layout assigned it). The second is what makes hoisting *worth* doing, and
it splits layout authority: the browser wraps text by its own rules while
golib's `textbuffer` and `Editor.wrap` decide the TUI's. Cursor position,
scroll offset, line numbers and the surrounding status line then disagree with
the content. That is ADR-0009 §2.6's wide-grapheme hazard one level up, and
§2.6's containment trick (geometric boxes that clip) does not apply to a
reflowing island.

**(c) One-directional sync cannot express a server-side write, and AutoKB needs
them.** "BE can only fetch the contents of the buffer" is a pull. But the
server legitimately wants to *change* the buffer: a file reload, an undo driven
from a menu, an **agent writing to the document**, or another user editing.
AutoKB is exactly this case — ADR-0084 §6.3 promises *"real-time synchronized
document viewing and editing"*, and the KB's whole premise is agents authoring
alongside humans. A client that owns the buffer turns every server-side write
into a conflict, and resolving conflicts on shared text is OT or CRDT. That is
a large, well-understood cost, and it is the reason Notion and Google Docs
carry one. **This is the single biggest gap in the design as stated.**

**(d) "Deliberate time lapse" is a data-loss window with a named cause.**
Whatever the debounce is, that is how much typing a closed tab, a dropped
socket or an eviction discards. ADR-0009 §2.8 specifies **idle eviction with a
configurable timeout and a hard cap on concurrent sessions** — so the session
can legitimately disappear inside the window. Solvable (flush on
`beforeunload`, local persistence, sequence-numbered acks) but it must be
designed rather than assumed.

**(e) The security posture changes from prediction to AUTHORITY, and the option
must therefore not be universal.** §2.1 refuses client authority permanently;
this design requires it. For a text buffer that is acceptable — it is the
user's own text, and the server must validate on persist regardless. It is
**not** acceptable for a widget whose logic *enforces* something: a permissions
picker, a destructive-action confirmation, a query builder deciding what SQL
runs. So `AllowedOnClientSide()` cannot be an option available on every widget;
it belongs only on widgets whose state is free user input, and that restriction
should be expressed in the type system rather than in documentation.

**(f) `enhancedWeb.Open()` as a second backend duplicates the expensive half.**
A separate package means a second implementation of sessions, authentication,
SSO, the handoff seam, peer binding and transport — the part ADR-0009 §2.8 calls
*"the real work, not rendering"*, which took that ADR 30 revisions and eight
consecutive review rounds to get right (§2.12.8-15 are one mistake made eight
ways). Hoisting is a **capability of the existing web backend**, not a new
backend: `web.Open(web.WithHoisting(true))`, negotiated through the `hello`
message that already carries `pointer` / `fontok` / `dark` capability flags.

### 11.3 The narrowing that saves it

Keep Johno's API **verbatim** and change only what it means. Not *"hoist this
component's logic"* but:

> **This widget's TEXT BUFFER is client-owned, edited by the client's single
> text-editing implementation.**

That one substitution answers (a), (b) and (e) at once:

- **One JS implementation total**, not one per widget — `textinput`, `textarea`
  and `editor` all hoist onto the same client buffer.
- **It stays a closed capability** (like §5.1's `Kind` enum): the server ships
  no behaviour, and a component cannot smuggle code to the browser.
- **The option only exists where it is safe**, because "has a free-text buffer"
  is exactly the safe set from (e).
- The island renders as a real `<textarea>` — native selection, native
  clipboard, screen-reader support, IME — which is the actual §10 win, not just
  latency.

**And the audience split dissolves the modal-editing fork.** The client
implementation is *plain* text editing; golib's four-mode vim keymap is **not**
hoisted. That is not a compromise — a director does not want modal editing, and
a dev who does can use the TUI, where the real editor already lives. The person
who wants `ciw` is not the person in the browser. So the 2,058 lines never need
a second implementation, and the option's docs say plainly: **hoisting trades
the modal keymap for native text affordances.**

(c) and (d) remain and are unavoidable: pick a conflict policy explicitly —
last-writer-wins with a visible "changed on the server" banner is a legitimate
and cheap answer for single-author documents, and CRDT is the answer only if
concurrent authorship is a requirement. Do not leave it implicit.

### 11.4 Verdict

**The design holds up well as an API and for one widget class; it does not hold
up as a general mechanism.** Adopted as written — hoist any component's full
business logic — it reintroduces ADR-0009 §5(B)'s rejected cost in a worse
form, per widget, in a second language. Adopted as §11.3's narrowing — hoist a
text buffer onto one client implementation, opted in per instance, ignored by
the terminal backend — it is **better than this ADR's own Option 0** for the
case it targets, because it delivers the §10 affordances (selection, clipboard,
a11y, IME) that prediction cannot.

**Where it lands for AutoKB:** a good mechanism for the **dev** path — a real
editing experience in the TUI's web backend without a second frontend. Still
**not** the answer for the director-facing document surface, which wants a web
client against the RPC API (§10). Those two conclusions are compatible: hoisting
makes the remote-TUI path genuinely usable for writing, and the API-backed web
client serves the audience that was never going to use a TUI.

### 11.5 The diagnosis is exactly right — and the text path is already half-built

Johno, 2026-09-08: *"I suspect the browser events are captured in the client
side first then sent to the TUI backend, and what I'm suggesting is to prevent
the roundtrip."*

**Confirmed in the code, and it is worth writing the path out because it makes
the proposal cheaper than §11.2(a) implies.**

```
keydown  (client.js:210)
  ├─ reserved(e)?           → browser keeps it, nothing sent      (client.js:164)
  ├─ named or modified key  → e.preventDefault(); send({t:'key'})  (client.js:216-220)
  └─ plain text             → return; the `input` event handles it (client.js:215)

input    (client.js:205) → drain()                                (client.js:186)
  └─ send({t:'text', x: <capture element's value>}) and CLEAR IT SYNCHRONOUSLY
```

Then: WebSocket → server → the App's handler → cells → `frame` →
`applyFrame` → `paint`. **Nothing on screen changes until the frame returns.**
The client holds the event, has already decided it is meaningful, and has
everything it needs to act — and does nothing but forward it. That is the round
trip, and the only reason it exists is that **the client does not know what the
event means**. Johno's option is precisely the missing declaration.

**The text case is cheaper than it looks, because the browser is already doing
the work.** `capture` is a real DOM input element with full composition
handling (`compositionstart` / `compositionupdate` / `compositionend`,
`client.js:197-203`) — so IME, dead keys and autocorrect already work natively.
`drain()` reads its value and then **destroys it synchronously on every
keystroke**.

So for a hoisted text buffer, §11.3's narrowing is not "write a text editor in
JavaScript". It is **"stop draining"** — let the native element keep its own
value, make it visible in place of the cells, and sync on the debounce. The
browser's text control already provides selection, clipboard, IME,
screen-reader support and undo. §11.2(a)'s 2,058 lines are the cost of hoisting
*golib's modal editor*; they are not the cost of hoisting *a text buffer*, and
§11.3 is what keeps them off the bill.

**But the drain is a SECURITY CONTROL, not an implementation detail** — and
hoisting reverses it. `client.js:180-184` states why it exists:

> Draining is also what keeps the DOM from becoming a keystroke log: without
> it, every character the user ever typed — passwords included — would sit in
> an element on a page reachable over the network.

That is ADR-0009 r8's requirement ("the capture buffer DRAINS, so no typed
history lingers in the DOM"). A hoisted buffer is, by definition, typed history
living in the DOM until the next sync. Three consequences, all of which belong
in the option's contract rather than in a reviewer's memory:

1. **The drain stays the default.** Only a widget that explicitly declares
   hoisting keeps its value, and only for its own element.
2. **`AllowedOnClientSide()` must be unavailable for secret input.** Whatever
   masks or collects a credential must not be hoistable — reinforcing
   §11.2(e)'s point that this cannot be a universal widget option. A
   `PasswordInput` that accepted the option would be a defect, so it should not
   compile.
3. **A hoisted buffer needs a bounded lifetime**, not just a debounce: cleared
   on blur-to-another-widget, on detach, and on session teardown. The
   `beforeunload` flush of §11.2(d) and this clearing are the same mechanism
   viewed from two directions, and they must agree about which happens first.

**This strengthens the verdict in §11.4 rather than changing it.** The
diagnosis is correct, the API is correct, and for a text buffer the
implementation is far smaller than a general hoisting mechanism would be —
provided it is scoped to the buffer (§11.3) and provided the drain reversal is
made explicit and type-restricted rather than assumed harmless.
