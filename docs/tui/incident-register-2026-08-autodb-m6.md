# tui incident register — autodb M6 (2026-08-16 → 17)

Every defect chased through `golib/tui` (and its autodb consumer) during
the M6 TUI build, with the remedy **scored**: was the cause identified
and fixed at the source, or was a symptom patched at the consumer on a
guess?

Standing rule this session produced (Johno, 2026-08-17):

> Don't put bandaids on symptoms. Fix the issue at the core so the
> modules stay reusable and maintainable.

## Scoring key

| Score | Meaning |
|---|---|
| **A — source fix** | Cause proven, fixed in the module that owns it. Every consumer benefits. |
| **B — correct consumer fix** | Cause proven, and the defect genuinely belonged to the consumer. |
| **C — bandaid, kept** | Works, but chosen without proving the cause; retained only because it is defensible on other grounds. |
| **D — bandaid, reverted** | Wrong patch on a guessed cause. Reverted once evidence arrived. |
| **F — speculative core change** | A guess pushed into the framework — the worst outcome, because it looks authoritative. |

**Tally: 11 × A, 3 × B, 1 × C, 2 × D, 1 × F.** The F and both Ds came
from the same incident (#9), the only one attacked before evidence
existed.

---

## The register

### 1. Tree cursor invisible under a delegating wrapper — **A**
`Tree.Render` painted the cursor row only when the Tree itself was
focused; autodb's explorer wrapper holds focus and forwards keys, so no
cursor ever drew. `List` never gated its cursor this way and ADR-0008
§2.2 says Tree reuses List semantics — the gate was simply wrong.
**Fix:** cursor row paints regardless of focus (golib `1bc2d48`).
**Evidence:** read the render path before changing it.

### 2. Cursor colour could not follow focus — **A**
Row styles were construction-time only, and a widget cannot see focus
resting on a wrapper.
**Fix:** `List.SetStyles` / `Tree.SetStyles` for runtime restyling; the
host supplies focused and blurred styles (golib `8edc5bf`).

### 3. Focus-dependent styling lagged until an unrelated re-layout — **B**
autodb applied styles from `Layout`, but focus changes repaint without
re-laying out. Presented exactly as "the highlight only turns grey when a
modal opens".
**Fix:** restyle on the `FocusEvent` that bubbles to the root (autodb).
**Evidence:** the user's own observation named the trigger precisely.

### 4. `List` ignored `j`/`k` — **A**
Arrow keys only, while `Tree` had honoured vim motions since ADR-0008 —
an internal inconsistency in a vim-keyed widget set.
**Fix:** `j/k/g/G` on List, `g/G` on Tree (golib `1968daf`).

### 5. `Ctrl-l` expanded a tree node instead of moving panes — **A**
Ctrl keys carry the bare letter in `Code`; `Tree`/`List` switched on
`Code` without checking `Mods`, so they consumed application chords.
**Fix:** both bubble any key carrying Ctrl/Alt/Super/Hyper/Meta
(golib `1968daf`).

### 6. One flex column swallowed the row — **A**
`Table.resolveWidths` honoured only the FIRST zero-width column and
pinned the rest to `flexMinWidth`. A uuid column took the whole results
pane.
**Fix:** remaining width shared evenly across all flex columns
(golib `3692317`).

### 7. Saved notes never appeared in the explorer — **A**
No way to refresh one subtree: `SetChildren` needs a host-invisible
generation, `ExpandPath` moves the cursor.
**Fix:** `Tree.Reload(id)` — re-request under a new generation, cursor
undisturbed (golib `acde263`).

### 8. JSON results view had no cursor — **A + B**
Two layers. The view was a `BufferView` (a log tail: no cursor, no
selection) — **A:** added `Editor.SetReadOnly`, a navigable viewer that
refuses edits (golib `c0878fa`). Even then nothing drew — **B:** the
runtime asks only the *focused* component for a cursor, and autodb's
results panel held focus while forwarding keys; fixed by delegating focus
to the child that draws.

### 9. The "picker flake" — **F, D, D, C, then A** ⚠️
~30% of full-suite runs: the connection picker opened, Enter selected
nothing. Attacked four times before any evidence was gathered:

| Attempt | Patch | Score |
|---|---|---|
| 1 | Retry modal focus seeding after layout, inside `Float` | **F** — speculative framework change. Caught only because the test written to prove it **passed without it**. Reverted. |
| 2 | Focus the list from its own mount hook | **D** — reverted |
| 3 | Build the picker's list in the constructor | **D** — harmless, but justified by an unverified diagnosis; kept as a convention, rationale corrected |
| 4 | Make the picker focusable and handle Enter itself | **C** — kept, but only because it matches the manager shape; the comment claiming a mount-order cause was removed as false |
| 5 | **Runtime trace** | **A** — see below |

**Actual cause:** the e2e waited on the picker's title, which the leader
MENU renders as its own label, so Enter was pressed before the float
existed:

```
key node=          prev=*widget.Editor (Enter)   ← consumed by nobody
scope node=*widget.floatLayer (open)            ← float opens AFTER
focus node=*app.connPicker
```

**Fix:** harness `leader()` helper that returns only once the menu has
closed (kills the class), plus product-side titles that no longer
duplicate menu labels. Zero failures in 12 full-suite race runs after.

**Cost of guessing:** four patches, one of them in the framework, and
several hours — against one trace read.

### 10. An `Editor` inside a modal float could not be dismissed — **A**
`handleCommandKey` consumed Esc unconditionally in Normal mode, which
vim treats as a no-op, so the float never saw the key.
**Fix:** Esc consumes only when it cancels something, otherwise bubbles
(golib `a5d537a`). Found by trace while chasing #9's neighbours.

### 11. Chooser floats were indistinguishable — **B**
Menus, confirmations and conflict prompts all rendered as
"SPC — commands": bad for the user, and the reason a test matched the
wrong surface.
**Fix:** `openLeader` takes a title; each chooser says what it asks.

### 12. Four e2e waits matched the wrong surface — **B (×3 late, then A)**
Waits matched the status bar, a tree row, and twice a menu label. The
first three were fixed one at a time (**B**, but the pattern should have
been recognised) before the harness-level fix landed (**A**).

### 13. Test-only data races in autodb fixtures — **B**
Package-level counters shared by `t.Parallel()` tests in `rpc` and
`core/exec`. Made atomic; the repo was then swept for the pattern.

### 14. Stale `--serve` daemon served old code — **B (product gap → A)**
A rebuilt binary kept talking to the daemon started earlier (the shared
server outlives frontends by design), so backend fixes appeared not to
work. Diagnosed from `/proc/<pid>/exe` showing `(deleted)`.
**Fix:** an admin-only `sys.shutdown` verb and an in-TUI restart binding,
so the lifecycle is operable rather than requiring `pkill`.

---

## What the scores say

- **Everything diagnosed with evidence scored A or B.** Every C/D/F came
  from one incident, and from attacking it before the runtime state was
  observable.
- **The framework change (F) is the one to remember.** It was plausible,
  it was small, it would have shipped — and it was wrong. The check that
  caught it is cheap and should be routine: *write the failing test
  first; if it passes without your fix, your diagnosis is wrong.*
- **Tooling converted the worst incident into the cheapest.**
  `tui.WithTrace` (golib `0684db6`) exists because #9 was undiagnosable
  from inside a component. Incident #10 was then found in minutes.
- **Three consumer-side bugs (#3, #8b, #12) share one root:** assumptions
  about runtime behaviour that the docs never stated. All three are now
  in the tutorial (chapters 4, 6, 8) rather than in someone's memory.
