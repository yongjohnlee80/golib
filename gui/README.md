# gui

A [golib/tui](../tui/README.md) `Backend` that is a native window, drawn with
[Gio](https://gioui.org). A tui program runs on it **unchanged**: it picks this
backend instead of a terminal's, and every cell it paints is drawn natively.

```go
func main() {
	gui.Main(func() error {
		app := tui.NewApp(root, tui.WithBackend(gui.NewBackend(gui.WithTitle("editor"))))
		return app.Run(context.Background())
	})
}
```

`gui` is its own Go module (`github.com/yongjohnlee80/golib/gui`), so a
TUI-only golib user never resolves Gio or cgo. `tui` cannot import it.

## What the window does

- **Cells, natively.** Glyphs are shaped in a monospace font (Go Mono by
  default; `WithFont` picks another), with system fonts as the fallback for
  Hangul, CJK and symbols. The font changes while the window runs:
  `Backend.SetFont(typeface, size)`, `SetZoom(percent)` (50 to 300, everything
  sized by the window's text follows) and `SetProseFont(family)` (what a native
  view's `Font` with no `Family` draws in; `gui.CellFamily` names the cell font).
  Each change lays the window out and paints it again, a change of family alone
  included. `gui.Families(mono)` lists the installed families, where fontconfig is. Colours, bold, italic, faint, underline,
  strikethrough and reverse are drawn as such. Box-drawing and block characters
  are drawn from the cell's geometry, so borders join without seams at any
  scale.
- **Units translate.** The grid is the window's size divided by the cell size
  at the display's scale. A widget keeps its own cell coordinates; the pointer
  is mapped back to cells.
- **Every claimed capability works as on a terminal.** Truecolor, the kitty
  keyboard protocol's detail (Ctrl+I is not Tab; key releases), bracketed
  paste, the mouse, kitty-graphics images (`widget.Image` draws natively),
  copy to the system clipboard, the cursor's shape and colour.
  `SyncOutput`, `InBandResize`, `UnicodeCore` and `Undercurl` are reported
  false: they name terminal protocol features.
- **tui widgets in pixels.** `gui/decl.Native()` gives a tui/decl program gui's
  `Editor`, lays an `HTMLView` out in pixels (`gui/widget.BindHTML`), and draws a
  cell-cursor `TableView`'s `DataTable` natively over the same `TableCore`
  (`gui/widget.BindTable`): its header, rows, cell cursor, selection and sort
  marks in the window's prose font, the keys and the paging the core's, as in a
  terminal. A row-cursor `TableView` stays cells. The native table:
  - **Row numbers.** With `TableRowNumbers` (decl's `rowNumbers`) each row's
    number, from 1, is drawn right-aligned in a gutter before the first field, in
    the header's bold, muted text on the header's tint. The gutter is as wide as
    the last row's number (`TableCore.RowNumberDigits`), so it widens as rows
    arrive, 9 rows to 10, and every field moves right with it. The header's
    corner of the gutter is blank.
  - **The pointer.** Its layout implements `TableLayoutHitter`: `HitAt` answers
    `TableHitGutter` over a row's number, `TableHitHeader` with the field under
    the pointer over the titles, `TableHitCell` over a cell, and `TableHitNone`
    at the gutter's corner, past the last field and below the last row. So a
    press on a title selects that field, every loaded row, a drag over the
    gutter selects whole rows, and a drag over cells selects them in reading
    order. The window reports a drag as `MouseMotion` with `MouseLeft`, and the
    table holds the pointer (`Context.CapturePointer`) until the release.
    `CellAt` still answers as before, and a row's number is no cell to it.
  - **The blurred cursor.** The cursor's cell is filled in
    `DataTable.CursorStyle()`: `Cursor` while the table has the keys,
    `CursorBlurred` while it has not, repainted when the focus moves. A style's
    ground is its background (its foreground when it reverses), as decl's
    palette gives `highlight` and `inactive.highlight`; the default styles have
    none, and fill the accent's band, or a faint grey one while blurred.
  - **The selection kinds.** Each cell is filled as `TableCore.InSelection`
    decides, not as the `Selection()` box: `v` (Cells) runs in reading order,
    the anchor's row from its field and the cursor's up to its field; `V` (Rows)
    fills whole rows; Ctrl+V (Block) the rectangle. Adjacent selected fields in
    a row are one fill, and a run the row above has too is the same fill, grown:
    a selection is one to three fills.
  - **Sort marks.** Every key in `SortedKeys` marks its field, ▲ or ▼ in the
    gap after it, as the terminal's DataTable marks them (a field keyed twice
    shows its first key's mark). With more than one key each mark carries the
    key's place, small (▲¹ ▼²). The first key's title says "(loaded rows)" while
    rows remain to load, when it fits.
- **Paste and IME.** Ctrl+Shift+V (Cmd+V on macOS) pastes, as in a terminal
  emulator, so Ctrl+V stays the app's. An input method's composition is drawn
  at the caret, and its candidate window opens there.

## Try it

```sh
cd gui
go run ./examples/demo     # tui/examples/demoapp in a window
go run ./examples/scene3d  # a 3D scene on its own GPU device, in a pixel pane beside cells
```

`scene3d` renders on Vulkan (Linux) or Metal (macOS) when the window draws its
frames without a copy, and on the CPU otherwise; `-producer=cpu` forces the
CPU. Its sidebar says which, and the frame rate is in the pane's corner.

Linux needs cgo and the Wayland/X11 development libraries:

```sh
# Arch
pacman -S wayland libx11 libxkbcommon libxcursor libxfixes vulkan-headers mesa
# Debian/Ubuntu
apt install libwayland-dev libx11-dev libx11-xcb-dev libxkbcommon-x11-dev \
  libgles2-mesa-dev libegl1-mesa-dev libffi-dev libxcursor-dev libvulkan-dev
```

## Status

Linux (Wayland, X11) and macOS. v0.2.0 adds pixel-root panes, raw keys, and
streams of frames, canvases and mutable images (ADR 1792306654, which amends
ADR 1791330692). Not yet: a native view spanning panes, and layer caching.
Accessibility: Gio's semantic operations have no desktop consumer yet, so the
window has no screen-reader support.

## Panes, pixel views and frames

A window can be more than one cell grid. `WithPanes` makes its root a tree of
rows and columns in dp, whose leaves are **cell panes** and **pixel panes**:

```go
panes := gui.NewPanes(gui.Row(
	gui.Sized(gui.Fixed(280), gui.Cells(sidebar)),   // a tui component, in cells
	gui.Sized(gui.Flex(1), gui.Pixels(viewport)),    // draws itself natively
))
app := tui.NewApp(panes, tui.WithBackend(gui.NewBackend(gui.WithPanes(panes))))
```

- **tui stays cell-only.** Each pane gets its own band of one virtual cell
  grid. tui lays out, diffs and routes in cells as ever; the backend maps each
  band to its pane's pixels, and the pointer back to the pane's cells.
- **A native view stays in its pane.** It is clipped to the pane it is anchored
  in, and so is its input. A dialog over a game is drawn in the game's pane.
- **Raw keys.** `Backend.SetRawKeys(true)` gives a game its keyboard. Every key,
  letters, space and lone modifiers included, arrives as a press and a release.
  The input method is off. Text mode, the default, delivers printable keys as
  text. A game view typically turns it on as it gains focus and off as it
  loses it. Stopping the window ends input: held keys get no releases.

### Streams: pictures at the display's pace

A view that changes every frame (video, a game's world, a 3D scene) does not
go through a tui flush. It records a **slot** once, and the window draws the
stream's newest picture there at every vsync after a push:

| Stream | What it carries | Draw it with |
|---|---|---|
| `Backend.NewStream` | `Frame`s: decoded video, another GPU device's renders, pixels in memory | `DrawStream` |
| `Backend.NewCanvasStream` | pictures a `Canvas` records on the producer's goroutine | `DrawCanvasStream` |
| `Backend.NewMutableImage` | an RGBA image edited in place; each `Update` uploads only the rectangle it changed | `DrawMutableImage` |

Every push returns a `Receipt`, which completes exactly once:

- `Drawn`: it was in a presented frame. `When` is that frame's time; pace by it.
- `Superseded`: a newer push replaced it before it was drawn.
- `NotShown`: a frame was presented without its slot (the view is hidden). The
  picture stays pending.
- `Unsupported`: the window draws no frame like it. Ask `FrameCaps` again.
- `Closed`: the stream was closed, or the window stopped.
- `Busy`: a `CanvasStream` was already recording, so `draw` was not called.

A `Frame` is `op/extimage`'s, from the Gio fork. Its contract (descriptor,
lease, release exactly once, caps) is the one every driver implements; read
[EXTIMAGE.md](https://github.com/yongjohnlee80/gio/blob/main/EXTIMAGE.md)
before writing a producer. `Backend.FrameCaps` says which frames this window
draws, and whether each draws with or without a copy:

- **Zero-copy:** Vulkan imports Linux DMA-BUFs, Metal imports macOS IOSurfaces.
  Vulkan on Linux is opt-in per window (`WithZeroCopyFrames`); without it, or
  where Vulkan is unavailable, the window keeps EGL. macOS uses Metal already.
- **Copy:** every other renderer draws the same frames through the CPU driver,
  which maps the buffer and uploads it. Same contract, same code in the app.

## Drawing performance: count the draw calls

> **⚠ A frame costs what it replays, op by op, not the pixels it covers.** Gio
> replays every recorded operation on every frame. Each op is a uniform upload
> and a draw call; each clip path also needs stencil packing. A thousand small
> rectangles cost far more than one large one. **Draw by runs. Never draw per
> cell, per glyph or per item in a hot path.**

**tui widgets get this for free.** Cells go through `drawSpan` (`paint.go`),
for rows and for `PaintCells`. Per row, it:

- fills runs of one background colour;
- shapes one glyph path per colour;
- merges underline, strike and box-line pieces into rectangles.

Rows are re-recorded only when dirty. Any widget that renders cells needs no
special care.

**Native code must batch itself.** This covers `Painter`s, `View`s, Mermaid
and the markdown view. Each `Canvas` call is replayed on every frame:

1. Merge adjacent same-colour fills; lay out text per line or block, never per
   character. A line of texts each at its own x, such as a table row's cells, is
   one layout: `TextShaper.LayoutRuns`, drawn by one `DrawText`. `BindTable`
   draws each row band so, and shapes a band again only when it changes. A
   band's row number, in another font and colour, is its second layout; the
   header is one.
2. Never call the Canvas per cell: use `PaintCells`.
3. Prefer cells where a widget can be cells.
4. Paths and rounded rectangles are stencils: keep them out of loops over many
   items.
5. Shape text and lay out on change, not on paint.
6. Request a frame only when something changed; stop when an animation stops.

`FillRect` is currently drawn as a path, so it costs one. Making it a plain
rectangle where the transform allows is planned.

Check a change by counting ops per frame, full screen on a large monitor. A
small window hides a cost that grows with area. `go test -tags gui_opcount`
counts them: fills, glyph paths, colour glyphs, images, clips and transforms.
Each recorded macro (a row, a frame segment, a canvas picture) is credited
with its own ops wherever it is replayed. `opbudget_test.go` holds:

- a full-window frame to the batched budget: fills plus one glyph batch per
  colour per row, and no glyph drawn alone;
- a 1920×1080 window's stream frame, through the real `present`, to at most
  the flushed frame's ops plus 4 per slot;
- a hundred stream frames to no row recorded and no view painted.

**Streams cost per slot, not per cell.** A push re-records nothing: the frame
replays its recorded rows and views, and draws the newest picture into each
slot. A slot is one texture draw. The pixel copy, when the frame needs one, is
the frame's own size. `batch_pixels_test.go` checks
that batched drawing matches per-cell drawing, within a small tolerance.

## Development

The module builds against a released golib, with no `replace`. To work across
both modules, use an uncommitted workspace in this directory:

```sh
go work init . ..
```

A `go.work` here is invisible to the root module, so its CI stays cgo-free.
