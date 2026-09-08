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
- Client-side **authority** over anything (§2.1). Refused, permanently.
- A second render path per widget (ADR-0009 §5(B)). Delegation adds a
  *prediction* declaration, not a second `Render`.
- Changing `CellUpdate`, `Flush`, `Component`, or any widget.
- Any change to the terminal backend, which ignores every seam proposed here.
