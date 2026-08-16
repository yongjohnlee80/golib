# ADR-0008 — `golib/tui`: vim-modal Editor, Tree, and pane zoom

- **Status:** Proposed (2026-08-16; authored by ultron-prime for autodb M6 —
  implementation on the `tui-m6` branch. Requirement basis: Johno's M6 TUI
  directive, 2026-08-16 — a sqlit-style DB IDE whose query pane "behaves
  like a vim editor", with widget maximize and a leader-key command menu.
  The reusable widgets land here upstream-first; autodb consumes the
  release — same split as ADR-0006→server/rpc.)
- **Date:** 2026-08-16
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** none (additive; discharges ADR-0007 §2.7 deferred
  follow-up #3 "Tree", and adds two small exported seams to `widget.Split`)
- **Related:** ADR-0004 (component tree/layout — the Flutter box protocol
  every widget obeys), ADR-0005 (events/focus/bubbling — the reason the
  leader key can live in the application), ADR-0007 (standard widget set —
  TextArea/List/Float are the substrates), autodb ADR-0057 (the consumer)

> **Self-containment contract.** Concrete signatures, behavioral rules,
> files, and acceptance criteria — an implementer needs no other context.

---

## 1. Context

autodb M6 needs three things golib/tui does not have (survey 2026-08-16):

- A **multi-line editor with vim modal behavior**. `widget.TextArea`
  already owns the hard substrate — grapheme-cluster addressing
  (`taPos{ln,col}`), atomic `insert`/`deleteRegion`, `moveTo(ln,col,extend)`
  char-wise selection, sticky-column `vertical()`, wrap, viewport
  scrolling, `CursorReporter`. What it lacks is exactly the modal layer: a
  mode state machine, a data-driven keymap, count/operator pending state,
  line-wise operations, a yank register, undo, and an escape chord.
- A **Tree** widget (ADR-0007's deferred follow-up #3): expandable
  hierarchy with lazy children — a DB explorer cannot enumerate a
  connection's schema until the node is opened.
- **Pane zoom**: `widget.Split` has no exported `SetRatio`, no maximize.
  A component mounts at most once (ADR-0004), so "maximize" cannot be
  implemented by remounting a pane elsewhere — the ancestors must
  collapse. The primitive belongs on Split.

House rules that bind the design: capability interfaces over interface
growth; options validated at construction (panic on misuse); events over
callbacks; widgets read style through tokens + `Inherit`; `Base.measure`
for geometry, `Surface.StringWidth` for paint; keys bubble unconsumed to
the application root (ADR-0005) — which is precisely how the leader key
stays an application concern.

## 2. Decision

### 2.1 `widget.Editor` — vim-modal multi-line editor

A NEW widget embedding the TextArea buffer machinery (the buffer/motion
internals are extracted into an unexported `textBuffer` shared by TextArea
and Editor — TextArea's public API and behavior are unchanged).

```go
type EditorMode uint8
const (
	ModeNormal EditorMode = iota
	ModeInsert
	ModeVisual      // char-wise
	ModeVisualLine  // line-wise (V)
)

func NewEditor(opts ...EditorOption) *Editor
// Options: WithEditorStyles(TextInputStyles), WithWrap(WrapMode),
// WithInitialText(string), WithEscapeChord(string),   // default "jk"
// WithKeymap(Keymap)                                   // overlay onto the default table

func (e *Editor) Value() string
func (e *Editor) SetValue(string)
func (e *Editor) Mode() EditorMode
func (e *Editor) SelectedText() string   // "" outside visual modes
func (e *Editor) Line() (row, col int)   // cursor position for status bars
func (e *Editor) AcceptsFocus() bool
func (e *Editor) Cursor() (x, y int, ok bool)

// ModeChangedEvent publishes every mode transition (status-bar "-- INSERT --").
type ModeChangedEvent struct { Owner tui.NodeID; Mode EditorMode }
```

**Cursor & range model (r2 — the authoritative invariants):**

- The substrate is an insertion-point buffer (`taPos{ln,col}`, col in
  `[0, len(line)]`). **Normal/Visual-mode cursor invariant:** the cursor
  denotes the grapheme AT insertion point `col`, so col is clamped to
  `[0, max(0, len(line)-1)]`; on an empty line the cursor sits at col 0
  denoting no grapheme. Insert mode uses the raw insertion point
  `[0, len(line)]`.
- **Insert→Normal** (Esc/chord): cursor moves one cluster left (vim
  behavior), clamped to 0.
- **Visual char-wise ranges are INCLUSIVE at both endpoints**; operations
  resolve anchor/cursor into `[lo, hi]` positions and act on
  `[lo, hi+1)` in insertion-point terms. Visual-line ranges cover whole
  lines including the trailing newline (line-wise register content).
- `$` moves to the LAST grapheme of the line (Normal) / end insertion
  point (Insert has no `$`); `0` moves to col 0. `x` deletes the grapheme
  under the cursor (no-op on an empty line); after deletion the cursor
  clamps to the new last grapheme. `p` pastes char-wise text AFTER the
  cursor grapheme, `P` before; a line-wise register pastes as new
  line(s) below/above, cursor to first pasted line.
- **Counts:** digits `1-9` (then `0-9`) accumulate a count in Normal and
  Visual modes; `0` with no pending count is the motion. Count applies
  to motions (`3w`, `2}`) in both modes, and to `x`, `dd`, `yy` in
  NORMAL mode only (r3): Visual `d`/`y`/`x` consume the selection and
  ignore any pending count. `o`/`O` are not count-able in v1. Count caps
  at 10⁶ (further digits ignored); Esc clears pending count.
- **No general operator grammar in v1** (r2 — the operator-pending claim
  is withdrawn). The command set is exactly: atomic commands, motions,
  and three DOUBLE-KEY commands (`dd`, `yy`, `gg`) implemented with a
  one-key pending buffer: after `d`/`y`/`g`, only the matching second key
  completes the command; ANY other key clears the pending state and is
  then processed normally (so `d` `w` moves the cursor by a word —
  `dw`/`d$`/`c`/`.` etc. are deliberately absent; ranged edits go through
  Visual mode: `vwd`). The pending buffer is cleared by mode changes,
  focus loss, and SetValue.

**Default keymap (v1 command set — the consumer's explicit list plus the
minimum companions that make it a usable vim; anything absent is absent
deliberately):**

- Motions (Normal + Visual, count-prefixable): `h j k l`, `0 $`,
  `w b e`, `{ }` paragraph back/forward, `[ ]` aliases of `{ }` in v1,
  `gg G`, arrows/Home/End/PgUp/PgDn as their obvious equivalents.
- Entering Insert: `i a o O A I` (`o/O` open lines below/above; `a`
  inserts after the cursor grapheme; `A`/`I` end/start of line).
- Insert mode: text/Paste inserts via the substrate; `Esc` OR the escape
  chord returns to Normal.
- Edits (Normal): `x`, `dd`, `D` (to end of line), `yy`, `p P`, `u`
  undo, `Ctrl-R` redo.
- Visual: `v` char-wise, `V` line-wise; motions extend; `y` yank,
  `d`/`x` delete, `Esc`/chord exits to Normal.
- **Not consumed in Normal/Visual mode:** Space, Tab, and every unbound
  key — they bubble (ADR-0005), so the application's leader menu and
  focus traversal work without editor cooperation. Insert mode consumes
  text keys (Tab inserts, matching TextArea's veto-mask rules).

**Escape chord (r2 — dispatch-order contract, not wall-clock):** the App
selects over input and timer channels, so "within 300 ms" is not an
observable arrival guarantee. The contract is DISPATCH-ordered: on the
first chord rune in Insert mode, the Editor holds it pending and requests
a one-shot tick addressed to itself; the chord completes iff the second
chord rune is DISPATCHED to the Editor before that tick; the tick commits
the held rune as an insertion. **Every non-chord input settles the
pending rune first**: any other key commits-then-processes; a
`PasteEvent` commits the pending rune, then inserts the pasted text as
ONE atomic literal (pasted bytes are never interpreted as chord keys);
focus loss, `SetValue`, and mode transitions commit the pending rune.
`WithEscapeChord` validates at construction: exactly two unmodified
printable runes (distinct or repeated), or `""` to disable the chord
(Esc alone); anything else panics.

**Keymap is data, not a switch:**

```go
type Keymap map[KeyChord]Action   // KeyChord: mode + normalized key; Action: enumerated op
func DefaultKeymap() Keymap       // returns a COPY — callers cannot mutate shared defaults
```

Actions are an exported enumerated set (`ActMoveLeft`, `ActDeleteLine`,
…) plus the reserved **`ActUnbound`**, which removes a default binding
(r2 — overlays can unbind, not just rebind). `WithKeymap` validates every
entry at construction: the action must exist, and the (mode, action)
combination must be supported (e.g. no Insert-mode motions); violations
panic with the offending chord.

**Registers & undo:** one internal register holding `{text string,
linewise bool}` (no system clipboard in v1 — OSC 52 is a later, separate
concern). The register is importable by the application (r2 — the
value-inspect float's copy path needs it):

```go
func (e *Editor) SetRegister(text string, linewise bool)
func (e *Editor) Register() (text string, linewise bool)
```

Undo is a bounded snapshot stack (64 entries) of whole-buffer states
pushed per atomic edit group, with a redo stack cleared on new edits.
**Group boundaries (r3 — one rule, no overlap):** an Insert-mode session
from entry to exit is ONE group, and a bracketed paste DURING Insert
stays inside that group (paste is Insert input); each Normal-mode edit —
including Normal-mode `p`/`P` — is its own group. Focus loss ENDS the
current Insert group (edits after refocus start a new group) without
leaving Insert mode; Esc/chord and SetValue end it as already specified.

**SetValue is a document-boundary operation (r2):** it settles/commits
any pending chord rune and pending count/double-key state, exits to
Normal mode, clears the selection, resets the cursor to the document
start, replaces the content, and CLEARS both undo and redo stacks. The
register is preserved. (An in-document replacement API can come later if
a consumer needs undo-preserving loads.)

**Cursor shape:** the backend already supports `CursorShape`; the runtime
gains one OPTIONAL capability interface (additive, follows the
capability-interface convention):

```go
// tui.CursorShaper is implemented by components whose hardware cursor
// shape depends on internal state (block in a modal editor's Normal mode,
// bar in Insert). Consulted only for the focused CursorReporter.
type CursorShaper interface{ CursorShape() CursorShape }
```

Editor reports block/underline/bar for Normal/Visual/Insert.

### 2.2 `widget.Tree` — lazy expandable hierarchy

```go
// TreeNode is one explorer entry. Children are UNKNOWN until the node is
// expanded: the widget publishes ExpandRequestEvent and the application
// answers with SetChildren (async loading stays in the app via ctx.Go —
// widgets never own goroutines).
type TreeNode struct { /* opaque; constructed via NewTreeNode */ }
func NewTreeNode(id string, label string, opts ...NodeOption) *TreeNode
// Options: WithLeaf() (never expandable), WithNodeStyle(style.Style),
// WithBadge(string) (trailing annotation, e.g. row counts, "view", "fn")

func (n *TreeNode) ID() string
func (n *TreeNode) SetLabel(string)

func NewTree(opts ...TreeOption) *Tree
// Options: WithRoots(...*TreeNode), WithTreeStyles(ListStyles),
// WithIndent(int) (default 2)

func (t *Tree) SetRoots(roots ...*TreeNode)
func (t *Tree) Selected() (*TreeNode, bool)
func (t *Tree) ExpandPath(ids ...string)   // programmatic reveal
func (t *Tree) AcceptsFocus() bool
```

**Load lifecycle (r2 — generation-tokened; every outcome settles the
spinner, stale results are inert):**

```go
// Expanding a node whose children were never supplied fires this with a
// fresh generation token. The application answers with EXACTLY ONE of
// SetChildren / SetLoadError carrying that token.
type ExpandRequestEvent struct {
	Owner tui.NodeID
	Node  *TreeNode
	Gen   uint64 // request generation
}
type CollapseEvent struct { Owner tui.NodeID; Node *TreeNode }

func (n *TreeNode) SetChildren(gen uint64, kids []*TreeNode)
// Marks loaded on the CURRENT generation only: a stale gen (superseded by
// collapse, re-expand, SetRoots, or Reset) is ignored entirely — late
// TaskResults can never overwrite newer state. Empty kids = loaded leaf.
func (n *TreeNode) SetLoadError(gen uint64, msg string)
// Settles the spinner into an error badge, collapses the node, and
// returns it to the unloaded state — the next expand re-fires
// ExpandRequestEvent with a NEW generation (retry is user-driven).
func (n *TreeNode) Reset()
// Discards loaded children and any in-flight generation (refresh).
```

Rules: collapsing a loading node invalidates its generation (the spinner
stops; the eventual result is ignored); `SetRoots` invalidates every
outstanding generation. **Node identity/ownership (r3 — Tree-owned, not
merely parent-linked):** attaching a node — via `SetRoots` OR
`SetChildren` — stamps it and its subtree with the owning `*Tree`.
`SetRoots`/`SetChildren` panic on: a node already owned (by ANY tree,
this one included — which rejects duplicate pointers, reuse across
trees, and re-attachment of a live root), a duplicate ID among the new
siblings, or adoption of the target's own ancestor (walk-up check —
root→child→root is impossible). Ownership release is precise (r3 amendment): `SetRoots`
releases the root subtrees it removes; `SetChildren` releases the child
subtrees it replaces out; `Reset()` releases the receiver's DISCARDED
descendants — the still-attached receiver itself remains owned. Released
nodes may be re-attached. Node mutations (`SetLabel`,
lifecycle calls) mark the OWNING tree dirty; on an unowned node they
only update state. `ExpandPath` resolves IDs level by level, so
sibling uniqueness is exactly sufficient.

Behavior: `j/k`/arrows move the cursor over the FLATTENED visible rows
(virtualized rendering reusing the List viewport arithmetic); `l`/`Enter`/
`→` expands (or activates a leaf — activation reuses
`widget.ActivateEvent`), `h`/`←` collapses (or jumps to parent). Rows
render as `indent + expander(▸/▾/·) + label + badge` (spinner/error
badges included), truncated with `…` via the shared textutil. Mouse:
click selects, click on the expander toggles.

### 2.3 `widget.Split` zoom (additive)

```go
func (s *Split) SetRatio(r float64)    // exported programmatic sibling of the drag path
type SplitPane uint8
const ( PaneNone SplitPane = iota; PaneA; PaneB )
func (s *Split) Zoom(p SplitPane)      // PaneNone restores
func (s *Split) Zoomed() SplitPane
```

While zoomed, `Layout` gives the zoomed pane the full rect and skips the
other (not laid out, not rendered, not hit-testable, excluded from the
focus ring via visibility — the existing `node.visible()` rule); the
divider disappears. `SplitResizedEvent` is NOT fired by zoom;
`SplitZoomEvent{Owner, Pane}` is.

**Focus repair (r2):** visibility exclusion alone is insufficient —
`App.focused` is not visibility-checked by key/paste routing, so a
focused pane hidden by Zoom would keep receiving input invisibly. Two
complementary rules:

1. **Split-side transfer:** `Zoom(p)` checks whether focus currently
   lives inside the pane being hidden — via a new runtime query
   `Context.FocusWithin(c Component) bool` (additive; walks the focused
   node's parent links) — and if so moves it to the retained pane with
   the existing `focusFirst` walk. If nothing in the retained pane is
   focusable, focus is CLEARED (r3 — never parked on a non-Focusable
   subtree root), leaving rule 2's post-layout repair as the single
   authority. `Restore` does NOT move focus back (the user's focus stays
   where they are).
2. **Runtime safety net (additive, benefits every future hider):** after
   any layout pass, if the focused node is no longer `visible()`, the
   runtime clears focus to the root scope's first focusable — no
   component can keep invisible focus regardless of who hid it.

### 2.4 What this deliberately is not

No syntax highlighting (a later Editor option — the paint path is cell-
based and can take a line-tokenizer seam without API breakage). No
ex-command line (`:w`, `:q`), no macros, no marks, no registers beyond the
single unnamed one, no multi-cursor. No `widget.CommandMenu`/which-key —
v1 proves the pattern app-side in autodb (a Float + List over a binding
table); it graduates to golib when a second consumer wants it.

## 3. Consequences

**Easier:** autodb M6 (and any future golib TUI app) gets a real modal
editor, an explorer, and IDE-style zoom; TextArea's substrate gets a
second consumer, hardening it; the keymap-as-data shape leaves room for
user-configurable bindings.
**Harder:** the Editor state machine is genuinely stateful (pending
counts, chord timer, edit groups) — mitigated by an exhaustive
table-driven key-sequence test suite (sequences in, buffer/mode/register
out) and by keeping every buffer mutation inside the already-tested
textBuffer primitives.

## 4. Files

- `tui/widget/textbuffer.go` (extraction from textarea.go; TextArea
  unchanged in behavior — its tests must pass untouched)
- `tui/widget/editor.go`, `editor_keymap.go`, `editor_test.go`
- `tui/widget/tree.go`, `tree_test.go`
- `tui/widget/split.go` (+Zoom/SetRatio), `split_test.go` additions
- `tui/component.go` (+`CursorShaper`), runtime cursor-shape plumbing
- `docs/tui/adr-0008-…` (this file); READMEs

## 5. Acceptance criteria

1. Editor: table-driven sequence tests covering every default binding,
   counts (incl. `0`-as-motion vs count digit), the double-key pending
   buffer (`dd`/`yy`/`gg` complete; `d`+other cancels-then-processes),
   the escape chord (second-rune-dispatched-first, tick-commit,
   paste-settles-pending, focus-loss/SetValue settle), cursor invariants
   at line ends/empty lines, inclusive visual ranges, register
   linewise-ness incl. SetRegister/Register, undo/redo group boundaries,
   SetValue document-boundary semantics; mode events observed; unbound
   keys (incl. Space) verified to bubble in Normal mode; keymap overlay
   validation incl. ActUnbound and DefaultKeymap copy semantics.
2. TextArea's existing test suite passes unchanged after the extraction.
3. Tree: expand-request generation lifecycle — stale SetChildren/
   SetLoadError ignored after collapse/re-expand/SetRoots; error badge +
   user-driven retry re-fires with a new generation; duplicate-sibling-ID
   and double-parent panics; virtualization over 10k visible rows stays
   allocation-sane.
4. Split: Zoom excludes the hidden pane from layout/render/hit-test/focus
   ring AND transfers focus out of the hidden subtree; the runtime
   safety net clears focus when any layout pass hides the focused node;
   Restore returns to the prior ratio without moving focus; SetRatio
   clamps to MinSizes.
5. `-race` clean; all interaction through exported APIs in tests (the
   package's harness idiom).
