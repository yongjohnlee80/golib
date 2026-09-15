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
//	                     │    2. Commit (side effects)  │
//	                     │    3. Render to Buffer       │
//	                     │    4. Apply Hardware Cursor  │
//	                     │    5. Diff against Last Frame│
//	                     │    6. Backend.Flush(diff)    │
//	                     └──────────────────────────────┘
//
// # Interpretation: what happens to an event at each node
//
// The two lanes above are TRANSPORT. Once they converge, an event is offered to
// one node at a time along the bounded bubble path, and at each node the
// runtime performs four steps in order:
//
//  1. Pointer policy — pointer input to a node whose effective policy is
//     disabled skips that node entirely, semantic and raw alike.
//  2. Resolve — the node's action resolvers are tried, consumer entries before
//     the widget's own defaults; first match wins.
//  3. Semantic dispatch — a resolved action is offered to the node in two
//     stages. First [ActionHandler.HandleAction], if the node implements it.
//     Then, for an [ActivateAction] that nobody handled, [Activatable.Activate].
//     Either returning true consumes the event.
//  4. Raw dispatch — otherwise the original event goes to
//     [Component.HandleEvent].
//
// The Activatable stage is not a detail: [widget.Button] implements no
// HandleAction at all. Its keyboard resolver produces an ActivateAction, no
// action handler exists to take it, and the fallback reaches Activate. That is
// how one control serves keyboard, pointer and programmatic activation without
// implementing any of the three.
//
// This is NOT a third transport lane. Nothing about Lane A or Lane B changes;
// this describes what an already-delivered event meets on arrival. A component
// with no resolvers, no HandleAction and no Activatable is byte-for-byte
// unaffected, which is why most widgets only ever implement HandleEvent.
//
// After the bounded walk, an unconsumed primary press on an opted-in control is
// offered to the gesture recogniser ([GestureRecognizer]), which may take the
// pointer ([Context.CapturePointer]) and later produce an [ActivateAction].
//
// What a HELD capture does with an event depends on who owns it, and the two
// differ:
//
//   - [CaptureRaw], taken by a component through Context, runs the same four
//     steps above on the owner alone — no hit-test, no parent walk — so a drag
//     begun semantically continues semantically.
//   - [CaptureGesture], taken by the runtime on a component's behalf, goes to
//     the recogniser ONLY. It never re-enters the node pipeline; any action the
//     recogniser produces is then dispatched to the owner.
//
// "No capture phase" above refers to EVENT ROUTING: there is no DOM-style
// downward phase in which ancestors preview an event before its target. That is
// unrelated to pointer capture, which is about which node keeps receiving
// pointer events once a gesture has begun.
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
//  2. Commit Phase: The callbacks registered during the pass by
//     [Context.AfterLayout] run, in registration order, keyed by [CommitKey] so a
//     component that lays out twice in a frame commits once. This is the ONLY legal
//     place for a geometry-derived side effect — publishing the ratio a split
//     actually reached, storing the size a wrapper was clamped to, dismissing an
//     overlay whose anchor is no longer laid out — which is what keeps Layout pure
//     and therefore safe for the runtime to run whenever it needs to. A callback
//     may legitimately dirty layout, so the two alternate until they settle, and a
//     cycle that will not settle is a fatal rather than a silently dropped frame.
//  3. Focus Invariant Enforcement: Any node holding focus that was hidden or zoomed
//     away during layout has its focus automatically repaired to a visible focusable node.
//  4. Render Pass: Components paint their chrome into their pre-clipped [Surface].
//     Children are painted depth-first in document order.
//  5. Real Hardware Cursor Rule: If the focused component implements [CursorReporter]
//     and reports an active insertion point, [App] positions the terminal hardware
//     cursor at that cell and configures its shape ([CursorShaper]). Otherwise, the
//     cursor is hidden. This ensures OS IME candidate windows (for CJK input) anchor
//     at the exact text editing position.
//  6. Diff & Flush: The frame buffer's current cells are diffed against the previous
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
