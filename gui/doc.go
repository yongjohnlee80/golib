// Package gui provides a native graphical window backend for golib/tui applications,
// implemented on top of the Gio immediate-mode vector graphics engine (gioui.org).
//
// Any [tui.App] program runs on [Backend] unchanged: substituting [NewBackend] in place of
// a terminal driver allows every cell to be rasterized natively using hardware-accelerated
// vector operations, shaped monospace glyphs, custom system font fallbacks, precise box-drawing
// line geometry, hardware-scaled image placement, native widgets, and OS input method integration.
//
// # Architectural Mission & Design Philosophy
//
// Terminal emulators traditionally bridge TUI applications to native operating system
// windows. While terminal multiplexers and pseudo-terminals suffice for standard command-line
// utilities, embedding rich graphical media, true sub-cell mouse movement, fractional font
// scaling, inline vector diagrams, and responsive OS clipboard interactions often encounter
// protocol friction and platform-specific escape sequence limitations.
//
// Package gui is built on four core architectural pillars:
//
//  1. Unmodified TUI Contract: The native window satisfies [tui.Backend], [tui.GraphicsBackend],
//     [tui.ClipboardWriter], and [tui.CursorColorSetter]. Widgets retain their familiar
//     cell coordinates, layout semantics, and event lifecycles.
//  2. Strict Three-Goroutine State Isolation: Value ownership is partitioned among three
//     cooperating goroutines (the TUI loop goroutine, the Gio window loop, and an event forwarder).
//     Synchronization is achieved through atomic snapshots and single-slot handoffs rather than
//     coarse-grained widget mutexes.
//  3. Run-Length Batching & Macro Replay: Gio operates in immediate mode, where every drawing
//     operation replays as a uniform GPU upload and draw call. The backend batches contiguous
//     identical cell backgrounds into single rectangles, merges box-drawing line segments,
//     and groups glyph outlines by color into unified path runs, recording each row into an
//     independent macro replayed across frames.
//  4. Seamless Unit & Coordinate Translation: Device-independent units (dp, sp) are resolved
//     against the display's scale factor. The cell grid dimensions are derived from the window
//     dimensions divided by monospace character cell bounds. Sub-cell pointer coordinates are
//     projected back into cell grid coordinates for exact hit testing.
//
// # Three-Goroutine Concurrency Architecture
//
// Concurrency in package gui strictly delineates responsibilities across three dedicated
// goroutines to guarantee that the UI loop never blocks on window event loops or GPU presentation:
//
//	┌────────────────────────────────────────────────────────────────────────┐
//	│                           Host Platform (OS)                           │
//	│       (Window Events, Keyboard, Mouse/Scroll, IME, Frame VSync)        │
//	└───────────────────────────────────┬────────────────────────────────────┘
//	                                    │ OS Window Events
//	                                    ▼
//	                     ┌──────────────────────────────┐
//	                     │      Gio Event Goroutine     │
//	                     │  - Reads app.Window events   │
//	                     │  - Translates Keys & Pointers│
//	                     │  - Manages IME preedit buffer│
//	                     │  - Publishes atomic metrics  │
//	                     │  - Submits latest frame draw │
//	                     └──────────────┬───────────────┘
//	                                    │ Enqueue tui.Event
//	                                    ▼
//	                     ┌──────────────────────────────┐
//	                     │    Forwarder Goroutine       │
//	                     │  - Sole sender to Events()   │
//	                     │  - Bounded FIFO queue buffer │
//	                     │  - Clean shutdown & close    │
//	                     └──────────────┬───────────────┘
//	                                    │ Stream to tui.App
//	                                    ▼
//	┌────────────────────────────────────────────────────────────────────────┐
//	│                           TUI Loop Goroutine                           │
//	│  - Executes tui.App event loop, layout passes, and component trees     │
//	│  - Owns the cell grid, latched cursor state, and image table           │
//	│  - Translates cell updates and diffs in Flush                          │
//	│  - Records rows and native views into immutable Gio macros             │
//	│  - Atomically stores the latest frame for Gio presentation             │
//	└────────────────────────────────────────────────────────────────────────┘
//
// # Ownership and Concurrency Invariants
//
// To avoid race conditions and deadlocks without global locking:
//
//   - TUI Loop Goroutine Invariant: All [tui.Backend] entry points ([Backend.Flush],
//     [Backend.ShowCursor], [Backend.SetCursor], [Backend.PlaceImage], etc.) run exclusively
//     on the TUI event loop goroutine. The grid buffer, row macro cache, latched cursor,
//     and placed image registry are private to this goroutine and never accessed concurrently.
//   - Gio Window Goroutine Invariant: The OS event loop runs inside [Backend.run] on a dedicated
//     goroutine spawned by [Backend.Start]. It never directly inspects or mutates the component
//     tree, cell grid, or renderer caches. It reads the latest atomic metrics and font configuration,
//     translates raw events, and presents the most recently published [frame].
//   - Forwarder Goroutine Invariant: The [eventQueue.forward] goroutine is the sole sender on the
//     unbuffered channel returned by [Backend.Events], and the sole goroutine permitted to close it.
//     This design guarantees that closing the event stream never races against concurrent event pushes.
//
// # Drawing Performance & Draw Call Optimization
//
// In immediate-mode rendering frameworks like Gio, the cost of a frame is proportional to the
// count and complexity of recorded operations rather than the raw pixel fill rate. Emitting draw
// calls per individual cell would severely degrade frame rates on high-resolution displays.
//
// Package gui enforces strict batching rules:
//
//   - Background Span Coalescing: Contiguous runs of cells sharing the same background color
//     are merged into a single filled rectangle operation.
//   - Glyph Outline Grouping: Monospace glyphs within a row sharing foreground attributes are
//     combined into a single compound path outline per color, requiring only one draw call.
//   - Line Geometry Merging: Continuous box-drawing line segments (such as horizontal and vertical
//     borders) are coalesced into single unbroken rectangles rather than fragmented per-cell stubs.
//   - Row-Level Macro Caching: Each row is recorded into an independent [op.Ops] macro. Rows whose
//     cells do not change across frames are replayed verbatim without re-shaping glyphs or
//     re-evaluating geometries.
//
// # Getting Started
//
// To launch a native window hosting a TUI application:
//
//	package main
//
//	import (
//		"context"
//
//		"github.com/yongjohnlee80/golib/gui"
//		"github.com/yongjohnlee80/golib/tui"
//		"github.com/yongjohnlee80/golib/tui/widget"
//	)
//
//	func main() {
//		gui.Main(func() error {
//			backend := gui.NewBackend(
//				gui.WithTitle("Editor Window"),
//				gui.WithSize(1024, 768),
//			)
//			root := widget.NewBox(widget.NewLabel("Hello, Native GUI!"))
//			app := tui.NewApp(root, tui.WithBackend(backend))
//			return app.Run(context.Background())
//		})
//	}
//
// [Main] binds the primary OS thread required by platforms such as macOS and iOS,
// running the user application logic on a concurrent goroutine alongside Gio.
package gui
