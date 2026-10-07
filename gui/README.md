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
  Hangul, CJK and symbols. Colours, bold, italic, faint, underline,
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

## Development

The module builds against a released golib, with no `replace`. To work across
both modules, use an uncommitted workspace in this directory:

```sh
go work init . ..
```

A `go.work` here is invisible to the root module, so its CI stays cgo-free.
