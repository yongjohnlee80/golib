// Package tui provides the core of golib's minimal-dependency, retained-mode
// terminal UI framework: the driver seam ([Backend], [Capabilities],
// [TestBackend]), the grapheme-cluster cell buffer and render pipeline ([Cell],
// [CellAttrs], [Surface]), the two-lane runtime loop ([App], [Bus], [Context]),
// the component tree protocol ([Component], [Focusable], [Container]), and the
// layout geometry system ([Rect], [Size], [Constraints]).
//
// # Architectural Mission & Design Philosophy
//
// Most terminal frameworks either lean on immediate-mode rendering (which redraws
// the world on every keystroke, generating terminal flicker and high CPU usage)
// or complex multi-threaded component hierarchies that require mutex locks inside
// widget methods.
//
// golib/tui is built on four core architectural pillars designed for deterministic,
// low-allocation, flicker-free terminal applications:
//
//  1. Single-Goroutine State Ownership Discipline: All component state, layout geometry,
//     focus, and cell buffers are owned and modified exclusively on the event loop
//     goroutine. Widget methods never acquire locks, and external goroutines communicate
//     with the loop by posting closures through [App.Update], dispatching managed tasks
//     via [Context.Go], or emitting events over [Bus.Publish].
//  2. Two-Seam Portability: The entire runtime interacts with the external world
//     through exactly two interfaces: [Backend] (what a terminal is) and [Surface]
//     (what components draw on). Real ANSI terminals and headless CI test harnesses
//     all satisfy the same contracts.
//  3. Flutter-Style Box Layout: A strict single-pass constraints-down, sizes-up
//     geometry protocol. Parents pass constraints to children; children choose
//     their size within those bounds; parents position children.
//  4. Grapheme-Cluster Double Buffering: The cell grid stores complete Unicode
//     grapheme clusters (with cached display cell widths) and resolved style
//     attributes. A diff pass between the current frame and the last displayed
//     frame minimizes terminal I/O, writing only cells that actually changed.
//
// # The Event Loop & Two-Lane Concurrency Model
//
// The central coordinator of the framework is [App]. The caller of [App.Run]
// executes the event loop on that calling goroutine:
//
//	┌────────────────────────────────────────────────────────────────────────┐
//	│                           External World                               │
//	│  (TTY Keyboard, Mouse, Terminal Signals)    (Background Worker Tasks)  │
//	└───────────────────┬────────────────────────────────────┬───────────────┘
//	                    │ Raw Input                          │ Goroutine Work
//	                    ▼                                    ▼
//	        ┌──────────────────────┐             ┌──────────────────────┐
//	        │   Backend.Events()   │             │   App.Post / Go      │
//	        │   (Terminal Intake)  │             │   Bus.Publish        │
//	        └───────────┬──────────┘             └───────────┬──────────┘
//	                    │                                    │
//	                    ▼                                    ▼
//	        ┌──────────────────────┐             ┌──────────────────────┐
//	        │   Lane A: Input      │             │   Lane B: Program    │
//	        │   (Unbuffered Pump,  │             │   (Unbounded Queue,  │
//	        │    Drop-Protected)   │             │    Never Dropped)    │
//	        └───────────┬──────────┘             └───────────┬──────────┘
//	                    │                                    │
//	                    └───────────────┬────────────────────┘
//	                                    ▼
//	                     ┌──────────────────────────────┐
//	                     │    Single Event Loop (Run)   │
//	                     │    - Route Key/Mouse Events  │
//	                     │    - Drain Program Queue     │
//	                     │    - Fire Timer Deadlines    │
//	                     │    - Coalesce Render Frames  │
//	                     └──────────────┬───────────────┘
//	                                    │
//	                                    ▼
//	                     ┌──────────────────────────────┐
//	                     │      Frame Pipeline Pass     │
//	                     │    1. Layout (if dirty)      │
//	                     │    2. Render to Buffer       │
//	                     │    3. Apply Hardware Cursor  │
//	                     │    4. Diff against Last Frame│
//	                     │    5. Backend.Flush(diff)    │
//	                     └──────────────────────────────┘
//
// # The Loop-Goroutine Invariant (Normative)
//
// INVARIANT: All component state — the component tree, every component's struct fields,
// focus identity, layout rects, and the cell buffer — is owned exclusively by the loop
// goroutine.
//
// [Component.Init], [Component.Layout], [Component.Render], [Component.HandleEvent],
// bus event handlers, and queued closures execute ONLY on the loop goroutine.
//
// The ONLY operations legal from other goroutines are:
//   - [App.Post] and [Context.Post] (enqueue an event)
//   - [App.Update] (enqueue a closure)
//   - [App.Go] and [Context.Go] (schedule bounded background tasks)
//   - [Bus.Publish] (enqueue a typed broadcast event)
//
// All of these methods are thread-safe, non-blocking, and return immediately.
//
// # The Two-Seam Portability Contract
//
// Portability is achieved through two clean seams:
//
//  1. [Backend]: Represents what a terminal is. It provides hardware initialization,
//     window dimensions, capability probing (color depths, Kitty keyboard protocol,
//     synchronized output), the input event stream, and a [Backend.Flush] method
//     that accepts the frame diff. The production driver lives in tui/term, while
//     [TestBackend] provides a deterministic in-memory simulator for unit tests.
//  2. [Surface]: Represents what components draw on. It provides a local, pre-clipped
//     drawing region with coordinate translation, grapheme cell writes ([Surface.SetCell]),
//     box filling ([Surface.Fill]), and child sub-surface partitioning ([Surface.Sub]).
//
// # Frame Lifecycle & Rendering Pipeline
//
// When component state changes, components mark the tree dirty via [Context.MarkDirty]
// (appearance change) or [Context.RequestLayout] (geometry change). The runtime
// coalesces dirty marks according to the configured minimum frame interval:
//
//  1. Layout Pass: If geometry is dirty, [App] runs a single-pass traversal starting
//     from the root with tight constraints matching the terminal size. Each container
//     measures its children via [Context.LayoutChild] and positions them via [Context.PlaceChild].
//     Absolute coordinates are then computed for hit testing.
//  2. Focus Invariant Enforcement: Any node holding focus that was hidden or zoomed
//     away during layout has its focus automatically repaired to a visible focusable node.
//  3. Render Pass: Components paint their chrome into their pre-clipped [Surface].
//     Children are painted depth-first in document order.
//  4. Real Hardware Cursor Rule: If the focused component implements [CursorReporter]
//     and reports an active insertion point, [App] positions the terminal hardware
//     cursor at that cell and configures its shape ([CursorShaper]). Otherwise, the
//     cursor is hidden. This ensures OS IME candidate windows (for CJK input) anchor
//     at the exact text editing position.
//  5. Diff & Flush: The frame buffer's current cells are diffed against the previous
//     frame. Only modified cells are formatted into an optimized batch and emitted to
//     [Backend.Flush] in a single write.
//
// # Getting Started
//
// A complete, runnable application assembling an overlay host, box chrome, and an input
// widget:
//
//	package main
//
//	import (
//		"context"
//		"os"
//
//		"github.com/yongjohnlee80/golib/tui"
//		"github.com/yongjohnlee80/golib/tui/term"
//		"github.com/yongjohnlee80/golib/tui/widget"
//	)
//
//	func main() {
//		backend, err := term.Open()
//		if err != nil {
//			os.Exit(1)
//		}
//		defer backend.Stop()
//
//		input := widget.NewTextInput(widget.WithPlaceholder("Type here..."))
//		box := widget.NewBox(input, widget.WithTitle("Interactive Form"))
//		root := widget.NewOverlayHost(box)
//
//		app := tui.NewApp(root, tui.WithBackend(backend))
//		if err := app.Run(context.Background()); err != nil {
//			os.Exit(1)
//		}
//	}
package tui
