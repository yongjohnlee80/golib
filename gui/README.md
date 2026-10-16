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
```

Linux needs cgo and the Wayland/X11 development libraries:

```sh
# Arch
pacman -S wayland libx11 libxkbcommon libxcursor libxfixes vulkan-headers mesa
# Debian/Ubuntu
apt install libwayland-dev libx11-dev libx11-xcb-dev libxkbcommon-x11-dev \
  libgles2-mesa-dev libegl1-mesa-dev libffi-dev libxcursor-dev libvulkan-dev
```

## Status

M0 of ADR 1791330692 (golib/gui): the backend, on Linux. Next: native GUI
widgets drawn in pixels over a tui node's cells (`NativeReporter`), with a mock
TUI or "not supported in TUI" fallback on a terminal; window chrome; macOS.
Accessibility: Gio's semantic operations have no desktop consumer yet, so the
window has no screen-reader support.

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
small window hides a cost that grows with area. `batch_pixels_test.go` checks
that batched drawing matches per-cell drawing, within a small tolerance.

## Development

The module builds against a released golib, with no `replace`. To work across
both modules, use an uncommitted workspace in this directory:

```sh
go work init . ..
```

A `go.work` here is invisible to the root module, so its CI stays cgo-free.
