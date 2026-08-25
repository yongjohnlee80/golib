# ADR-0010 — `golib/tui`: mouse interaction (click-to-focus, Table, Editor)

- **Status:** **Proposed (rev 9)** (2026-08-25, authored by jarvis at Johno's
  request). **Rev 6 adds §2.5, double-click as activation** — Johno, 2026-08-25:
  *"Let's implement double mouse click as ENTER."* Nothing in rev 5 changes. Companion to autodb ADR-0064 §2.2, which is the consumer that asked
  for this and which decided *not* to build a webapp instead.
  - **Lector r2 `change_requested`** — three findings against this ADR, **all
    CONFIRMED against the code and folded into rev 2**. Each was a case of rev 1
    specifying something the codebase cannot represent or already contradicts:
    - **The focus-mutation retry broke this ADR's own invariant.** Rev 1 said
      re-hit-test and deliver; the replacement could then be a *different*
      focusable widget receiving a press while unfocused, contradicting
      focus-before-delivery and criterion 1. Rev 2 re-runs candidate/scope/focus
      for the replacement, exactly once, and skips delivery if it mutates again
      (§2.1 step 5).
    - **Discarding a pending insert rune reversed an accepted ADR silently.**
      ADR-0008 binds *every* non-chord input to settle the pending rune first,
      and `editor.go:375 settlePendingRune` implements it. Rev 1's "discard" was
      both data loss (the user typed that character) and an unannounced
      supersession. Rev 2 **settles at the old caret, then moves** — command
      state is still discarded, since discarding a count or operator modifies no
      text (§2.3).
    - **One-visual-row soft-wrap scrolling is not representable.** `Editor`
      stores a logical-line origin only (`editor.go:23-27`), and render, `Cursor`
      and `ensureVisible` all start at logical `top`. Rev 2 makes the wheel step
      **one logical line in both wrap modes** and defers a visual-row viewport
      origin — which would need new state plus invariants across five paths — to
      its own ADR (§2.3).
  - Rev 1 closed r1's Table-ownership finding outright.
  - **Lector r3 `change_requested`** — one finding here, **CONFIRMED**, and it
    retires an idea I had now defended twice: the focus-mutation **retry is not
    implementable at all**. A replacement mounted by a `FocusEvent` handler is
    invisible to `hitTest` until the next layout pass (`tree.go:40`,
    `routing.go:174`, `app.go:220-232`), so the synchronous retry rev 2 specified
    could only ever reach a stale ancestor. Rev 3 takes the **skip** rule lector
    proposed in round one. Two amendments also landed: the genuinely stale §5
    "unspecified" bullet (r2's correction had been appended to a *different*
    alternative — the wrong bullet), and "the press never modifies buffer text",
    which contradicted rev 2's own pending-rune settlement.
  - **Lector r4 `change_requested`** — one finding, **CONFIRMED**: the trap
    boundary must be `currentScope()`, not `trapScopeOf(focused)`. golib has a
    repair state in which the focused child dies, `focused` becomes 0, and the
    trap survives on `scopeStack`; deriving the boundary from the focused node
    then finds no trap and lets a click escape it. `currentScope()` is the
    primitive keyboard traversal already uses and handles that path explicitly.
    Criterion 4b pins the state (§2.1).
  - **Lector r5: APPROVED** (2026-08-24) — no remaining design or wording blocker
    across five rounds. Approval is of the DESIGN only: it is explicitly not
    authority to implement, and Johno's authorization remains an external gate.
  - **Rev 5 (implementation round r3)** restates criterion 18 only. Lector's r3
    review cleared all production code and held that three representative web
    tests do not satisfy "every behaviour through both producers" — correct, and
    my wording was worse than unachievable: it named `TestBackend` as "the
    terminal path" while `TestBackend` bypasses the terminal decoder, so the
    terminal producer had no coverage at all. Criterion 18 now states the
    three-layer structure that was actually intended, and both producers now
    have a behavioural integration slice.
  - **Lector r1 `change_requested`** (2026-08-24) raised three findings against
    this ADR; **all three are CONFIRMED against the code and folded into rev 1**,
    two of them by reproducing the behaviour rather than reading it:
    - **§2.2 was not implementable.** Rev 0 said `Table` would translate row
      presses "regardless of which node the hit test reached". Under ADR-0004's
      target-first, no-capture routing it cannot: `List` is a placed child, so a
      body press reaches `List` directly and `Table` never sees it. A probe on
      the unmodified tree (`TestCurrentTableMouseRouting`, run independently of
      lector's) selected **row 2** for a body press at absolute `Y=3` — already
      correct — and **row 0** for a header press at `Y=0` from a pinned
      selection of 2, which is the real defect. Rev 1 adopts the ownership the
      routing actually permits (§2.2).
    - **§2.1 cited a capability golib does not have.** `FocusTarget` exists only
      in autodb (`tui/results.go:48`) and appears nowhere in `golib/tui`, so
      "honours `FocusTarget()`" was unimplementable. Rev 1 drops the promise.
    - **The trap guard was assumed, not present.** `App.requestFocus`
      (`tui/focus.go:29-48`) records *entering* a trapping scope but has no
      guard rejecting a candidate *outside* an existing trap, so acceptance
      criterion 4 could not have been met by "find candidate, call
      requestFocus". Rev 1 specifies the scope computation as a precondition.
  - Wheel-under-pointer without focus was explicitly approved as intentional.
- **Date:** 2026-08-24
- **Module:** `github.com/yongjohnlee80/golib` (`tui`, `tui/widget`)
- **Supersedes:** nothing. **Completes** ADR-0004 §2.5.2 (mouse routing) and
  ADR-0007 (the widget set) for pointer input.
- **Related:** ADR-0002 (SGR mouse in the terminal backend), ADR-0004
  (hit-testing and routing), ADR-0007 (widget contracts), ADR-0009 (the web
  Backend, which reports mouse in cell coordinates).

**Abstract:** `golib/tui` transports and routes mouse events end to end, and
seven widgets already act on them — yet **nothing focuses a widget when you click
it**, and `Editor` ignores the pointer entirely. The result, measured in autodb's
browser frontend, is that clicking anywhere does nothing observable. This ADR adds
one framework rule (a primary press focuses the focusable node under the pointer,
within the active focus trap, before the widget sees the event), corrects `Table`'s
single unwanted delegation, and specifies what a press means inside a modal
`Editor`. It declines to invent either a "clickable" abstraction or a
focus-delegation capability.

---

## 1. Context

### 1.1 What already works

Verified by reading `tui-mouse` @ `bd5b4ac` (v0.4.1) and by measuring autodb
v0.2.1 through a real browser on 2026-08-24:

- **Terminal backend**: mouse reporting is enabled — `term.go:207` writes
  `\x1b[?1002h\x1b[?1006h`, disabled again on teardown, suppressible with
  `WithoutMouse`; `decoder.go:420` decodes SGR reports; the capability probe
  treats mouse as tri-state on a verifiable DECRQM `?1006`.
- **Web backend** (ADR-0009): the client reports the full set. Measured frames:
  `{"t":"mouse","mk":"move"|"down"|"up"|"wheel",...}` carrying **cell**
  coordinates, decoded by `web/input.go:decodeMouse` into `MousePress`,
  `MouseRelease`, `MouseMotion`, `MouseWheel`.
- **Core routing** (ADR-0004 §2.5.2): `routing.go` hit-tests laid-out absolute
  rects topmost-first in reverse paint order, then walks target→root rewriting
  `X`/`Y` **local to each receiving node** at every hop, stopping at the first
  `HandleEvent` that returns true. `intake.go` coalesces consecutive
  `MouseMotion` so a drag cannot flood the queue.
- **Widgets that act on mouse today**: `list` (click to move the cursor,
  double-click inside `doubleClickWindow` to activate), `select`, `tree`,
  `tabs`, `split`, `float`, `bufferview`.

So the plumbing is not the gap. This ADR is about finishing the last mile.

### 1.2 What does not work, and the measurement that says so

`table.go`, `editor.go`, `textinput.go`, `textarea.go` and `box.go` contain no
reference to `Mouse` at all.

`Table` is the interesting case, because it is not simply unimplemented:

- `Table[T]` is "a List with column structure" — it holds `list *List[T]`,
  gives the header one row, and `PlaceChild(t.list, Rect{X:0, Y:1, …})`, so the
  inner list **is a placed child node** and should therefore be hit-tested
  directly, receiving coordinates already local to itself.
- `Table.HandleEvent` forwards *everything* to `t.list.HandleEvent`, including
  mouse events that arrive addressed to the Table.

Rev 0 called this ambiguous and stopped there. **Rev 1 measured it instead**, and
the behaviour is determinate — a `TestBackend` probe on the unmodified tree needs
no daemon and no populated grid, which is what rev 0 wrongly treated as a blocker:

| Press | Node reached | Result |
| --- | --- | --- |
| body, absolute `Y=3` | `List` (placed child at `Y:1`) | selects row 2 — **already correct** |
| header, absolute `Y=0` | `Table` | blind forwarding of `Y=0` → **selects row 0** |

So `Table` is not missing a coordinate system; it has **one unwanted delegation**.
The header press is the only defect, and it is a defect precisely because it looks
like it works. Recorded here because "unspecified" was a claim about my own
reading, not about the library.

**The consumer-visible state**, measured in autodb `--web-ui` (v0.2.1, real
daemon, headless Chromium): clicking the explorer pane and clicking the query
pane both leave the rendered screen **byte-identical**. The client demonstrably
sent `mk:"down"`. Nothing in the app responded. Two independent reasons, both
addressed below: the query pane is an `Editor` (no pointer handling at all), and
**no click anywhere focuses the thing it landed on**.

### 1.3 Why click-to-focus cannot be a per-widget feature

A widget receiving `MousePress` cannot distinguish "you were clicked in order to
be focused" from "you were clicked in order to act". `List` already shows the
tension: its click handler moves the cursor and its double-click activates —
correct once the list is the focused thing, and wrong as the *only* response
when the user's intent was to move focus into that pane first.

Worse, the widgets that most need click-to-focus are the ones with no reason to
implement a pointer handler otherwise: an `Editor` should not have to know about
the mouse merely to become focusable by it. So the rule belongs in the framework,
above the widget, next to the hit test that already knows which node was hit and
the focus machinery that already knows which nodes accept focus.

---

## 2. Decision

### 2.1 A press focuses the focusable node under the pointer, before delivery

On `MousePress` with `MouseLeft`, the App resolves a focus candidate and focuses
it, **then** delivers the event through the existing target→root bubble
unchanged.

**The algorithm, in order** (rev 1 makes each step explicit, because rev 0's
"walk toward the root honouring `FocusTarget()`" named a capability that does not
exist and left the trap rule to chance):

1. **Compute the active trapping scope FIRST**, before any candidate is chosen,
   using **`currentScope()`** — the same effective boundary keyboard traversal
   uses. **Not** `trapScopeOf(focused)`, which rev 3 specified and which is wrong
   in exactly one state: golib deliberately supports a *repair* condition where
   the focused child dies but its trap survives with no remaining focusables —
   `repairFocus` sets `focused = 0` and returns (`tui/focus.go:158-165`) while the
   scope stays on `scopeStack`. With `focused = 0` there is no node to walk from,
   so `trapScopeOf` yields nil, the algorithm would see *no trap*, and a click on
   an outside focusable could **escape a surviving trap** — violating criterion 4
   and leaving the scope stack inconsistent. `currentScope()` already handles this
   (`focus.go:113-123`: "no live focused node (repair path): fall back to the
   innermost surviving trap"). Root counts as unrestricted; a surviving non-root
   scope confines candidates.
2. Walk from the hit-test target toward the root for the first node whose
   component is `Focusable` and `AcceptsFocus()`.
3. **Reject the candidate if it is outside the active trapping scope**, and leave
   focus unchanged. This is a precondition of the focus call, not a consequence
   of it: `App.requestFocus` handles *entering* a scope but does not refuse
   *leaving* one, so a mouse path that simply called it could walk focus out of
   an open modal.
4. Focus the candidate, emitting the same lost/gained `FocusEvent` pair a
   keyboard focus change does.
5. **If focus handling unmounted the pointer target, SKIP this press.** Focus
   handlers run arbitrary component code synchronously (`tui/focus.go:54-66`) and
   may unmount the node the press was addressed to. When that happens the press
   is dropped: the focus change still stands, and the user's next click lands
   normally on the rebuilt tree.

   Two earlier revisions tried to preserve the press instead, and both were
   wrong — recorded here because the reason generalises. Rev 1 said "re-hit-test
   and deliver", which would hand a press to a *different* focusable widget while
   unfocused, breaking this ADR's own invariant. Rev 2 said "re-run steps 1–4 for
   the replacement", which **cannot execute**: `mount` creates nodes with
   `measured=false` / `placed=false` and only marks `layoutDirty`
   (`tui/tree.go:75-97`); `visible()` requires both flags (`tui/tree.go:40`);
   `hitTestNode` rejects anything not visible (`tui/routing.go:174`); and input
   dispatch returns before the queued wake runs layout (`tui/app.go:220-232`). So
   a synchronous retry can only reach a stale ancestor or nothing — never the
   replacement it was written for.

   Preserving the press would therefore require either deferring it until after
   the dirty layout completes (queued input carrying coordinates and a retry
   marker) or a synchronous relayout mid-dispatch — new framework behaviour, for a
   rare edge case, to save one click. **Not worth it.** Skip is representable
   today and cannot deliver to an unfocused or unmounted widget.
6. Deliver the press to the original target, which is still mounted.

**No focus-delegation capability is introduced.** Rev 0 promised to honour
`FocusTarget()`; that method is autodb-private (`autodb/tui/results.go:48`) and
has no counterpart in `golib/tui`'s capability set (`Focusable`, `Container`,
`CursorReporter`, `CursorShaper`, `FocusScope`). Adding one would be a new
externally-implemented interface — the expensive kind of change under
[[interface-evolution-capability-interfaces]] — for a case a consumer can already
express by making the delegate the focusable node. A container that wants focus
to land elsewhere should not report `AcceptsFocus()` and should let its focusable
child be found by step 2. If a real need for delegation appears later it lands as
its own optional capability with cycle, nil, mounted-descendant, visibility and
scope rules stated; it is out of scope here.

**Unchanged from rev 0, and approved:**

- **Only `MousePress`, only the primary button.** Motion, wheel and release never
  move focus, so the wheel scrolls the pane under the pointer without stealing
  keyboard focus. "Pane under pointer" and "focused pane" therefore diverge by
  design; any future keyboard-driven scroll must not assume they agree.
- **No focusable ancestor → focus unchanged**, and dead space never clears focus.
- **Focus first, delivery second**, so a widget handling the press already sees
  itself focused and one gesture both focuses and acts.

### 2.2 `Table`: `List` keeps body presses; `Table` refuses the header

Rev 1 takes the ownership target-first routing actually permits, which is
lector's option (1). The alternative — restructuring layout/routing so `Table`
becomes the pointer target and owns all translation — would mean introducing
capture or retargeting and **amending ADR-0004**, for no behavioural gain over
this. Rejected on that basis (§5).

Measured on the unmodified tree, so the design starts from what is rather than
what was assumed:

| Press | Node reached | Today | Rev 1 |
| --- | --- | --- | --- |
| body, absolute `Y=3` | `List` (child at `Y:1`) | selects row 2 — **correct** | unchanged |
| header, absolute `Y=0` | `Table` | forwards `Y=0` → **selects row 0** | **inert** |

- **`List` owns body presses, double-click and wheel over the body.** No change
  is required and none is made: it already receives correct local coordinates
  because it is a placed child, and it already implements click-to-move-cursor
  and the `doubleClickWindow` activation. `Table` adds nothing here.
- **`Table` stops forwarding pointer events.** `Table.HandleEvent` forwards
  everything to its list so that a controller can hand it navigation keys; that
  forwarding stays for keys and **stops for `MouseEvent`**. A header press is
  inert: it selects nothing and changes no state. The header remains reserved
  for column sorting, which is not in this ADR.
- **Wheel over the header scrolls the body.** The header is part of the same
  scrollable surface as far as a user is concerned, and a dead strip at the top
  of a grid is a worse surprise than a scroll. `Table` forwards only
  `MouseWheel` to its list, and only wheel.

This is a smaller change than rev 0 proposed and a more honest one: the bug is
one unwanted delegation, not a missing coordinate system.

### 2.3 `Editor`: a press is a named command-state boundary

Rev 0's "press places the caret" was under-specified: `Editor` carries
Normal/Insert/Visual mode, a count prefix, a pending operator, a visual anchor,
a pending insert escape-chord rune, and an open undo group
(`tui/widget/editor.go:18-50`). A click during `2d`, during Visual, or after the
first rune of `jk` had several defensible meanings, which means code would have
picked one by accident.

**A primary press is a command boundary.** Concretely:

- **Pending COMMAND state is discarded; a pending insert RUNE is settled.** The
  two are not the same thing and rev 1 wrongly lumped them together:
  - A **count** and a **pending operator** are abandoned. Completing `2d` against
    a clicked location would turn a mis-click into a destructive edit, and the
    pointer carries no evidence the user meant the operator to apply there.
    Discarding them modifies no text.
  - A **pending insert escape-chord rune is SETTLED at the OLD caret** before the
    caret moves. That rune is **input the user typed**, and accepted ADR-0008
    binds every non-chord input to settle it first — paste, focus loss,
    `SetValue` and mode transitions all commit it (`golib-tui-0008` §pending
    rune; `editor.go:375 settlePendingRune`). Rev 1 said "discard", which was
    silent data loss *and* an unannounced reversal of an accepted ADR. A click is
    a non-chord input like any other: settle, then move. ADR-0008 stands
    unamended.
- **Normal mode**: caret moves; mode unchanged.
- **Insert mode**: stays in Insert, caret moves, and the press **closes the
  current undo group**. A click is a deliberate discontinuity, so the text typed
  before it and after it must undo separately.
- **Visual / Visual-line**: the press **leaves Visual** and clears the anchor.
  Extending a selection by clicking is drag-selection, which is deferred (below);
  silently keeping the anchor would make the next motion extend a selection the
  user believes they dismissed.
- **The press modifies buffer text in exactly one case:** committing a pending
  insert rune, as above. That is not the click editing the buffer — it is the
  click settling input the user had already typed, at the caret where they typed
  it, per ADR-0008. Beyond that single settlement a press never inserts, deletes
  or replaces text.

**Inverse viewport mapping** must be complete before implementation, because
this is where an off-by-one is invisible:

- **`WrapNone`**: buffer line = `top + localY`; column resolved from `left` plus
  the clicked cell, by accumulating cell width so a wide grapheme resolves to the
  cell it **starts** at (ADR-0003).
- **`WrapSoft`**: one logical line spans several visual rows, so the row→line
  inverse walks the same wrap computation the renderer used
  (`editor.go:1131-1208`) rather than dividing by width.
- **Past end of line** → clamp to the last column (end-of-line in Insert, last
  grapheme in Normal). **Below the last painted line** → clamp to the last buffer
  line. **The scroll-indicator column** is not text: a press there is inert.
- **Wheel** scrolls the viewport without moving the caret. **One `MouseEvent` is
  one step** — the event carries a wheel *direction* (`WheelUp`/`WheelDown`) and
  no magnitude, so a producer sending N notches sends N events; the consumer never
  multiplies. Clamped at both ends.
- **One step is one LOGICAL LINE, under both wrap modes.** Rev 1 specified one
  *visual row* under `WrapSoft`; that is **not representable** by the current
  viewport, which stores a logical-line origin only (`editor.go:23-27` has `top`
  and `left`, no intra-line wrapped-row offset) and begins rendering, `Cursor`
  and `ensureVisible` at logical line `top`. A logical line wrapping to five
  visual rows therefore cannot be scrolled by one row while remaining the first
  line. Introducing a visual-row origin would mean new viewport state plus new
  invariants across **five** paths — render, `Cursor`, `ensureVisible`, click
  inversion and end clamping — which is a larger change than a wheel nicety
  justifies, and it is deferred as its own ADR. Logical-line stepping is
  representable today and correct in both modes.
  - Click inversion is unaffected: it maps a *rendered* row back through the same
    wrap computation the renderer used, which needs no new viewport state.

**`TextInput` and `TextArea` are DEFERRED from rev 0** rather than carried
un-specified: they were named in rev 0's decision but absent from its acceptance
criteria, which is exactly the gap this ADR is trying to close. They get their
own rev once `Editor`'s mapping is proven.

**Drag-selection remains deferred**, not refused. It needs a selection model plus
press/motion/release state, and caret placement is what unblocks a browser
frontend.

### 2.4 No general "Clickable" interface

Rejected deliberately. `AcceptsFocus()` plus `HandleEvent(MouseEvent)` already
express everything the framework needs, and per
`interface-evolution-capability-interfaces` a new externally-implemented
interface is the expensive kind of change — it would have to be probed
everywhere and would make every existing widget's mouse handling
retro-inconsistent. §2.1 needs only the focus predicate that already exists.

---

### 2.5 Double-click is activation, synthesised once at the event boundary

Johno's request is "double click = ENTER". ENTER is not one behaviour, though — it
means whatever the focused widget makes it mean. So the primitive is
**activation**, and each widget keeps its own answer.

**`MouseEvent` gains `Count int`.** It is the press ordinal: 1 for a single press,
2 for the second press of a double, 3 for a triple. It is **0 on every non-press
kind**, so nothing can read a count off motion, wheel, or release and believe it.

**Synthesised in `dispatch`, not in the producers.** Criterion 18's three-layer
rule decides this: a click *count* is not decode shape, it is behaviour derived
from timing and position, so it belongs at the one point every producer's events
already flow through — the `MouseEvent` case in `App.dispatch`.

**Committed immediately before DELIVERY, not on arrival** (lector r1 finding 1).
Rev 6 stamped the ordinal before hit-testing and before the focus step, which
meant a press the focus step then *skipped* still advanced the run: the widget
that replaced an unmounted target saw `Count == 2` as its first ever delivered
press, and `Count` drives activation. A press nobody received must not count. So
the order is hit-test → focus → **commit** → deliver.

**Continuity includes the DELIVERED TARGET**, not just button, cell and window: a
press landing on a different node is a different gesture even at identical
coordinates, which is exactly what happens when the tree under the pointer
changes between clicks.

**Non-press kinds are canonicalised to zero**, not merely left alone (finding 4).
Rewriting only presses let a producer hand a wheel event a count that was
delivered verbatim, contradicting the promise above. `tui/term/decoder.go` and `tui/web/input.go` are
**unchanged**; a third producer would inherit the behaviour for free, which is the
property the layered rule exists to protect.

**The widget owns LOGICAL identity; the App cannot** (finding 2). An absolute
cell is not a row — and a row index is not durable either: it names a row only
within one **source epoch**, so replacing or refreshing a `List`'s source ends the
pairing (lector r2). Scroll a `List` between the two presses and the same cell
addresses different data — `List`'s own previous detection compared logical rows
and refused that pair, so keying activation on the cell alone silently activated
the wrong row. `List` therefore also requires the same logical row, and `Tree` the
same node (by pointer, since a host may render one id under two parents). The
split is the point: the App knows timing, button, cell and target; only the widget
knows what those address.

**Activation is exactly `Count == 2`** (finding 3). `>= 2` made a triple-click
activate twice, on the 2nd and 3rd press, while "double-click = ENTER" means one
activation per pair.

**The `Tree` expander is not an activation target.** Its press already toggles, so
activating there as well would make one gesture both change expansion and
activate. A double-click on the expander toggles twice and the node ends as it
started.

**Same button, same cell, inside the window.** The App remembers the last press
(button, x, y, time, count). A press increments the count only when all three hold;
otherwise the count restarts at 1.

- **Same cell, not "near".** Terminal cells are coarse and a row is one cell tall,
  so a one-cell drift is a *different row*. Tolerating it would activate the row
  the user did not click, which is worse than requiring a steady hand.
- **Window is configurable** — `WithDoubleClickWindow(d)`, default **400ms**. The
  option exists for tests as much as for taste: a large window makes the positive
  case deterministic without a clock seam, and `1ns` makes the negative case
  deterministic for the same reason. Adding an injectable clock to `App` for this
  would be a larger, more invasive seam than the behaviour warrants.
- A release between the two presses is normal and does **not** reset the count; a
  press of a *different* button does.

**`Tree` publishes `ActivateEvent` on a double-click, and does NOT expand.** The
Tree already publishes `ActivateEvent{Owner, Index}` when ENTER lands on a leaf, so
double-click reuses that channel rather than inventing one — a host that already
handles activation gets double-click support without changing.

It fires for **branches as well as leaves**, and the Tree deliberately does not
also toggle expansion, because the Tree **cannot know** whether a branch is
activatable: autodb's `tbl:` nodes are branches whose children are columns, and
whose activation is a query scaffold rather than an expansion. A widget that
guessed here would scaffold *and* expand. Hosts that want folder-opens-on-double-
click implement it in their own handler, where the node grammar is known.

The first press of the double has already selected the row through the existing
single-press path, so activation always targets the row under the pointer.

## 3. Consequences

**Good.** Click-to-focus is one rule in one place, so every widget — including
the four with no pointer code — becomes reachable by pointer at once. Consumers
get it by upgrading rather than by implementing anything, which is the property
that made `tui.Backend` worth keeping (autodb ADR-0064 §2.1).

**Costs and risks.**

- §2.1 changes focus behaviour for every existing consumer. A click that
  previously only acted now also focuses. That is the intended behaviour, and it
  is still a behaviour change that belongs in a minor version with a release
  note, not a patch.
- §2.2 corrects a case that currently "works" by accident in some hit paths;
  anyone who compensated for the header offset in their own code will find the
  compensation now doubles. The header-inert rule and the offset test exist to
  make that discoverable rather than silent.
- Wheel-without-focus (§2.1.2) is a deliberate asymmetry. It is the right one,
  but it means "the pane under the pointer" and "the focused pane" can differ,
  and any future keyboard-driven scroll must not assume they are the same.

---

## 4. Acceptance criteria

**Focus (§2.1)**

1. A left press on an unfocused focusable widget focuses it, and the widget also
   receives the press (one gesture, both effects).
2. Motion, wheel and release never change focus.
3. A press with no focusable ancestor leaves focus unchanged — it does not clear
   it.
4. A press outside an active trapping scope **does not move focus**, proven with a
   **custom partial-size `FocusScope`** — not only `widget.Float`, whose full-area
   backdrop would let a broken guard pass by accident.
4b. **The repair state is covered:** enter a partial trap, remove or disable its
   last focusable **without unmounting the scope**, confirm focus is empty
   (`focused = 0`), then click an **outside** focusable and prove focus stays
   empty/confined. This is the case a `trapScopeOf(focused)` implementation passes
   every other test while failing.
5. `FocusEvent` pairs fire for a mouse-driven focus change exactly as for a
   keyboard-driven one.
6. If a `FocusEvent` handler **unmounts and mounts a replacement** for the
   pointer target, the press is **not delivered** — proven with a handler that
   performs a real unmount+mount, so stale ancestor geometry cannot make the test
   pass by accident. The focus change itself still stands, and no press ever
   reaches an unmounted or unfocused component.

**Table (§2.2)**

7. A body press at a **non-zero scroll offset** selects the row the keyboard would
   call current at that visual position.
8. A header press **from a pinned non-zero selection** leaves the selection
   unchanged — the regression the probe caught (it selected row 0).
9. A body double-click activates, using `List`'s existing window.
10. Wheel over the **body** scrolls; wheel over the **header** also scrolls the
    body; neither moves the selected row.

**Editor (§2.3)**

11. A press in Normal mode with a pending count and pending operator (`2d`)
    discards both, moves the caret, and modifies no text.
12. A press mid insert-chord (first rune of `jk`) **settles that rune at the old
    caret** — the character survives in the buffer — then moves the caret and
    stays in Insert. Nothing typed is lost, per ADR-0008.
13. A press in Insert mode closes the undo group: text typed before and after the
    click undo separately.
14. A press in Visual and in Visual-line leaves Visual and clears the anchor.
15. Caret placement is correct at non-zero horizontal **and** vertical scroll,
    under `WrapNone` **and** `WrapSoft`, and across a wide grapheme (resolving to
    the cell the grapheme starts at).
16. A press past end-of-line clamps to the last column; below the last line clamps
    to the last line; on the scroll-indicator column is inert.
17. Wheel scrolls without moving the caret — **one logical line per event in both
    wrap modes**, clamped at both ends, with N notches arriving as N events.

**Both backends, and the standard**

18. The contract is proven against **both real producers**, in three layers.
    Rev 5 restates this: rev 4 said "every behaviour above is exercised through
    `TestBackend` injection **and** through `web/input.go`'s decoder", which was
    both unachievable-by-intent and **incoherent** — it called `TestBackend`
    injection "the terminal path", and `TestBackend` bypasses `tui/term`'s
    decoder entirely, so as written it never covered the terminal producer at
    all. The layers that matter:
    - **(a) Decode coverage, exhaustive, per producer, by event shape.**
      `web/input_test.go` for `MouseReport` → `tui.Event`;
      `term/decoder_test.go` for SGR bytes → `tui.Event`. This is where
      producer-specific failure modes live: wrong button, wrong coordinate
      base, wrong kind.
    - **(b) Behaviour coverage, exhaustive, via `TestBackend` injection.**
      Producer-agnostic on purpose: both decoders emit ordinary `tui.Event`
      values, so a widget cannot tell them apart. Duplicating this matrix per
      producer multiplies tests without adding a failure mode.
    - **(c) A behavioural integration slice per real producer** — press selects
      the clicked row, press focuses the clicked widget, N wheel events are N
      steps — driven from real `MouseReport`s (`tui/web/input_app_test.go`) and
      real SGR bytes (`tui/term/input_app_test.go`) through a live `App` and
      real widgets. This is what joins (a) to (b): it proves a decoder's output
      actually reaches the behaviour, rather than merely having the right shape.

    A behaviour is therefore covered once in (b) and reachable-from-each-producer
    by (c), instead of once per producer.
**Double-click (§2.5)**

19. Two left presses on the **same cell** inside the window yield `Count` 1 then 2.
20. The **same-cell** rule: two presses inside the window one cell apart yield
    `Count` 1 and 1. Deterministic without a clock.
21. The **window** rule: with `WithDoubleClickWindow(1ns)`, two immediate presses
    yield `Count` 1 and 1.
22. A press of a **different button** inside the window restarts the count.
23. An intervening **release** does not reset the count; an intervening press
    elsewhere does.
24. `Count` is **0** on motion, wheel and release — asserted, so no consumer can
    read a count that was never computed.
25. Synthesis happens **immediately before delivery** — after hit-testing and
    after the §2.1 focus step — so a double-click that also moves focus still
    carries `Count == 2`, **and** a press the focus step skips does not advance the
    run. Negative control: commit on arrival and the skipped-press test fails.
25a. A press delivered to **nobody** (focus handling unmounted the target) leaves
    the run untouched: the replacement occupying that same cell sees `Count == 1`
    on its first delivered press.
25b. Non-press kinds are delivered with `Count == 0` even when **seeded** nonzero.
25c. `List` does not activate when the two presses land on the same cell but
    different **logical rows** — a scroll, a source **replacement**, or an
    in-place **refresh**. An index names a row only within one **source epoch**,
    and an epoch ends at every **NOTIFIED** change: `SetSource`, `SetItems`,
    `RefreshSource`.

    "Every source change" was unimplementable as written (lector r3). A
    `ListSource` exposes only `Len`/`Item`, and `SliceSource` aliases the caller's
    slice, so a caller can mutate a row on the loop without invoking any `List`
    method — `List` cannot observe it and cannot defend against it. The contract
    is therefore normative on the source side: an in-place change MUST be followed
    by `RefreshSource` on the loop before anything else observes it, and an
    unnotified mutation is a **caller violation**, not a widget defect.

    Paired positive: with the source untouched the same gesture still activates —
    without it, clearing unconditionally would satisfy every negative control and
    break double-click entirely.
25d. `Tree` does not activate when the two presses land on the same cell but
    different **nodes**.
25e. A triple-click activates **once**, in both `List` and `Tree`.
25f. A double-click on the `Tree` **expander** activates nothing and leaves the
    node's expanded state unchanged.
26. **`Tree`**: a double-click publishes `ActivateEvent` for the row under the
    pointer — for a **branch** as well as a leaf — and does **not** change its
    expanded state. Negative control: expand on double-click and this fails.
27. A **single** click publishes no `ActivateEvent`.
28. Both producers are covered by a behavioural bridge per criterion 18: a real SGR
    byte sequence through `term`, and a real message through `web`, each yielding
    `Count == 2` without either producer computing it.
29. Every test **fails without its fix** — the negative-control standard from
    autodb v0.2.1's regression suite.

`TextInput` and `TextArea` are deliberately absent: §2.3 defers them, so they
carry no criteria here rather than carrying unstated ones.

## 5. Rejected alternatives

- **Implement mouse handling per widget, with no framework rule.** Rejected per
  §1.3: click-to-focus is not expressible inside a widget, and requiring an
  `Editor` to grow a pointer handler in order to be clickable is the wrong
  shape.
- **Leave `Table` delegating to its inner list.** Rejected: routing is
  **determinate**, and that is precisely the problem — blind delegation makes the
  determinate header path *wrong*. A header press reaches `Table`, which forwards
  Table-local `Y=0` to its list and selects row 0. It is not unspecified; it is
  specified and incorrect, which is worse than absent because it looks like it
  works.
- **Ship drag-selection in rev 0.** Deferred (§2.3), not refused. It needs a
  selection model, and caret placement is the part that unblocks a browser
  frontend.
- **A `Clickable` capability interface.** Rejected in §2.4.
- **Restructuring layout/routing so `Table` is the pointer target** and owns all
  coordinate translation (lector's option 2). Rejected: it requires capture or
  retargeting and therefore an **amendment to ADR-0004**'s target-first,
  no-capture contract, and it buys nothing over §2.2 — body presses already land
  correctly, so the only thing needing a decision is the header, which one
  `MouseEvent` guard settles.
- **A focus-delegation capability** (`FocusTarget` promoted into golib).
  Rejected for rev 1 in §2.1: it is a new externally-implemented interface for a
  case a consumer can express by not accepting focus on the container, and it
  would need cycle/nil/mounted/visibility/scope rules that nothing yet demands.
  Available later as its own optional capability if a real need appears.
