# Implementing in golib/gui

The rules every change to `gui` and its sub-packages (`widget`, `decl`, `mermaid`, `vegalite`,
`flow`, `svg`, `record`, `internal`) follows. "Must" and "never" admit no exceptions without an ADR
that names the rule it overrides. Where code today breaks a rule, fix it when you touch it, and never
copy the broken pattern.

**gui also follows `tui/IMPLEMENTATION.md` in full** — capabilities as interfaces (§2), one home per
mechanism (§3), responsibility and size (§4), extension by table (§5), phases and threading (§6),
options/errors/enums/styles (§9), tests (§12), comments (§13). This file adds only what is gui's own.

## 1. The release boundary

`gui` is its own module and builds against a **released** golib (`gui/go.mod`, no `replace`).

- **Never copy tui code into gui.** When gui needs a tui mechanism that is unexported, or a
  capability tui lacks, export it in tui, release golib, then raise gui's golib version. A copy
  "until the release" is never made: it diverges (it already has, in the palette, the editor's event
  routing, the document view and the diagram host).
- A change that needs both sides lands as two changes in that order: tui (released), then gui.
- `gui/record` and other toolkit-free sub-packages import no Gio; their dependency tests
  (`record/deps_test.go`) keep it so.

## 2. Behaviour lives in tui; gui supplies pixels

- A native widget **binds** a tui widget and draws it; it never re-implements its state, keys,
  routing or state machines. Behaviour reaches gui only through tui's interfaces: `pixelLayout`
  is a `tui/widget.EditorLayout`, `tableLayout` implements the `TableLayout*` interfaces,
  `BindTable`/`BindHTML` bind rather than fork.
- **A native painter reads a tui widget's render state through a capability interface tui exports**
  (selection span, fill fraction, checked state, caret, scroll). It never re-derives layout from the
  widget's cells, never type-switches on concrete tui widgets, and never keeps package-global maps
  keyed by widget.
- Every native widget keeps its cell `Render` fallback, paired with `NativeView()`/`NativeScope()`:
  the terminal build must still draw it.

## 3. The canvas

`Canvas` is the drawing abstraction; `gioCanvas`, `SVGCanvas` and `RecordingCanvas` implement it.

- **Every drawing feature works on every Canvas.** An optional fast path is an interface plus a free
  helper that answers false when absent (`CellCoverer`/`CellCovered`, `streamDrawer`/`DrawStream`).
  Code never asserts `*gioCanvas`, `*SVGCanvas` or `*RecordingCanvas`; production never names a test
  canvas.
- **Draw by runs, never per cell or per glyph**; batch shapes into one `Path` and one draw call.
  New paint paths are counted under the `gui_opcount` build tag.
- Colours come from composed, uncovered cells (`OpenCellColors`), never from a fixed cell.

## 4. Colour and look

- **One palette.** Theme tokens resolve through a canvas colour resolver fed by the configured
  palette from `tui/style`; never `DefaultTheme()` as the palette, never a local copy of the
  xterm-256 cube, the 16-colour table or a luminance formula.
- **Named look constants** — line height, minimum text size, accent, diagram font scale — live once,
  as named values; no bare `1.2`, `10`, `0x5c9cf5` at the call site.
- **One look-hash helper** for cache keys; caches that depend on shaping are keyed on
  `Fonts().Gen` as `canvas.go` requires.

## 5. Renderers (`mermaid`, `vegalite`, `flow`, `svg`, HTML)

Every renderer has the same shape:

```go
func Lay(ctx context.Context, src string, width float32, th Theme, m gui.Measurer) (*Laid, error) // off the loop
func (l *Laid) Paint(c gui.Canvas)                                                               // on the loop
```

- `Lay` runs off the loop with a pooled `AcquireMeasurer`, checks `ctx.Err()` inside its loops, and
  returns plain values; `Paint` shapes lazily. Results come back through `ctx.Go` with a generation
  check.
- **Hostile input is bounded** by named constants; every loop has a bound (dash walkers, series,
  shapes, iterations). Limits fail with wrapped sentinels (`ErrTooLarge`, `ErrUnsupported`) that the
  caller handles with `errors.Is` and a fallback.
- **Shared mechanisms have one home** in gui: the picture base (fit, label cache), the dash walker,
  CSS colour/length/declaration parsing (`gui/internal`). A renderer never keeps its own copy.
- **Kinds register themselves** (a package registry filled by each kind's file `init`); adding a
  diagram or chart kind edits no central switch.
- The diagram-request host (pending, stale, ready) exists once and serves every view that shows
  diagrams.

## 6. Threading

- Each struct comment states which goroutine owns each field ("tui loop", "Gio goroutine"); each
  method's doc says where it may be called.
- Gio-side state is touched only on the Gio goroutine; tui-side state only on the loop; they meet
  through the backend's documented hand-off, never shared mutable fields.

## 7. Errors and invalid arguments

- Package-prefixed sentinels (`"gui: …"`, `"mermaid: …"`), wrapping golib `errs` categories; never
  an inline `errors.New` for an error a caller may test.
- Runtime setters clamp; constructors and `With*` options panic on programmer errors through
  `errs.Fatal` — the same policy for every setter (no setter panics where its siblings clamp).

## 8. Tests and docs

- Renderers and painters are tested headlessly against `RecordingCanvas`/`SVGCanvas` and the real
  `AcquireMeasurer`; test names are sentences.
- Doc comments are definitional ("X is …") and state the invariant; a file with a pipeline opens with
  its ASCII diagram. Docs describe the code as it is: a supported subset, a platform list, a parameter
  name is updated in the same change as the code.
- A change to an API, a pattern or a behaviour updates, in the same commit, every user-facing
  document that shows it (`README.md`, `doc.go`, examples), so none teaches an outdated usage.

## 9. Before you open a PR

- [ ] Nothing copied from tui; a missing tui export is a tui change and release first (§1).
- [ ] No re-implemented tui behaviour; painters use tui render-state capabilities (§2).
- [ ] Works on every Canvas; no concrete canvas assertions; draws by runs (§3).
- [ ] Colours from the one palette; look values named; caches keyed on `Fonts().Gen` (§4).
- [ ] Renderer: `Lay` off the loop, bounded, cancellable; shared parsers and picture base (§5).
- [ ] Everything in `tui/IMPLEMENTATION.md` §14.
