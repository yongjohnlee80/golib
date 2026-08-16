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

**Default keymap (v1 command set — the consumer's explicit list plus the
minimum companions that make it a usable vim; anything absent is absent
deliberately):**

- Motions (Normal + Visual, count-prefixable — `3w`, `2}`):
  `h j k l`, `0 $`, `w b e`, `{ }` paragraph back/forward, `[ ]` aliases
  of `{ }` in v1 (vim's `[`/`]` are prefix families; the useful single-key
  reading is paragraph motion — revisit if the consumer wants otherwise),
  `gg G`, arrows/Home/End/PgUp/PgDn as their obvious equivalents.
- Entering Insert: `i a o O A I` (`o/O` open lines below/above).
- Insert mode: text/Paste inserts via the TextArea substrate; `Esc` OR the
  **escape chord** returns to Normal. Chord rule (`WithEscapeChord("jk")`):
  on the first chord rune, hold it pending for 300 ms — a following second
  rune within the window cancels the pending insert and leaves Insert
  mode; timeout or any other key commits the held rune first. Implemented
  with the runtime tick (no goroutines in widgets).
- Operators/edits (Normal): `x` (count-able, into the register),
  `dd` (line delete, into the register), `D` (to end of line),
  `yy` (line yank), `p P` (paste after/before; line-wise register pastes
  on a new line), `u` (undo), `Ctrl-R` (redo).
- Visual: `v` (char-wise), `V` (line-wise), motions extend; `y` yanks,
  `d`/`x` delete, `Esc`/chord exits.
- **Not consumed in Normal/Visual mode:** Space, Tab, and every key with
  no binding — they bubble (ADR-0005), so the application's leader menu
  and focus traversal work without editor cooperation. Insert mode
  consumes text keys (Tab inserts, matching TextArea's veto mask rules for
  modified keys).

**Keymap is data, not a switch:**

```go
type Keymap map[KeyChord]Action   // KeyChord: mode + normalized key; Action: opaque named op
func DefaultKeymap() Keymap       // the table above; WithKeymap overlays entries
```

Actions are an enumerated set (`ActMoveLeft`, `ActDeleteLine`, …); the map
makes rebinding data-driven for consumers (autovim parity can grow without
forking the widget). Unknown chords in a `WithKeymap` overlay panic at
construction.

**Registers & undo:** one internal register holding `{text string,
linewise bool}` (no system clipboard in v1 — OSC 52 is a later, separate
concern). Undo is a bounded snapshot stack (64 entries) of whole-buffer
states pushed per atomic edit group (an Insert-mode session from entry to
Esc is ONE group; each Normal-mode edit is one group), with a redo stack
cleared on new edits.

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
func (n *TreeNode) SetChildren(kids []*TreeNode) // also marks loaded; empty = leaf-like
func (n *TreeNode) SetLabel(string)

func NewTree(opts ...TreeOption) *Tree
// Options: WithRoots(...*TreeNode), WithTreeStyles(ListStyles),
// WithIndent(int) (default 2)

func (t *Tree) SetRoots(roots ...*TreeNode)
func (t *Tree) Selected() (*TreeNode, bool)
func (t *Tree) ExpandPath(ids ...string)   // programmatic reveal
func (t *Tree) AcceptsFocus() bool

type ExpandRequestEvent struct { Owner tui.NodeID; Node *TreeNode } // fired once per unloaded expand
type CollapseEvent      struct { Owner tui.NodeID; Node *TreeNode }
// Activation reuses widget.ActivateEvent (Enter / double-click) with the
// selected node retrievable via Selected().
```

Behavior: `j/k`/arrows move the cursor over the FLATTENED visible rows
(virtualized rendering reusing the List viewport arithmetic); `l`/`Enter`/
`→` expands (or activates a leaf), `h`/`←` collapses (or jumps to parent);
expanding a node whose children were never supplied fires
`ExpandRequestEvent` exactly once and shows a spinner badge until
`SetChildren` lands. Rows render as `indent + expander(▸/▾/·) + label +
badge`, truncated with `…` via the shared textutil. Mouse: click selects,
click on the expander toggles.

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
`SplitZoomEvent{Owner, Pane}` is. Nested-split maximize (a 3-pane IDE is
`Split(H, explorer, Split(V, query, results))`) is achieved by the
application zooming each Split along the ancestor chain — the widget
stays a two-pane primitive.

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
   counts, the escape chord (both the cancel and the timeout-commit
   paths), visual char/line operations, register linewise-ness, undo/redo
   group boundaries; mode events observed; unbound keys (incl. Space)
   verified to bubble in Normal mode.
2. TextArea's existing test suite passes unchanged after the extraction.
3. Tree: expand-request fires exactly once per unloaded node; SetChildren
   from a TaskResult renders; collapse/re-expand does not re-fire;
   virtualization over 10k visible rows stays allocation-sane.
4. Split: Zoom excludes the hidden pane from layout/render/hit-test/focus
   ring; Restore returns to the prior ratio; SetRatio clamps to MinSizes.
5. `-race` clean; all interaction through exported APIs in tests (the
   package's harness idiom).
