
# ADR-0003 — golib/tui: Cell Buffer & Render Pipeline

**Tags:** `type:adr` `status:accepted` `owner:shared` `repo:golib` `area:tui` `area:render` `kind:cell-buffer` `kind:surface` `kind:unicode` `kind:frame-scheduling`

**Abstract:** Defines the grapheme-cluster `Cell`, the double-buffered curr/last cell
grid and its diff algorithm, wide-cell boundary invariants, the `tui.Surface` clipped
render target, the single-write flush path with synchronized-output bracketing, frame
scheduling (render-on-dirty, min-frame-interval, resize full-repaint), the generated
Unicode width/segmentation tables in `tui/internal/grapheme`, and the normative
anti-flicker and IME cursor rules.

> **Authored natively in the KB** (jarvis, 2026-07-08) as part of the golib/tui design
> dossier. golib/tui keeps its own 0001–0007 ADR numbering; the `golib-tui-` filename
> prefix namespaces it against this KB's auto-agents ADR sequence. Navigation hub:
> golib-tui. Umbrella ADR: golib-tui-0001-overview-and-architecture.

- **Status:** **Accepted** (Johno, 2026-07-08) — revision 1 (Lector r1 `change_requested` → r2 `approved_with_amendments`, all amendments folded).
- **Date:** 2026-07-08
- **Reviewed by:** lector, 2026-07-08 — agents/lector/reviews/2026-07-08-golib-tui-adrs-review.md (r1); agents/lector/reviews/2026-07-08-golib-tui-adrs-rereview.md (r2, `approved_with_amendments`)
- **Module:** github.com/yongjohnlee80/golib
- **Supersedes:** none (greenfield)
- **Related:** ADR-0001 (umbrella — fixes grapheme cells, diff+one-write, and the
  Surface seam this ADR specifies), ADR-0002 (backend — `Flush(diff)` consumes this
  ADR's diff; `Capabilities.SyncOutput/UnicodeCore` gate §2.5/§2.8), ADR-0004 (layout —
  produces the rects components paint through Surfaces), ADR-0005 (runtime — hosts the
  frame scheduler specified in §2.6 on the App loop), ADR-0006 (styling — `style.Style`
  is the cell's third field; the Surface carries the resolution context). golib server
  ADR-0006 (transport scaffold — the registry wake/broadcast idiom reused by the frame
  scheduler).

> **Revision history.** rev 1 (2026-07-08): folded Lector r1 `change_requested` —
> (1) must-fix #3: the grapheme width policy is now explicit and callable —
> `WidthPolicy` enum, package-level `StringWidth` pinned to `WidthPolicyDefault`,
> policy-explicit `StringWidthPolicy`, `Graphemes` re-specified as segmentation-only
> (policy-independent), `Surface` gains a policy-applying `StringWidth`, and the
> components-measure-via-Surface normative rule (§2.4, §2.7); (2) Q5 answer folded:
> `Fill` with a width-2 cluster now fills the trailing odd column with a styled space
> instead of leaving it untouched (§2.4, §5.9); (3) Lector's Q1–Q5 answers annotated in
> §6.

> **Self-containment contract.** This ADR is implementable with no prior context beyond
> the golib conventions (golang, golib) and the sibling ADRs listed under Related.

## 1. Context

ADR-0001 §2.4 #3 fixes the pipeline shape: components paint Surfaces → grapheme-string
cell buffer → curr/last diff → one buffered write inside mode-2026 brackets. This ADR is
the full specification of that middle layer — the data structures, the invariants, the
byte-emission rules, and the Unicode machinery underneath them.

Evidence base (research dossier, 2026-07-08):

- **Cells hold grapheme clusters, not runes.** The 2026 convergence across
  independently-evolved stacks: tcell v3 rewrote its cell to a grapheme string
  (`currStr string`) after a decade of rune+combining-slice cells; vaxis cells are
  `Character{Grapheme string, Width int}` (go.rockorager.dev/vaxis); bubbletea v2
  replaced its string renderer with the cell-based ultraviolet compositor. Mitchell
  Hashimoto's write-up is the canonical statement of the failure class — a terminal and
  an app disagreeing on cluster width desynchronizes every subsequent cursor-addressed
  write (https://mitchellh.com/writing/grapheme-clusters-in-terminals).
- **The curr/last double buffer is proven.** tcell's `CellBuffer` stores current and
  last-shown state per cell; dirty = `curr != last`, with no separate bitmap to fall out
  of sync; `Show()` emits only dirty cells, moves the cursor only across
  discontinuities, and re-emits SGR only on style change — buffered into a single flush.
- **Go stdlib has no width data.** `unicode` exposes neither UAX #11 East Asian Width
  nor UAX #29 grapheme breaks (verified against Go 1.25 — https://go.dev/doc/go1.25);
  runewidth and uniseg both generate tables from the UCD, and uniseg is itself zero-dep
  — the recipe golib copies rather than the dependency it takes (ADR-0001 §2.2).
- **Flicker and wide-char corruption are the two classic failure modes** (dossier §8):
  flicker from partial writes/cursor jumps/clear-then-redraw; corruption from splitting
  wide cells or from app/terminal width disagreement on ZWJ emoji, VS16, and flag
  sequences — with tmux compounding the damage. Mode 2027 / Terminal Unicode Core is
  the negotiated fix where available
  (https://github.com/contour-terminal/terminal-unicode-core).

### 1.1 Goals

- **G1** — A cell model that cannot express a torn grapheme: one cluster per cell,
  width cached, continuation cells structurally distinct.
- **G2** — Byte-minimal frames: only dirty cells cross the wire, one `Write` per frame,
  zero bytes when idle (ADR-0001 G5; the PTY write + emulator parse path is the real
  bottleneck — dossier §8 soft_ratatui datapoint).
- **G3** — Flicker-freedom as *normative rules* (§2.9), not folklore — testable against
  `TestBackend` and a counting writer.
- **G4** — `Surface` as the only thing components ever paint on: clipped, composable
  via `Sub`, carrying the style-resolution context so rendering needs no globals.
- **G5** — Unicode correctness with a documented provenance chain: generated,
  committed, versioned tables; UAX #29 conformance-tested segmentation; a defined
  refresh procedure.
- **G6** — Graceful behavior on terminals whose width opinions we cannot know: a
  conservative emission strategy (§2.8) instead of hoping.
- **G7** — Predictable frame cadence: render-on-dirty with a min-frame-interval,
  coalesced dirty marks, full repaint across resizes.

### 1.2 Non-goals

- **N1** — Pixel/graphics cells (sixel, kitty graphics). Additive later behind the same
  Cell/Surface seams (ADR-0001 N1).
- **N2** — Damage tracking finer than the cell diff (per-component damage rects). The
  curr/last diff already bounds output bytes; rect bookkeeping would optimize CPU we
  have not measured.
- **N3** — General text shaping (bidi, Arabic joining beyond what clusters give,
  ligatures). Cluster segmentation + width is the contract; shaping is the terminal's
  problem.
- **N4** — Word/sentence segmentation (UAX #29 has them; widgets needing word-wrap get
  a helper in ADR-0007, built on cluster iteration, not a full boundary engine in v1).
- **N5** — Scrollback/history preservation of the primary screen (alt-screen apps by
  default; inline mode is ADR-0002's `WithoutAltScreen`).

## 2. Decision

### 2.1 The `Cell`

```go
package tui

// Cell is one terminal grid cell. Content holds a COMPLETE grapheme
// cluster — never a partial one, never more than one. This is the
// tcell v3 / vaxis / bubbletea-v2 convergence
// (https://mitchellh.com/writing/grapheme-clusters-in-terminals).
type Cell struct {
	Content string      // one grapheme cluster; "" on a wide-cell continuation
	Width   uint8       // display columns: 1 or 2; 0 marks a continuation cell
	Style   style.Style
}

// Continuation reports whether c is the right half of a wide cell.
func (c Cell) Continuation() bool { return c.Width == 0 }
```

Width is **cached at write time** (measured once by `SetCell`, under the Surface's width
policy, §2.4), not recomputed per frame: the diff and the emitter consult it on every pass, and measurement is a
table-walk over the cluster's runes. `style.Style` is a comparable value type with
typed fields + a set-bitfield (ADR-0006, following lipgloss v0.11's reversal of the
property-bag design —
https://raw.githubusercontent.com/charmbracelet/lipgloss/v0.11.0/style.go), so `Cell`
is comparable and cell equality — the entire dirty test — is one `==`.

Rune cells with a combining-slice (tcell v2's model) are rejected: a `[]rune` field
destroys comparability and allocates per decorated cell, and the model still equates
"cell" with "code point + marks", which is exactly the desync class tcell v3 abandoned
it over.

### 2.2 The double buffer and diff

```go
// buffer is the double-buffered grid (unexported; owned by the runtime).
type buffer struct {
	w, h int
	curr []Cell // row-major, len w*h — what components painted this frame
	last []Cell // what the terminal is currently showing
}
```

The tcell model, adopted verbatim: **the last-frame copy *is* the dirty tracking** —
`dirty(i) ≡ curr[i] != last[i]`. No separate bitmap or dirty list exists to fall out of
sync with reality.

Diff walk (produces ADR-0002's `[]CellUpdate`, row-major):

1. Skip clean cells (`curr[i] == last[i]`).
2. Skip continuation cells whose head cell is also dirty (the head's emission covers
   both; a continuation can only *be* dirty alongside its head — §2.3 invariant).
3. Emit dirty runs; on emission, `last[i] = curr[i]`.

The emitter (in `tui/term`, consuming the diff) applies two byte-economy rules from
tcell's `Show()`: the cursor is repositioned (`CUP`) **only when the next dirty cell is
discontiguous** with the implicit advance, and SGR is re-emitted **only when style
changes** between consecutively emitted cells. `TestBackend` applies the diff
structurally and ignores byte economy — the rules are term-emitter concerns, tested
against a captured output stream.

### 2.3 Wide-cell boundary invariants

A width-2 cluster occupies a **head cell** (`Content=cluster, Width=2`) and a
**continuation cell** (`Content="", Width=0`) immediately to its right. Invariants,
enforced by the buffer's write path (not by callers):

- **W1 — No orphan halves.** Writing over *either* half of a wide pair clears **both**:
  the surviving half becomes a space cell (width 1, writer's style for the overwritten
  position, head's style for the freed sibling). Overwriting one half of a CJK cell and
  leaving the other is the classic corruption (dossier §8).
- **W2 — No split emission.** A damage region never splits a cluster: if a diff run
  would start at a continuation cell or end on a head cell, it is widened to include
  the full pair. (Follows structurally from W1 — halves are only ever dirtied together
  — and is asserted, not assumed: `TestBackend.Flush` panics on an orphaned half,
  ADR-0002 §2.3.)
- **W3 — No wide write into the last column.** A width-2 cluster whose continuation
  would fall outside the clip or the grid is not written (the write is dropped whole —
  never half-painted). The right edge of a pane shows a dropped cluster as the
  underlying fill, which is correct-by-construction rather than corrupt.

### 2.4 `tui.Surface` — clipped views into the buffer

Components never see the buffer; they see a `Surface` (ADR-0001 §2.4 #5 — the second
portability seam).

```go
// Surface is what components render onto: a clipped, offset view into the
// frame's cell buffer, carrying the style-resolution context.
type Surface interface {
	// SetCell writes one grapheme cluster at surface-local (x, y).
	// content must be a single cluster; if it contains more than one,
	// only the first is written (callers use Graphemes to iterate text).
	// Width is measured internally (§2.7), under the Surface's width
	// policy, and cached on the Cell.
	// Writes outside the clip are silently dropped (W3 applies).
	SetCell(x, y int, content string, st style.Style)

	// Fill sets every cell in r (clipped) to content/st. Fill with a
	// width-2 cluster fills in steps of two columns; a trailing odd
	// column, if any, is filled with a SPACE cell in st — never left
	// untouched (rev 1; W3 still forbids a half-painted cluster).
	Fill(r Rect, content string, st style.Style)

	// Sub returns a child Surface clipped to r ∩ bounds, with r's origin
	// as the child's (0,0). Sub of Sub composes; the style context flows
	// to the child unchanged. Cheap: a view header, no cell copying
	// (the vaxis Window model — go.rockorager.dev/vaxis).
	Sub(r Rect) Surface

	Size() Size

	// StringWidth measures s under the App-configured width policy
	// (WithWidthPolicy, ADR-0005). NORMATIVE: components MUST measure
	// text through the Surface (or Context) — never the package-level
	// default — so the per-App policy is honored (§2.7).
	StringWidth(s string) int

	// Resolution context (ADR-0006): the theme, the negotiated terminal
	// capabilities, and the width policy travel WITH the surface, so
	// components and style resolution need no globals and tests can
	// inject all three.
	Theme() *style.Theme
	Caps() Capabilities
}
```

> **Rev 1 (Lector must-fix #3).** `StringWidth` added to `Surface`, and the width policy
> made part of the Surface's resolution context: a package-level function cannot close
> over a per-App option, so rev 0's measurement API made the policy unusable as
> specified. Components measure via Surface/Context — normative.

> **Rev 1 (Lector Q5 answer).** `Fill` with a width-2 cluster now paints the trailing
> odd column with a styled space instead of leaving it untouched — an untouched column
> risked a stale visual stripe in the common background-fill case.

Clipping semantics: all coordinates are surface-local; the clip is the intersection of
the surface's rect with its parent's clip (computed once at `Sub`). Out-of-clip writes
are dropped silently — clipping is a rendering fact, not an error (fail-loud applies to
*construction* mistakes, not per-cell paints in the hot path).

Cluster iteration for text painting is public core API, backed by
`tui/internal/grapheme`:

```go
// WidthPolicy selects the UAX #11 East Asian Ambiguous interpretation.
// Fixed once per App (WithWidthPolicy, ADR-0005); travels with the
// Surface's resolution context (§2.4).
type WidthPolicy uint8

const (
	WidthPolicyDefault       WidthPolicy = iota // Ambiguous = 1 (the default everywhere)
	WidthPolicyAmbiguousWide                    // Ambiguous = 2 (CJK legacy contexts)
)

// Graphemes yields the grapheme clusters of s, in order. SEGMENTATION
// ONLY — cluster boundaries are policy-independent (UAX #29 does not
// depend on width). Measure clusters via Surface.StringWidth or the
// functions below.
func Graphemes(s string) iter.Seq[string]

// StringWidth is the display width of s under WidthPolicyDefault —
// explicitly and only. Component code should prefer Surface.StringWidth,
// which applies the App-configured policy.
func StringWidth(s string) int

// StringWidthPolicy is the display width of s under an explicit policy.
func StringWidthPolicy(s string, p WidthPolicy) int
```

### 2.5 The flush path

Per frame, after the dirty subtree has repainted into `curr` (ADR-0004/0005):

1. Diff `curr` vs `last` (§2.2) into the reusable `[]CellUpdate` scratch.
2. If the diff is empty and cursor state is unchanged: **stop — zero bytes** (G2).
3. `Backend.Flush(diff)`. The term emitter accumulates into one reusable
   `bytes.Buffer`, in order:
   a. `CSI ?2026 h` — begin synchronized update — iff `Capabilities().SyncOutput`
      (https://gist.github.com/christianparpart/d8a62cc1ab659194337d73e399004036;
      terminals force-flush an unclosed bracket after a timeout, so a crash mid-frame
      cannot wedge the terminal);
   b. `CSI ?25 l` iff the cursor is currently visible (R1, §2.9);
   c. the dirty runs — CUP only on discontinuity, SGR only on style change (§2.2),
      conservative re-anchoring after risky clusters (§2.8);
   d. final cursor position (CUP), shape (DECSCUSR) if changed, and `CSI ?25 h` iff
      the latched state says visible (the IME rule, §2.9 R5);
   e. `CSI ?2026 l` iff opened.
4. **One `Write`** of the buffer (R3). The buffer and diff slice are retained and
   reused across frames — steady-state rendering allocates nothing (golib perf
   discipline; dossier §8).

Mode 2026 is emitted per-frame (bracketing), never latched on; terminals without it
simply get rule R1–R3 behavior, which is what bubbletea v1 shipped for years and is
acceptable (dossier §8).

### 2.6 Frame scheduling

Owned by the App loop (ADR-0005) but specified here because it is a rendering policy:

- **Render-on-dirty.** `Context.MarkDirty()` (ADR-0004) coalesces into a pending-frame
  flag; the loop is woken by the close-and-replace `chan struct{}` broadcast idiom from
  `server/registry.go` (close the channel under the mutex, replace it — every waiter
  wakes exactly once, late subscribers get the new channel).
- **Min-frame-interval.** Default 16 ms (~60 fps ceiling), an `AppOption`. A dirty mark
  inside the interval arms a timer for the boundary; further marks coalesce into the
  already-armed frame. This beats bubbletea v1's always-on ticker: busy apps cap at the
  interval, and **idle apps emit zero bytes and hold zero timers** (G2; bubbletea
  standard_renderer precedent inverted).
- **Resize (never diff across a size change).** On the resize delivered by the App's
  intake stage — which owns latest-wins coalescing of the backend's ordered
  `ResizeEvent`s (ADR-0002 §2.8 rev 1, ADR-0005): reallocate `curr`/`last` at the new
  size, emit one `ED 2` clear, invalidate `last` entirely, full relayout + repaint.
  Diffing against a wrong-sized previous frame is worse than repainting (dossier §8
  resize-storm analysis, https://github.com/manaflow-ai/cmux/issues/3831). During a
  storm the intake's latest-wins coalescing means intermediate sizes are never laid
  out; the **final size is always rendered**.

### 2.7 `tui/internal/grapheme` — generated Unicode tables

The one genuinely laborious zero-dep item (ADR-0001 §2.2), scoped precisely:

```go
package grapheme // tui/internal/grapheme — internal; public access via tui.Graphemes /
                 // tui.StringWidth / tui.StringWidthPolicy / Surface.StringWidth (§2.4)

//go:generate go run gen/gen.go -unicode 16.0.0
//
// gen.go downloads (or reads from a pinned local mirror) and compiles:
//   UCD/EastAsianWidth.txt          → eastAsian ranges (W, F, A classes)
//   UCD/GraphemeBreakProperty.txt   → break-property ranges (GB rules)
//   UCD/emoji/emoji-data.txt        → Extended_Pictographic (GB11), Emoji_Presentation
//   UCD/auxiliary/GraphemeBreakTest.txt → conformance test data (test file, not tables)
// into tables_gen.go: sorted, non-overlapping [lo, hi] rune ranges per
// property + binary search lookups (the runewidth/uniseg recipe).

// Next returns the first grapheme cluster of s, its display width, and the rest.
func Next(s string, eastAsian bool) (cluster string, width int, rest string)

// Width measures a single cluster.
func Width(cluster string, eastAsian bool) int

// StringWidth sums cluster widths.
func StringWidth(s string, eastAsian bool) int
```

**Segmentation:** UAX #29 extended grapheme clusters — the GB rules driven by the
break-property table, with GB11 (Extended_Pictographic ZWJ sequences) and GB12/13
(regional-indicator pairs) requiring the emoji and RI data. Conformance is defined as
passing every line of the pinned Unicode version's `GraphemeBreakTest.txt`, which the
generator turns into a table test.

**Width (wcwidth model, dossier §1):** per-rune width ∈ {0, 1, 2} — 0 for combining
marks (Mn/Me), ZWJ/ZWNJ and default-ignorables; 2 for East Asian Wide/Fullwidth and
Emoji_Presentation; else 1. **Cluster width = width of the first non-zero-width rune**,
with overrides: an RI pair (flag) = 2; trailing VS16 forces 2; trailing VS15 forces 1.
East Asian **Ambiguous = 1 by default**, 2 when `eastAsian` is set. The public surface
of this switch is `tui.WidthPolicy` (§2.4): the App fixes it once via `WithWidthPolicy`
(ADR-0005) and it travels with the Surface's resolution context; package-level
`tui.StringWidth` is pinned to `WidthPolicyDefault`, `tui.StringWidthPolicy` takes the
policy explicitly, and `tui.Graphemes` is segmentation-only and policy-independent. The
policy is always an explicit parameter or Surface/Context state — never a global.

> **Rev 1 (Lector must-fix #3).** Rev 0 claimed the package-level wrappers "close over"
> the per-App option — impossible for a package-level function, which left the policy
> uncallable. It is now an explicit, named API: `WidthPolicy`, `StringWidthPolicy`, and
> `Surface.StringWidth` (§2.4).

**Committed tables, documented refresh.** `tables_gen.go` is committed (no network at
build time; ADR-0001 accepts the ~100–200 KB cost). The generated header records the
Unicode version and source-file hashes. Refresh procedure, documented in the package
README: bump `-unicode` in the go:generate line → `go generate ./tui/internal/grapheme`
→ conformance + width tests must pass → commit tables with the version bump named in
the message. Refresh cadence follows Unicode releases (~annual) and is a routine PR,
not an event.

### 2.8 Conservative emission without mode 2027

The residual risk after correct tables is **disagreement**: the terminal's width
opinion for a cluster differs from ours (🧑‍🌾 advances 2/4/5/6 columns depending on
emulator; VS16 and flags similarly; tmux compounds it —
https://mitchellh.com/writing/grapheme-clusters-in-terminals). Where the probe reports
`UnicodeCore` (mode 2027 / Terminal Unicode Core —
https://github.com/contour-terminal/terminal-unicode-core), the terminal has committed
to cluster semantics and our advance arithmetic is trusted. Elsewhere:

- A cluster is **risky** iff it is multi-rune and contains ZWJ, VS15/VS16, or an RI
  pair (single-rune wide CJK is *not* risky — every terminal agrees on it).
- After emitting a risky cluster, the emitter **does not trust the implicit cursor
  advance**: the next emission in the run is preceded by an absolute `CUP`, and the
  frame's final cursor position is likewise set absolutely (which §2.5(d) does
  unconditionally anyway).
- Cost: one CUP (~8 bytes) per risky cluster on legacy terminals; zero on 2027/kitty/
  ghostty-class terminals. Correctness of *our own grid* never depends on the
  terminal's opinion — only emitted-byte efficiency does.

This is strategy, not heroics: the grid is ours; the terminal is re-anchored whenever
its agreement is in doubt.

### 2.9 Normative anti-flicker and cursor rules

The dossier §8 flicker checklist, promoted to numbered rules code and reviews cite
(Textual's rule set; bubbletea's renderer does R1–R3, its v2 added R4):

- **R1 — Hide the cursor during paint.** The cursor is hidden (`?25l`) before any cell
  emission and re-shown only per R5, within the same write.
- **R2 — Never clear-then-redraw.** No `ED`/`EL` before painting; dirty cells are
  **overwritten** in place. Full-screen clear is emitted in exactly one situation:
  the resize repaint (§2.6). Trailing-shrink blanking is expressed as space cells in
  the diff, not erase sequences.
- **R3 — One frame, one `Write`.** The entire frame — brackets, cursor ops, cells —
  is accumulated and hits the fd as a single `Write` syscall (ADR-0002 §2.1 latched
  cursor exists to serve this rule).
- **R4 — Bracket with mode 2026 when the capability is present.** Begin/end
  synchronized-update around every frame (§2.5); never latched on.
- **R5 — The IME rule.** When a text-input component has focus, the **real hardware
  cursor is parked at that component's insertion point and shown**. Frameworks that
  hide the real cursor and draw a fake styled one break OS IMEs, which anchor the
  composition/candidate window to the hardware cursor (CJK candidate windows at (0,0) —
  https://github.com/xtermjs/xterm.js/issues/5734), and screen readers. The focused
  component reports its cursor position and desired shape up the tree (ADR-0004 focus
  chain; bubbletea v2 made the cursor a declarative View field for exactly this
  reason); the frame emits it via §2.5(d). When no focused component claims an
  insertion point, the cursor stays hidden after paint.

## 3. Consequences

**Positive**

- The dirty test is one comparison of a comparable struct; the diff needs no auxiliary
  state; a whole class of "dirty bitmap out of sync with buffer" bugs is
  unrepresentable (tcell's decade-proven model).
- Grapheme-cluster cells make torn emoji/CJK structurally impossible inside our grid
  (W1–W3 are buffer invariants, not caller obligations), and §2.8 bounds the damage
  from terminals that disagree with our widths.
- Byte-minimal, single-write, 2026-bracketed frames give flicker-freedom on modern
  terminals and best-effort (R1–R3) everywhere else — and idle apps emit literally
  nothing, unlike ticker-driven renderers.
- The Surface seam keeps components ignorant of the buffer, the backend, and global
  state — the property that makes `TestBackend` frames cell-exact and ADR-0007 widgets
  trivially testable.

**Negative (costs)**

- ~100–200 KB of committed tables and a `go:generate` toolchain with an
  us-owned Unicode refresh duty (accepted in ADR-0001 §3; the procedure in §2.7 makes
  it routine).
- `Cell.Content string` costs a string header per cell (16 bytes) versus tcell v2's
  packed rune; a 200×60 double buffer is ~1 MB of headers + shared backing bytes.
  Accepted: interning of the common single-ASCII-cell case is a measured-later
  optimization that the comparable-struct design does not preclude.
- Width cached at write time means a UCD table change (refresh) or a different
  `WidthPolicy` can't retroactively re-measure a live buffer — irrelevant in practice
  (both are fixed before an App runs).
- The min-frame-interval adds up to one interval of latency to a burst's *first* paint
  only when a frame was just emitted; accepted as the standard coalescing trade.

**Evolution**

- Graphics cells (sixel/kitty images) extend `Cell` with an optional payload behind
  the same diff (vaxis precedent); `Surface` grows nothing.
- Per-component damage rects (N2) can be layered above the diff without changing the
  buffer contract if profiling ever demands it.
- Mode 2027 adoption improves §2.8 automatically — the conservative path simply fires
  on fewer terminals as `UnicodeCore` spreads.

## 4. Alternatives considered

1. **Rune + combining-slice cells (tcell v2).** Rejected: non-comparable cells (slice
   field) break the `==` dirty test, allocate per decorated cell, and still model
   "character = codepoint + marks", the exact abstraction tcell v3, vaxis, and
   ultraviolet all abandoned for grapheme strings
   (https://mitchellh.com/writing/grapheme-clusters-in-terminals).
2. **Line-granular diff over rendered strings (bubbletea v1's standardRenderer).**
   Rejected: any change rewrites whole lines (byte-heavy for dense lazygit-class UIs),
   forces per-frame string splitting and ANSI re-measurement (the ~25%-of-CPU lipgloss
   string-ops profile — https://eieio.games/blog/secure-massively-multiplayer-snake/),
   and a string pipeline is what bubbletea v2 itself replaced with cells.
3. **A separate dirty bitmap / dirty list.** Rejected: it is redundant state that must
   be kept coherent with `curr`/`last` under every write path including W1's sibling
   clears; tcell's compare-on-walk needs no bookkeeping and the walk is memory-bandwidth
   cheap at terminal sizes.
4. **Always-on frame ticker (bubbletea v1, 60 fps).** Rejected: idle apps must emit
   zero bytes and schedule zero wakeups (G2) — an idle ssh'd dashboard should cost
   nothing; render-on-dirty with an interval floor gives the same busy-case cap.
5. **Taking uniseg + runewidth as dependencies (or vendoring uniseg wholesale).**
   Rejected for v1 per ADR-0001 §2.2/Q4: generation from the UCD gives provenance,
   size control (only EAW + grapheme-break + emoji properties; uniseg also carries
   word/sentence/line tables we don't need — N4), and zero-dep purity. Vendoring
   remains the documented fallback if generator maintenance proves costlier than
   expected (ADR-0001 Q4 is still open for the reviewer).
6. **Immediate-mode full repaint every frame, diff only at the terminal (gocui's
   shape).** Rejected as the *scheduling* model: re-running full layout+paint on every
   event burns CPU for static screens; dirty-driven repaint of a retained tree renders
   only what changed (ADR-0001 §2.4 #1) and still ends in the same cell diff.
7. **Trusting terminal cursor advance everywhere (no §2.8 re-anchoring).** Rejected:
   the desync failure is silent, cumulative, and off-by-N for the whole remaining row
   (dossier §8); ~8 bytes per risky cluster on legacy terminals is a trivial premium.
8. **`Surface` as a concrete struct instead of an interface.** Tempting (devirtualizes
   the hot `SetCell` path), but ADR-0001 §2.4 #5 fixes Surface as the second
   portability seam, and TestBackend-style instrumented surfaces plus future non-cell
   implementations need the interface. Revisit only with profiles in hand (see Q5).

## 5. Acceptance criteria

1. `tui/internal/grapheme` passes every case of the pinned Unicode version's
   `GraphemeBreakTest.txt`, plus width spot-checks: `👩‍👩‍👦` (ZWJ family) = 2, `🇦🇺` (RI
   pair) = 2, `❤️` (VS16) = 2, `‍text☂︎` (VS15) = 1 for the cluster, `世` = 2, `é`
   (combining) = 1; ambiguous `±` = 1 under `WidthPolicyDefault` and 2 under
   `WidthPolicyAmbiguousWide`.
2. Tables are committed; `go generate ./tui/internal/grapheme` is reproducible from
   pinned UCD inputs (hash-recorded in the generated header) and a version bump +
   regenerate + green tests is the documented refresh procedure.
3. Property test (fuzzed `SetCell`/`Fill`/`Sub` sequences over random sizes): after
   every operation, no orphaned continuation cell exists — every `Width==0` cell has a
   `Width==2` head immediately left (W1); no width-2 head occupies a last column (W3).
   `TestBackend.Flush` re-asserts the invariant on every applied diff.
4. Golden diff tests: scripted buffer mutations produce the exact expected
   `[]CellUpdate` sets; the term emitter's byte stream for those diffs shows CUP only
   at discontinuities and SGR only at style changes (captured-writer assertions).
5. One-write rule: a counting writer observes exactly one `Write` per non-empty frame;
   an untouched frame performs no `Write` at all; frames on a `SyncOutput` profile
   begin with `?2026h` and end with `?2026l` in the same write.
6. Frame scheduling under a fake clock: N dirty marks inside one min-frame-interval
   yield one frame; an idle App arms no timers; a resize storm (rapid
   `InjectResize` sequence) renders only the final size, preceded by exactly one full
   clear, with `last` fully invalidated (no cross-size diff — verified because the
   post-resize diff equals the full grid).
7. R5: with a focused text input (ADR-0007's), `TestBackend.CursorPos()` reports the
   insertion point and `visible == true`; with no focused input, `visible == false`
   after every frame.
8. Benchmarks committed alongside (`_test.go` co-located, std testing): full-repaint
   diff of a 200×60 grid, steady-state dirty-region frame, and `StringWidth` over
   mixed ASCII/CJK/emoji corpora; the steady-state frame path allocates zero bytes
   amortized (reused scratch — asserted with `testing.AllocsPerRun`).
9. Policy plumbing (rev 1): with an App configured
   `WithWidthPolicy(WidthPolicyAmbiguousWide)`, `Surface.StringWidth` measures
   ambiguous-width text at 2 columns per ambiguous cluster while package-level
   `StringWidth` still measures 1 — proving the policy flows through the Surface
   context and the package default stays fixed. `Fill` with a width-2 cluster over an
   odd-width rect paints the trailing column as a space cell in the fill's style — no
   untouched stripe (§2.4).

## 6. Questions for the reviewer

- **Q1.** `Cell.Content string` with `""`-continuation and cached `Width uint8`
  (§2.1): an alternative packs the common case (single ASCII rune) into a fixed
  `[4]byte`+overflow-string encoding to eliminate per-cell string headers (~1 MB at
  200×60 double-buffered). We chose the plain string for simplicity and comparability
  and deferred interning as a measured optimization — is the memory profile acceptable
  to freeze, given `Cell` is exported and its layout is therefore API?
  — **Lector r1:** accepted — keep `Cell.Content string` for v1; the plain comparable
  struct is more valuable than packing complexity before profiles exist.
- **Q2.** East Asian Ambiguous width as a per-App boolean option defaulting to 1
  (§2.7): should the driver instead attempt runtime detection (e.g. cursor-position
  probe after emitting an ambiguous character during ADR-0002's startup probe, the way
  some editors measure), which auto-fixes CJK-locale legacy terminals at the cost of
  one more probe round-trip and a fuzzier capability model?
  — **Lector r1:** no runtime probing in v1 — explicit per-App policy, with the
  measurement API fixed so the policy is actually usable. Folded as must-fix #3
  (§2.4, §2.7).
- **Q3.** §2.8 re-anchors after **every** risky cluster on non-2027 terminals. A
  cheaper variant re-anchors once per dirty *run* (at run end), betting that
  disagreement inside a run only corrupts that run's remainder until the next CUP. Per
  cluster is maximally safe and costs ~8 bytes each; per-run halves the overhead on
  emoji-dense rows. Is per-cluster the right default, and should it be tunable at all
  (lean: no tunable — one behavior, testable)?
  — **Lector r1:** keep per-risky-cluster re-anchoring and do not make it tunable; one
  tested behavior beats a byte-saving knob.
- **Q4.** `Surface.Theme()`/`Caps()` put the style-resolution context on the render
  seam (§2.4), which means resolved colors can differ per-backend at `Render` time. The
  alternative resolves styles once per frame in a pre-pass, letting `Surface` carry
  nothing and cells store pre-resolved attributes. We chose context-on-Surface for
  testability and simplicity; does the reviewer see a problem for ADR-0006's adaptive
  (light/dark) colors — e.g. an OSC 11-reported background change mid-session
  invalidating cached cells, which the current design handles by full repaint?
  — **Lector r1:** confirmed — Surface-carried theme/caps is right; it keeps render
  tests simple, and theme/capability changes are handled by invalidating cells (full
  repaint).
- **Q5.** `Fill` with a width-2 cluster leaves a trailing odd column untouched (§2.4
  W3). Alternatives: fill the odd column with a space in the fill's style (visually
  cleaner for background fills), or reject wide-cluster fills entirely (fail loud at
  the call). The untouched-column choice is the least surprising for the diff but can
  leave a stale stripe if the caller assumed full coverage — which semantic should be
  frozen?
  — **Lector r1:** CHANGED — fill the trailing odd column with a styled space; an
  untouched column risks stale visual stripes in the common background-fill case.
  Folded in rev 1 (§2.4, §5.9).
