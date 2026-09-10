// Package widget provides golib/tui's standard widget suite: the [Base] embedding
// contract, the [Box] titled-panel container, the [OverlayHost] modal/popup layer,
// and the complete set of fourteen production-grade TUI components sufficient to build
// sophisticated terminal applications (such as lazygit-, sqlit-, and neovim-shaped tools)
// out of the box with zero custom widget plumbing.
//
// # Complete Widget Inventory
//
//	Widget          Category         Focusable       Primary Emitted Events (Bus)
//	TextInput       Form Input       yes             [SubmitEvent], [ChangeEvent]
//	TextArea        Multi-line Text  yes             [ChangeEvent]
//	Select[T]       Form Input       yes             [SelectionChangedEvent], [OpenedEvent], [ClosedEvent]
//	List[T]         Collection       yes             [SelectionChangedEvent], [ActivateEvent]
//	Table[T]        Collection       yes (via List)  [SelectionChangedEvent], [ActivateEvent]
//	Tree            Navigation       yes             [ExpandRequestEvent], [CollapseEvent], [ActivateEvent]
//	Editor          Modal Text       yes             [ModeChangedEvent], [YankEvent]
//	BufferView      Stream / Pager   yes (scroll)    [FollowTailChangedEvent]
//	Tabs            Navigation       yes (bar)       [TabChangedEvent]
//	Split           Container        no (panes are)  [SplitResizedEvent], [SplitZoomEvent]
//	Float           Overlay / Modal  children        [DismissEvent]
//	StatusBar       Chrome           no              —
//	ProgressBar     Feedback         no              —
//	Text            Static Display   no              —
//
// Every bus event carries Owner ([tui.NodeID]) as its first field, allowing subscribers
// to filter events by emitting widget identity. Bus publication is strictly enqueue-only
// onto the application event loop.
//
// # The Five Architectural Pillars
//
// Package widget is built around five core architectural invariants designed to ensure
// predictability, zero-allocation rendering, and robust state management:
//
// 1. Loop-Goroutine Ownership and Concurrency Boundaries:
//
// All widgets are retained, mutable [tui.Component] implementations living strictly on the
// application loop goroutine. Every widget method—aside from constructors—must only be
// invoked from the loop goroutine or via App.Update callbacks.
//
// There is exactly ONE sanctioned concurrent exception: the [io.Writer] handle returned by
// [BufferView.Writer]. It is fully thread-safe from any background goroutine (e.g. streaming
// stdout from an exec.Cmd). It utilizes a bounded semaphore queue to prevent unbounded memory
// growth when the loop lags, guarantees ordered delivery, and returns [ErrClosed] after the
// BufferView unmounts. The [BufferView] value itself remains loop-owned and deliberately does
// not implement [io.Writer].
//
// Asynchronous background workloads (such as fetching database rows or remote branches)
// must never spawn uncoordinated goroutines. Instead, they schedule work via App.Go addressing
// the target widget's [tui.NodeID], and the results arrive safely on the loop goroutine as
// a typed [tui.TaskResult].
//
// 2. The Base Embedding Contract (Method Promotion vs Virtual Dispatch):
//
// Every widget embeds [Base] by value. In Go, struct embedding provides method promotion
// (e.g., exposing Context, NodeID, MarkDirty, and RequestLayout without boilerplate) and zero-cost
// memory layout without pointer indirection.
//
// Crucially, Go embedding is NOT class inheritance: there is no virtual dispatch. An embedded
// struct method cannot invoke an overridden method on the outer struct. Consequently:
//   - [Base] never uses template-method patterns. The runtime calls interface methods
//     ([tui.Component], [tui.Focusable], [tui.Container]) on the OUTER type.
//   - Capability interfaces ([tui.Focusable], [tui.Container], [tui.FocusScope],
//     [tui.CursorReporter], [tui.CursorShaper]) are detected via type assertions on the outer
//     widget value; [Base] implements none of them, preventing accidental capability leakage.
//   - Any widget overriding [Base.Init] MUST chain to b.Base.Init(ctx) first.
//
// 3. Composable Panel Chrome via Box:
//
// Widgets themselves remain strictly chrome-free (no built-in borders, frame margins, or titles).
// Any widget that represents a distinct visual panel (such as a List, Tree, Table, or BufferView)
// is wrapped in a [Box]:
//
//	┌─ [Title] ────────────────────────────────────────────────────────┐
//	│                                                                  │
//	│                     Wrapped Child Widget                         │
//	│                    (List, Table, Editor)                         │
//	│                                                                  │
//	└─────────────────────────────────────────────── [Status / Hints] ─┘
//
// [Box] embeds titles directly into the top border row and status hints into the bottom border
// row, consuming zero extra vertical lines of screen real estate. When a [Box] or any of its
// descendants gains focus, [Box] automatically merges [style.TokenBorderFocused] over its border,
// providing the familiar active-panel highlight (standardized in tools like lazygit) with no
// custom drawing code required.
//
// 4. Decoupled Bus Events vs Synchronous Seams:
//
// Widgets publish domain events through the application event bus ([tui.Bus]). Subscribers
// listen via [tui.Subscribe] or [tui.SubscribeScoped] without needing direct widget references.
//
// However, because the application loop alternates between an input lane (keystrokes, mouse)
// and a program lane (bus events), publishing a bus event introduces an asynchronous queue delay.
// For operations that must execute synchronously before the next pending keystroke is processed
// (such as advancing focus to the next field in a form or closing a modal popup), widgets provide
// synchronous hooks—such as [WithOnSubmit] on [TextInput]—to eliminate input race conditions.
//
// 5. Stack Overlay Architecture and Floating Modals:
//
// Full-screen overlays and modal dialogs are managed by wrapping the application root in
// an [OverlayHost]:
//
//	┌──────────────────────────────────────────────────────────────────┐
//	│ [OverlayHost] Layer Stack                                        │
//	│                                                                  │
//	│  Layer 3: Ephemeral Popups ([Select] dropdown list)              │
//	│           ▲                                                      │
//	│  Layer 2: Floating Dialogs ([Float] modal window)                │
//	│           ▲                                                      │
//	│  Layer 1: Scrim / Dimmer ("░" dimming backdrop)                  │
//	│           ▲                                                      │
//	│  Layer 0: Base Application Tree (Splits, Boxes, Panels)          │
//	└──────────────────────────────────────────────────────────────────┘
//
// [OverlayHost] implements the overlay protocol:
//   - [Select] automatically discovers the mounted [OverlayHost] via an internal bus handshake
//     and projects its dropdown option list onto the top overlay layer, complete with a focus trap.
//   - [Float] attaches explicitly via [OverlayHost.Attach] and provides toggleable dialogs
//     ([Float.Show] / [Float.Hide]), focus trapping, Esc-dismissal, and background dimming.
//
// # Practical Composition Example
//
// Below is a complete example demonstrating how standard widgets compose into a responsive,
// two-pane database/file exploration workspace with a status bar:
//
//	func BuildWorkspace(app *tui.App) tui.Component {
//		// 1. Left pane: Interactive schema explorer tree
//		tree := widget.NewTree(
//			widget.NewTreeNode("root", "Database",
//				widget.WithBadge("connected"),
//			),
//		)
//		leftBox := widget.NewBox(tree,
//			widget.WithTitle("Explorer"),
//			widget.WithStatus("enter: expand"),
//		)
//
//		// 2. Right pane: Query editor + Log viewer split vertically
//		editor := widget.NewEditor(
//			widget.WithInitialText("SELECT * FROM users LIMIT 50;"),
//		)
//		editorBox := widget.NewBox(editor,
//			widget.WithTitle("SQL Query (Vim)"),
//			widget.WithStatus("ESC: normal | :w run"),
//		)
//
//		logs := widget.NewBufferView(
//			widget.WithFollowTail(true),
//		)
//		logsBox := widget.NewBox(logs,
//			widget.WithTitle("Execution Log"),
//		)
//
//		rightSplit := widget.NewSplit(widget.Vertical, editorBox, logsBox,
//			widget.WithRatio(0.6),
//		)
//
//		// 3. Main layout: Horizontal master-detail split
//		mainSplit := widget.NewSplit(widget.Horizontal, leftBox, rightSplit,
//			widget.WithRatio(0.25),
//			widget.WithMinSizes(20, 40),
//		)
//
//		// 4. Chrome: Docked status bar at bottom
//		status := widget.NewStatusBar()
//		status.SetLeft("Ready")
//		status.SetRight("F1: Help | Ctrl+C: Quit")
//
//		body := tui.NewFlex(tui.FlexVertical).
//			Add(mainSplit, tui.FlexItem{Flex: 1}).
//			Add(status, tui.FlexItem{Fixed: 1})
//
//		// 5. Wrap root in OverlayHost to support floating dialogs and dropdowns
//		return widget.NewOverlayHost(body)
//	}
package widget
