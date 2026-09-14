// Package widget provides golib/tui's standard widget suite: the [Base] embedding
// contract, the [Box] titled-panel container, the [OverlayHost] modal/popup layer,
// and the sixteen production-grade TUI components inventoried below, sufficient to
// build sophisticated terminal applications (such as lazygit-, sqlit-, and
// neovim-shaped tools) out of the box with zero custom widget plumbing.
//
// # Complete Widget Inventory
//
//	Widget          Category         Focusable       Primary Emitted Events (Bus)
//	Button          Control          when enabled    [tui.ControlActivatedEvent]
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
//	Modal           Dialog           trap owner      [OverlayDismissedEvent]
//	StatusBar       Chrome           no              —
//	ProgressBar     Feedback         no              —
//	Text            Static Display   no              —
//
// Every bus event carries Owner ([tui.NodeID]) as its first field, allowing subscribers
// to filter events by emitting widget identity. Bus publication is strictly enqueue-only
// onto the application event loop.
//
// # Button
//
// Button is the smallest complete interactive widget, and the demonstration
// that the runtime carries the interaction burden rather than each widget. It
// holds no pointer arithmetic, no press/release bookkeeping, no hit-testing and
// no capture handling: it implements [tui.Activatable] — Activate and SetArmed —
// and the runtime supplies keyboard, pointer, drag-out-and-back and
// programmatic activation identically across every backend.
//
// Four behaviours are worth knowing before using it:
//
//   - A callback-free Button is ACTIVATABLE, not inert. Activating it succeeds
//     and publishes [tui.ControlActivatedEvent]; it simply runs no callback of
//     its own, so observers on the bus still see the activation.
//   - A DISABLED Button leaves the focus ring. Tab does not stop on it, and
//     [Context.InvalidateFocusability] repairs focus synchronously if it held
//     focus when it was disabled.
//   - POINTER POLICY is per-widget and inherited by the subtree. It may be set
//     before mount by chaining [Button.WithPointerPolicy] onto the constructor,
//     and is applied when the Context arrives.
//   - [tui.ActivationAvailability] is an optional runtime HINT, implemented here
//     so the gesture recogniser does not begin a gesture on a control that
//     cannot be activated. It never replaces the authorization check, which
//     lives in Activate and only there.
//
// Styling is an association: a Button holds a [ButtonStyle] it does not own, so
// one immutable style value can safely dress many Buttons. Its values are style
// tokens, so the App's theme decides the rendered colours.
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
//   - [Float] attaches explicitly via [OverlayHost.Attach] (and detaches via
//     [OverlayHost.Detach]) and provides toggleable windows ([Float.Show] /
//     [Float.Hide]), focus trapping, Esc-dismissal, and background dimming. It is the
//     LOWER-LEVEL primitive: a positioned, trapping layer around arbitrary content.
//   - [Modal] is the COMPOSED dialog built on the same stack: a card with a title,
//     a body and role-carrying buttons, plus the lifecycle a dialog needs — Open and
//     Dismiss, LIFO stacking, typed dismissal reasons, and one backdrop shared by
//     whichever dialog is on top. Reach for Modal when you want a dialog; reach for
//     Float when you want a floating layer and intend to supply the behaviour
//     yourself. See the Modal section below.
//
// # Practical Composition Example
//
// # Modal
//
// Modal is the composed dialog: a focus trap filling its host, holding a card
// that carries a title, the caller's body, and a row of buttons. It is three
// nodes rather than one, and the split is forced rather than tidy — a component
// cannot place ITSELF, so centring a card requires a parent to do it, and a
// card-sized surface cannot paint a full-screen backdrop.
//
//	OverlayHost (Stack)
//	└── Modal          ← fills the host; owns the trap, the lifecycle, dismissal,
//	    │                the card's placement and the backdrop PREFERENCE
//	    └── card       ← non-focusable; border, title, body, buttons
//
// Minimal use:
//
//	host := widget.NewOverlayHost(appRoot)   // once, wrapping the whole UI
//
//	ok := widget.NewButton("Save", widget.WithRole(widget.ButtonRoleDefault),
//		widget.WithOnActivate(func() { save() }))
//	no := widget.NewButton("Cancel", widget.WithRole(widget.ButtonRoleCancel))
//
//	dlg := widget.NewModal(widget.NewText("Save your changes?"),
//		widget.WithModalTitle("Unsaved work"),
//		widget.WithButtons(no, ok),
//		widget.WithOnDismiss(func(r widget.DismissReason) { log(r) }))
//
//	if err := dlg.Open(host); err != nil { … }   // loop goroutine
//
// LIFECYCLE. Open mounts the dialog as the topmost layer and moves focus into
// it before returning, so no input can reach the covered UI in between. Opening
// one that is already open returns [ErrModalAlreadyOpen] and changes nothing; a
// dialog whose own tree cannot be mounted returns [ErrModalNotMountable] and
// leaves the host exactly as it was. Dismiss is idempotent — dismissal arrives
// from Escape, a Cancel button and the program at once, so closing twice
// publishes one [OverlayDismissedEvent] carrying a [DismissReason].
//
// ORDERING. The dialog is removed from the tree BEFORE the WithOnDismiss
// callback runs, and the event is published after it. That is what makes
// reopening the same dialog from its own dismissal callback ordinary rather
// than a double-mount panic.
//
// FOCUS. The dialog traps focus, so Tab cannot reach the controls underneath.
// It nominates where focus lands through [tui.InitialFocusProvider]: the enabled
// button carrying [ButtonRoleDefault], else the first enabled button, else the
// Modal node itself. The last case is what keeps Escape reachable when every
// control is disabled — the ring inside a trap must never be empty. The
// preference applies on EVERY focus repair, not only at open, so a button
// enabled or added later still takes focus if it is the default.
// [Modal.SelectedButton] reports the focused button's index, or -1 when the
// Modal node itself holds focus.
//
// ESCAPE resolves the CANCEL ROLE, never a label or a position: matching
// "Cancel" breaks under translation and matching the last button breaks under
// reordering. When a cancel-role button exists Escape activates it through the
// runtime — publishing the same [tui.ControlActivatedEvent] a click would, with
// keyboard provenance — and then dismisses with [DismissCancel]. With no such
// button it dismisses with [DismissEscape].
//
// STACKING is LIFO and owned by the host. Several dialogs may be open at once;
// only the topmost dims the background, and dismissing one that is not on top
// closes every dialog above it first, topmost-first, each publishing its own
// event with reason [DismissReplaced]. [OverlayHost.TopModal] reports which is
// current.
//
// RUNTIME CHANGES. [Modal.SetButtons] replaces the button list atomically: the
// whole list is validated first — no nil entries, no repeated *Button, nothing
// already mounted elsewhere, at most one Default and one Cancel — and a rejected
// call changes nothing at all, returning an error matching both
// [ErrInvalidButtonList] and the specific sentinel. Accepted, it reconciles the
// mounted tree in one batch, moving retained buttons rather than remounting
// them, so their identity and focus survive a reorder.
// [Modal.WithPointerPolicy] sets the pointer policy for the whole dialog
// subtree, and [Modal.WithStyle] restyles the live card and its backdrop
// together.
//
// ACCESSIBILITY AND KEYBOARD PARITY. Everything a pointer can do here, a
// keyboard can do: Tab and Shift-Tab cycle the dialog's buttons and stop at its
// edges, Enter activates the focused button, Escape resolves the cancel role,
// and focus returns to where it came from when the dialog closes. A dialog with
// its pointer policy disabled remains fully operable. Nothing depends on the
// mouse, and no control is reachable only by clicking.
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
