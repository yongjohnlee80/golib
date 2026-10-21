# ADR-0012 — `golib/tui`: the architecture follow-up, its shared contracts and lane boundaries

- **Status:** **Proposed** (2026-10-21, authored by kimmy-vision for Johno). The contracts below have been
  in force as "interim contracts" since 2026-10-21. Johno started implementation before this ADR was
  reviewed, so it records them and does not gate the lanes. Two amendments have been made since,
  both included here.
- **Date:** 2026-10-21
- **Module:** github.com/yongjohnlee80/golib (tui, tui/*, gui)
- **Reviewer:** Zen (Johno: "Zen will be the reviewer for these follow PRs").
- **Related:** golib #401, the Phases 0–3 checkpoint (v0.7.25). ADR-0004 (tree and lifecycle), ADR-0005
  (events, bus), ADR-0007 (widgets), ADR-0010 (mouse). The audit reports are in the KB under
  `reviews/kimmy-vision/2026-10-21-golib-architecture-audit-*.md`, and the revalidation against fe4cba9 is
  `reviews/kimmy-vision/2026-10-21-golib-tui-followup-lane0-revalidation.md`.
- **Lane tasks:** KB `.todo-list/in-progress/2026-10-21-golibtui-follow-up-lane-{0,a,…,g}-*`. These hold the
  live assignee, item status, PRs, heads, tags and evidence; this ADR holds the boundaries.
- **Rationale:** `adr-0012-architecture-follow-up-contracts-rationale.md`. **Read it before changing C1–C4
  or a lane boundary.** Each of these was considered and rejected:
  - one ADR per mechanism with no shared contract;
  - an epoch for keyed views (Menu);
  - a tui-root home for cell attributes (an import cycle);
  - deferring owner notifications to the end of an operation through a depth counter;
  - patching each callback site on its own;
  - keeping the lane boundaries only in KB tasks.

## 2. Problem

Observed in the code at fe4cba9:

- An owner callback may replace or refresh a row view's rows mid-operation; only List, Tree and TableCore guard it.
  - FileList, Menu, subscription teardown and stale async completions were outside the guard.
- The editor's key precedence exists only as the code order of `handleInsertKey` and `handleCommandKey`.

Risks of the parallel work:

- Seven lanes replacing copied mechanisms will write competing helpers unless each helper has one home.
- Lane PRs change API that reaches AutoDoc, AutoDB and gui (released tags) with no per-change rule.
- A new or shared file has no owner rule, so two lanes can both claim it.
- Choices the audit leaves open would be settled inside a PR without a named decider.

Requirements:

- C1: one source-lifecycle contract per kind of row view; what ends the rows, and how an operation stops.
- C2: one home per shared helper, with its package, exports, producer, consumers and order.
- C2: the shared invariants named: Render and Layout purity, and cross-package capabilities.
- C3: the key-dispatch precedence stated, with the key ADR choosing a mechanism within it.
- C4: source and behavioural compatibility, per change.
- Lane boundaries, the ordering points, the producer and adopter of each seam, and the file-owner rule.
- A decision register with a decider per open choice; amendments go through this ADR.

Add'l Improvements:

- The owner-callback guard widened to every positional view as lanes reach them.

## 3. Strategic summary

1. This ADR fixes C1–C4 and the lane boundaries. A lane's own ADR chooses a mechanism only within
   them, and cannot override them.
2. Each lane edits only its own paths.
   - A lane that produces a shared seam lands the minimal seam first, in its own PR.
   - Each adopting lane switches to it in its own PR and deletes its copy there.
3. Lane D runs D → E → F as a dependency chain, with minimal producer seams first. Lane A runs before G.
   G adopts each export once a tag carries it (Jarvis tags).
4. A new file belongs to the lane that owns its directory or name prefix. If that is ambiguous, Lane 0
   decides, and the answer is recorded here as an amendment.
5. A contract or boundary that proves wrong stops the lane that found it. The fix is an amendment
   here, reviewed by Zen, never a local workaround.
6. A choice in the decision register is made by its named decider and recorded in the lane's ADR or
   PR, never settled implicitly by an implementation.

Keeping the record:

- The KB lane tasks hold the live assignee, item status, PRs, heads, tags and evidence. Reassigning
  a lane changes its task, not this ADR.

## 4. Detail

### 4.1 C1 — source lifecycle

Every row view is one of two kinds. Its owner is always told **last**: the view's own writes and the
bus publish come first.

```go
// rowsEpoch is a positional view's row identity over time (tui/widget/presspair.go).
type rowsEpoch struct{ n uint64 }

func (e *rowsEpoch) advance()                  // the rows an operation began on are gone
func (e *rowsEpoch) Survives(step func()) bool // run step (it may tell an owner); false if it advanced

// pressPairer embeds it: Reset advances; Press = Survives(moved) then pair.
type pressPairer[K comparable] struct {
	rowsEpoch
	last K
	has  bool
}
```

**Positional views** (an index names a row: List, Tree, TableCore, DataTable, FileList). After an owner
callback, a positional view goes on only through a **survival call**: a method whose result is
declared `live bool`, checked. Any other result, such as a yank's `ok`, says nothing about the rows.

| Operation | Ends the epoch | Then |
|---|---|---|
| New source (`List.SetSource`, `Tree.SetRoots`, `TableCore.SetSource`) | yes (`Reset`) | the cursor, selection, sort and filter start again |
| An owner's in-place refresh (`List.RefreshSource`, `TableCore.Changed`) | yes | the cursor is kept on its row where the row survives |
| The core's own edit (`TableCore.changed`: delete, cut, paste, insert, keep) | no | the operation goes on with its rows |
| A new listing (`FileList.show`) | yes (`advance`) | hooks are told only while that listing is shown |
| A pager's arrival (`TablePager`: "rows arrive later, followed by TableCore.Changed") | yes (at rest) | a double-click straddling an arrival does not pair; whether an append preserves identity is Lane B's decision (§4.7) |
| Unmount | — | subscriptions taken through `ctx` are released in `OnUnmount` |
| An async result (`TreeNode.SetChildren`, `SetLoadError`) | — | generation-checked: a stale `gen` is inert |

What "stop" forbids: no continuation against stale indices, and no later activation, capture, release
or menu dispatch to a stale target.

**Keyed views** (Menu, by `ItemID`). A keyed view holds no epoch. It re-resolves by key after an owner
callback (`findItem`, `Checked`, `repairSelection`'s scan). A row with the same key after a model change
is the same item by the owner's own naming.

`internal/audit/owner_callback_test.go` checks the positional shape: a call that can reach an `on*` hook
is in tail position or is a checked survival call. It covers List, Tree, TableCore, DataTable,
TreeNode, FileList, Select and its popup. The guard is structural; the callback-replacement tests are
the behavioural proof (rationale §R3).

### 4.2 C2 — shared helpers and invariants

One producer per helper. Consumers adopt it in their own PRs and delete their copies there. Anything gui
or decl must use is exported, with public types; `internal/` is for helpers used only inside the root
module.

| Helper | Home | Producer | Consumers (in order) |
|---|---|---|---|
| Paste/newline normalizer (support-D5) | `tui.NormalizeNewlines`, new (tui/clipboard.go) | D | A (term, web), C (textbuffer.go), G after a tag |
| Cell attributes, cursor shapes (support-D7) | `style.AttrMask`, `style.CursorShape` (tui/style); `tui` and `vt` alias them | A | A (style, vt, web client.js via clientConfig), G |
| xterm key/SGR tables (support-D8) | tui/internal/xterm | A | A (term, vt, web, widget/terminal_keys.go) |
| Cursor/image latch (support-D3) | exported in the tui root (backend.go) | D | A (term, web), D (testbackend), G after a tag |
| `Translator` (support-I1) | new interface in the tui root | D | `*i18n.Set` satisfies it unchanged |
| Text painting and wrap (core-D7) | exported in the tui root | D | B, C, E in their own PRs; G after a tag |
| Relative luminance (support-D4, gui-D1) | tui/style | A | A (term/probe), G after a tag |
| `ContinuationOf` keeps `Face` (gui-D11) | tui root | D | G after a tag |
| `Context.Theme()` (data-S5) | tui root | D (#409) | B (DataTable.Theme) |

Named exception: Lane A may edit tui/cell.go and `CursorShape` in tui/backend.go only to turn the
declarations into aliases. Lane D leaves those declarations alone until that PR merges.

Shared invariants:

```go
// Render and Layout are pure. Temporary measurement is free; committed, observable state goes
// through ctx.AfterLayout(key, fn). Never defer an assignment so that layout runs forever.
func (w *X) Layout(c tui.Constraints) tui.Size // stores only what AfterLayout commits
func (w *X) Render(s tui.Surface)              // stores nothing; reads are bounded

// A capability another package implements has exported methods and public types; discovery is
// optional and consumer-driven, by assertion at the point of use.
```

Each widget lane adds, for its views, a test that repeated layout, render and hit queries change no
semantic state. (`TestAListPaintChangesNothingItWasNotTold` is the first, in #407.)

### 4.3 C3 — key precedence

This is today's order, and it is the contract until Lane C's key ADR replaces it under this ADR. A
lane may not reorder it implicitly.

```text
EditorCore.HandleKey(k):
  Insert mode → handleInsertKey:
    1. read-only core: an editing key (insertEdits) is consumed, refused
    2. keymap: unbound → bubble; bound → exec (the one-shot keeps its own record)
    3. modal: the escape chord (takeEscapeChord)       — a held rune, its timeout
    4. Replace mode (replaceKey)
    5. Insert assists, in order (insertAssists: tableKey, listKey)
    6. record for . and macros (recordInsertKey)
    7. editing keys (insertEditKey: Tab, Esc, Enter, Backspace, Delete)
    8. caret keys (insertCaretKeys) → motions
    9. text
  Normal/Visual:
    1. Insert's Ctrl-o one-shot (oneShotKey)
    2. the host's chord hook (SetChordHook), when nothing is pending, never Esc
    3. dot/macro record, then handleCommandKey:
       Esc (commandEscape)
       → the key an armed command waits on: find, r, object, mark, macro, register (take*)
       → a count's digit (takeCountDigit)
       → keymap lookup; an unbound Alt/Super chord bubbles; unbound drops what is pending
       → the g prefix's second key (completeGoPrefix)
       → i/a text-object arming; an operator's doubled form
       → a bound key under a pending operator (continueOperator)
       → the key's own action (commandAction)
```

Lane C's key ADR states, under this order:
- consumption versus fallthrough per step;
- the mode change each step may cause;
- the escape-chord and pending-command timeouts;
- custom bindings over assists (`binding Tab` must not break table and list Tab).

Command ids are the source of truth for listings and dispatch (editor-S8).

### 4.4 C4 — compatibility

An API-changing PR states its old and new **behaviour**: events, signatures, enum spelling and values,
nil policy, and lifecycle order.
- It keeps the old name in `<file>_deprecated.go` ("Deprecated: … Not maintained; removed in the next minor version").
- Where an alias cannot carry the behaviour, it adds a wrapper or an event migration.
- It runs the VM43 exact-head gate (root and gui modules, read-only) and the AutoDoc and AutoDB compatibility builds.

gui builds against released tags. Lane G adopts each export once a tag carries it, not after all
lanes finish.

Ordering point: decl-D7 (the enum system) lands with or before gui-S12, which depends on the bare
`Editor` enum value.

### 4.5 Lane boundaries (initial owners)

| Lane | Initial owner | Paths (exclusive) |
|---|---|---|
| 0 | kimmy-vision | this ADR, revalidation, the contracts |
| A | jarvis | tui/term, tui/vt, tui/internal/vtparse, tui/web, tui/pty, tui/style, tui/i18n, auth/*, tui/widget/terminal*.go |
| B | kimmy-vision | tui/widget list*, tree*, table.go, tablecore*, datatable*, table_csv/text/document*, tablewhere/order, select*, filelist*, filesource*, fileviews*, filepath*, menu*.go, presspair.go; tui/decl views.go, treeview.go, tableview*.go, filedialog.go |
| C | kimmy-vision | tui/widget editor*, textbuffer.go, textarea*, textinput*, commandline*, bufferview*, editormenu.go; tui/keymap, tui/key; tui/decl editor_decl.go, keys.go |
| D | gold-man | the tui root package, tui/internal/* (not vtparse), tui/widget/text.go (not tui/cell.go until Lane A's D7) |
| E | gold-man | the rest of tui/widget |
| F | gold-man | the rest of tui/decl |
| G | jarvis | gui/ |

Ordering points:
1. A's support-D7 lands before D edits tui/cell.go.
2. D's latch helper (support-D3) lands before A adopts it.
3. D's normalizer (support-D5) lands before A, C and G adopt it.
4. B's tablecore PRs land before C's shared count/g grammar (data-D10).
5. C exposes the editor's motions before A's shell-D16 (Terminal normal mode).
6. F's decl-D7 lands with or before G's gui-S12.
7. Each of G's adoptions follows the tag carrying its export.

Shared hot files:
- tui/widget/commands.go: each lane declares its commands in its own files; `KeyCommands` is derived.
- internal/audit ledgers: re-pin per PR.
- README.md and IMPLEMENTATION.md: each lane edits only its own sections.
- internal/audit/owner_callback_test.go: Lane B widens its view list.

### 4.6 New and shared files

- A new file belongs to the lane that owns its directory or name prefix.
- A new test helper shared across lanes lives beside its first user's code, owned by that lane.
- Ambiguity goes to Lane 0, and the answer is recorded as an amendment (§4.8).

### 4.7 Decision register

| Decision | Finding | Decider | Escalate to Johno when |
|---|---|---|---|
| Container foreign-child policy | core-S7 | D | it is a public behaviour change |
| `WithTheme(nil)` | core-S5 | D | it panics where it used to default |
| `Keyer`: remove, or build a keyed reconciler | core-S8 | D | it is removed (public API) |
| Tooltip and too-small presenter seams | core-S4/I4 | D | new public options |
| Value vs pointer action semantics | shell-S8 | E | always (behaviour) |
| Generic service registry for `Build` | decl-S8 | F | new public API |
| `HostObject` / model reading | decl-S7 | F | new public API |
| Public Adopt and declaration markers | decl-S9 | F | new public API |
| `Arity` for child counts | decl-D12 | F | — |
| Cached Type tables (no shared mutable maps) | decl-D11 | F | — |
| Whether a pager append preserves row identity | C1 | B | a double-click behaviour change |
| Tree as a List (data-I1) | data-I1 | B | public Tree API changes |

### 4.8 Amendments

| # | Date | Change | By |
|---|---|---|---|
| 1 | 2026-10-21 | C2: support-D7 homed in tui/style (tui imports style); tui and vt alias it; `vt.AttrStrike` 128→64 and `AttrInvisible` 64→128, stated in #405 | Jarvis, approved by Lane 0 |
| 2 | 2026-10-21 | C1: `rowsEpoch` is the shared type; positional views stop on it and keyed views (Menu) re-resolve by key; the guard covers the positional views | Lane B (#410) |

## 5. Expected results

Every lane PR is gated on VM43: the root and gui modules at the exact head (read-only), and the
AutoDoc and AutoDB compatibility builds. Defects are fixed test-first, red on VM43 before the fix.
Where a test claims sensitivity, a mutant shows it.

1. Callback replacement and refresh through real entry points, per positional view, each with an
   unchanged-source positive control. List, TableCore, DataTable and FileList are done (#401, #410);
   decl's views are next.
2. Render/Layout purity: repeated layout, render and hit queries change no semantic state, per widget
   lane.
3. Compatibility: AutoDoc and AutoDB build and vet against each exact head, and each C4 behaviour
   statement is checked against downstream use.
4. Key precedence: Lane C's ADR keeps today's order (§4.3) unless it changes it explicitly. Default
   schemes keep working.
5. Shared helpers: each producer PR lands before its adopters, and no copy remains once the adopters
   are merged.

Also: the owner-callback guard holds at zero across every view it covers. Each amendment is reflected in
both §4.8 and the Lane 0 task.
