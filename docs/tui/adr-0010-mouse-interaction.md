# ADR-0010 — `golib/tui`: mouse interaction (click-to-focus, Table, Editor)

- **Status:** **Proposed (rev 2)** (2026-08-24, authored by jarvis at Johno's
  request). Companion to autodb ADR-0064 §2.2, which is the consumer that asked
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
  - Rev 1 closed r1's Table-ownership finding outright; r2 also caught stale
    prose in §5 reviving the disproven "unspecified" framing, corrected here.
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

1. **Compute the active trapping scope FIRST**, before any candidate is chosen —
   `trapScopeOf` of the currently focused node.
2. Walk from the hit-test target toward the root for the first node whose
   component is `Focusable` and `AcceptsFocus()`.
3. **Reject the candidate if it is outside the active trapping scope**, and leave
   focus unchanged. This is a precondition of the focus call, not a consequence
   of it: `App.requestFocus` handles *entering* a scope but does not refuse
   *leaving* one, so a mouse path that simply called it could walk focus out of
   an open modal.
4. Focus the candidate, emitting the same lost/gained `FocusEvent` pair a
   keyboard focus change does.
5. **Revalidate the pointer target before delivery, and re-run the whole rule if
   it changed.** Focus handlers run arbitrary component code synchronously
   (`tui/focus.go:54-66`) and may unmount the node the press was addressed to.
   Rev 1 said "re-hit-test once and deliver", which **broke this ADR's own
   invariant**: the replacement could be a different focusable widget receiving a
   press while unfocused. So: if the original target is no longer mounted,
   re-hit-test **and re-run steps 1–4 for the replacement** (scope recomputed,
   candidate re-chosen, focus applied). **Exactly one retry.** If that second
   focus change mutates the tree again, **skip delivery** — an unbounded
   focus/unmount loop is worse than a lost click, and a click that lands on an
   unfocused widget violates the guarantee in criterion 1.
6. Deliver the press — to a target that is mounted and whose focus candidate has
   been focused.

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
- The press never itself modifies buffer text.

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
5. `FocusEvent` pairs fire for a mouse-driven focus change exactly as for a
   keyboard-driven one.
6. If focus handling unmounts the original pointer target, the retry re-runs
   candidate/scope/focus resolution so the **replacement is focused before it
   receives the press**; and if that retry mutates the tree again, the press is
   **not delivered**. Never delivered to an unmounted component.

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

18. Every behaviour above is exercised through `TestBackend` injection **and**
    through `web/input.go`'s decoder, so the contract is proven against both
    producers rather than one.
19. Every test **fails without its fix** — the negative-control standard from
    autodb v0.2.1's regression suite.

`TextInput` and `TextArea` are deliberately absent: §2.3 defers them, so they
carry no criteria here rather than carrying unstated ones.

## 5. Rejected alternatives

- **Implement mouse handling per widget, with no framework rule.** Rejected per
  §1.3: click-to-focus is not expressible inside a widget, and requiring an
  `Editor` to grow a pointer handler in order to be clickable is the wrong
  shape.
- **Leave `Table` delegating to its inner list.** Rejected: the behaviour is
  currently determined by which node the hit test happens to reach, and the
  header offset is unaccounted. Unspecified is worse than absent, because it
  looks like it works.
- **Ship drag-selection in rev 0.** Deferred (§2.3), not refused. It needs a
  selection model, and caret placement is the part that unblocks a browser
  frontend.
- **A `Clickable` capability interface.** Rejected in §2.4.
- **Restructuring layout/routing so `Table` is the pointer target** and owns all
  coordinate translation (lector's option 2). Rejected: it requires capture or
  retargeting and therefore an **amendment to ADR-0004**'s target-first,
  no-capture contract, and it buys nothing over §2.2 — body presses already land
  correctly, so the only thing needing a decision is the header, which one
  `MouseEvent` guard settles. (Rev 1's wording here still called the behaviour
  "determined by which node the hit test happens to reach" and "unspecified".
  That was the disproven framing: routing is **determinate**, and the defect is
  that `Table`'s blind delegation makes the determinate header path wrong.)
- **A focus-delegation capability** (`FocusTarget` promoted into golib).
  Rejected for rev 1 in §2.1: it is a new externally-implemented interface for a
  case a consumer can express by not accepting focus on the container, and it
  would need cycle/nil/mounted/visibility/scope rules that nothing yet demands.
  Available later as its own optional capability if a real need appears.
