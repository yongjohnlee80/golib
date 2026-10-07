// Package gui is a golib/tui Backend that is a native window, drawn with Gio (gioui.org).
//
// A tui program runs on it unchanged: it chooses this backend instead of a terminal's, and every
// cell it paints is drawn natively — glyphs shaped in a monospace font, colours, attributes, box
// drawing as real lines, images, a caret — at the window's display scale. The cell grid is the
// window's size divided by the cell size, so a tui widget keeps its own cell coordinates and the
// backend translates them to pixels.
//
//	func main() {
//		gui.Main(func() error {
//			app := tui.NewApp(root, tui.WithBackend(gui.NewBackend(gui.WithTitle("editor"))))
//			return app.Run(context.Background())
//		})
//	}
//
// Main gives Gio the main thread, which macOS and iOS require, and runs the program beside it.
//
// # Goroutines
//
// Three goroutines meet here, and each value has one owner:
//
//   - tui's loop calls every Backend method. It owns the cell grid, the latched cursor and images,
//     and the renderer, and records each frame as an immutable Gio macro in Flush.
//   - the Gio goroutine reads the window's events. It never touches a component or the grid: it
//     translates input into tui events, publishes metrics, and submits the newest frame.
//   - the forwarder is the only sender on, and the only closer of, the Events channel.
//
// See ADR 1791330692 (golib/gui) in the project knowledge base for the design and its rationale.
package gui
