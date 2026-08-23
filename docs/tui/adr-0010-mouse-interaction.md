# ADR-0010 — `golib/tui`: mouse interaction (click-to-focus, Table, Editor)

- **Status:** **Proposed (rev 0)** (2026-08-24, authored by jarvis at Johno's
  request). Companion to autodb ADR-0064 §2.2, which is the consumer that asked
  for this and which decided *not* to build a webapp instead.
- **Date:** 2026-08-24
- **Module:** `github.com/yongjohnlee80/golib` (`tui`, `tui/widget`)
- **Supersedes:** nothing. **Completes** ADR-0004 §2.5.2 (mouse routing) and
  ADR-0007 (the widget set) for pointer input.
- **Related:** ADR-0002 (SGR mouse in the terminal backend), ADR-0004
  (hit-testing and routing), ADR-0007 (widget contracts), ADR-0009 (the web
  Backend, which reports mouse in cell coordinates).

**Abstract:** `golib/tui` transports and routes mouse events end to end, and
seven widgets already act on them — but the two that matter most for an
application do not, and **nothing focuses a widget when you click it**. The
result, measured in autodb's browser frontend, is that clicking anywhere does
nothing observable. This ADR adds one framework rule (a click focuses the
focusable node under the pointer, before the widget sees the event) and pointer
contracts for `Table` and `Editor`, and it declines to invent a general
"clickable" abstraction.

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

Which means part of table clicking may already work through delegation, and
part may be off by the header row depending on which node the hit test lands on.
**That ambiguity is itself the finding**: the behaviour is unspecified, untested,
and differs by which node the pointer happens to hit. An ADR that guessed here
would be guessing about its own library.

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

On `MousePress` with `MouseLeft`, the App walks from the hit-test target toward
the root for the first node whose component reports `AcceptsFocus()` (honouring
`FocusTarget()` where a container redirects focus, as `resultsPanel` does), and
focuses it — **then** delivers the event through the existing target→root bubble
unchanged.

Properties this must hold:

1. **Focus first, delivery second, always in that order.** A widget handling the
   press then sees a consistent world: it is already the focused node, so a
   click that both focuses and acts (click a list row in an unfocused pane) does
   the intended thing in one gesture rather than requiring two clicks.
2. **Only `MousePress`, only the primary button.** Motion, wheel and release
   never move focus — a wheel over an unfocused pane scrolls that pane without
   stealing focus, which is what every editor does and what makes reading a
   result grid while typing possible.
3. **No focusable ancestor → focus unchanged.** Clicking dead space is not a
   focus event, and must not clear focus: a modal must not lose focus because
   the user clicked its border.
4. **Focus scopes are respected.** A press inside an open modal scope may focus
   only within that scope (ADR-0004 focus traps); a press outside it is not a
   way to escape the trap. This is the invariant that autodb's About-splash
   deadlock (autodb v0.2.1) proved matters: focus leaving an open modal makes
   the modal unclosable.
5. **`FocusEvent`s fire exactly as keyboard focus changes do**, so components
   whose styling tracks focus (`FocusWithin` consumers) need no new code.

### 2.2 `Table`: an explicit pointer contract, replacing delegation-by-accident

`Table` stops forwarding mouse events blindly and states its own behaviour:

- **A press in the row area** selects the row under the pointer. `Table` is
  responsible for the header offset — one row — so a press is translated into
  the inner list's row space regardless of which node the hit test reached.
- **A press on the header row** does not select a row. It is reserved for column
  sorting (not in this ADR) and must be inert rather than accidentally selecting
  row 0, which is the concrete failure mode delegation produces today.
- **Wheel** scrolls the rows, and does not move the cursor. Scrolling and
  selection are different intents; conflating them makes a result grid unreadable
  while a query is being edited.
- **Double-click in the row area** activates, matching `List`'s existing
  `doubleClickWindow` semantics rather than inventing a second timing rule.

The keyboard path is untouched, and the scroll-offset and wide-grapheme
invariants the keyboard path already honours apply identically: a press must
resolve to the same row the keyboard would call current after an equivalent
move. A click that computes the wrong row is worse than a click that does
nothing, so the tests below pin the offset explicitly.

### 2.3 `Editor`: press places the cursor, wheel scrolls

- **Press** places the caret at the clicked cell, clamped to the end of the
  clicked line and to the buffer. Wide graphemes resolve to the cell the
  grapheme *starts* at, consistent with ADR-0003's cell invariants.
- **Wheel** scrolls the viewport without moving the caret.
- **Drag-select is out of scope** for rev 0. It needs a selection model on the
  editor and press/motion/release state, and it is separable — placing a caret
  is the thing that makes a browser frontend usable, and selection can land
  later without redesign.

`TextInput` and `TextArea` get press-to-place-caret on the same rule. `Box`
remains inert: it is a frame, and §2.1 already makes a click on its interior
focus whatever focusable node lives inside it.

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

1. A left press on an unfocused focusable widget focuses it, and the widget also
   receives the press (one gesture, both effects).
2. Motion, wheel, and release never change focus.
3. A press with no focusable ancestor leaves focus unchanged — it does not clear
   it.
4. A press outside an open modal scope cannot move focus out of that scope.
5. `FocusEvent` pairs (lost/gained) fire for a mouse-driven focus change exactly
   as for a keyboard-driven one.
6. `Table`: a press on row *n* of the visible rows selects the row the keyboard
   would call current after moving to that visual position — **tested at a
   non-zero scroll offset**, which is where an offset error hides.
7. `Table`: a press on the header row selects nothing and changes no state.
8. `Table`: wheel scrolls and leaves the selected row unchanged.
9. `Table`: double-click inside the row area activates, using `List`'s existing
   window.
10. `Editor`: a press places the caret at the clicked cell, clamped at
    end-of-line; verified across a wide grapheme.
11. `Editor`: wheel scrolls without moving the caret.
12. Both backends are exercised: the terminal path through `TestBackend`
    injection, and the web path through `web/input.go`'s decoder, so the
    contract is proven against the two producers rather than one.
13. Every test **fails without its fix** — the negative-control standard from
    autodb v0.2.1's regression suite, which is what made that fix trustworthy.

---

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
