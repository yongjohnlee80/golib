// Package widget provides golib/tui's standard widget suite: the [Base] embedding
// contract, the [Box] titled-panel container, the [OverlayHost] modal/popup layer,
// and the twenty production-grade TUI components inventoried below. Applications
// compose these primitives and add domain-specific controllers rather than
// reimplementing their interaction machinery.
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
//	Resizable       Wrapper          content only    [ResizedEvent]
//	Float           Overlay / Modal  children        [DismissEvent]
//	Modal           Dialog           trap owner      [OverlayDismissedEvent]
//	Menu            Menu / Command   yes             [MenuActivatedEvent], [MenuSelectionChangedEvent]
//	MenuBar         Menu / Chrome    no (menu is)    — (delegates to [Menu])
//	MenuItem        Control          when enabled    [tui.ControlActivatedEvent]
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
//   - [Select] resolves the NEAREST ENCLOSING [OverlayHost] from the component tree
//     and projects its dropdown option list onto its top overlay layer, complete with a
//     focus trap. Two hosts in one application each serve only the widgets inside them.
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
// DISMISS KEYS. Escape always closes a [Modal]. A terminal application usually
// has a second dismiss key, and a dialog traps focus and swallows it, so
// [WithModalDismissKeys] lets the host declare one. It is EMPTY BY DEFAULT and
// host-owned: only the application knows which of its dialogs a reader
// navigates and which take typed input, and a `q` that closes the dialog is a
// `q` nobody can type into it. A configured key is an escape equivalent in full
// — it activates a Cancel-role button if there is one and reports
// [DismissEscape], because the reason names the intention rather than the key.
//
// Precedence, highest first: a focused control that consumes the key, an
// enabled button's mnemonic, a host dismiss key, then the navigation aliases.
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
//	ok := widget.NewButton("Save", widget.WithRole(widget.ButtonRoleAccept), widget.WithDefault(true),
//		widget.WithOnActivate(func() { save() }))
//	no := widget.NewButton("Cancel", widget.WithRole(widget.ButtonRoleReject))
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
// default button ([WithDefault]), else the first enabled button, else the
// Modal node itself. The last case is what keeps Escape reachable when every
// control is disabled — the ring inside a trap must never be empty. The
// preference applies on EVERY focus repair, not only at open, so a button
// enabled or added later still takes focus if it is the default.
// [Modal.SelectedButton] reports the focused button's index, or -1 when the
// Modal node itself holds focus.
//
// ESCAPE resolves the REJECT ROLE, never a label or a position: matching
// "Cancel" breaks under translation and matching the last button breaks under
// reordering. When a Reject-role button exists Escape activates it through the
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
// edges, Space activates the focused button, Enter the default one, Escape
// resolves the Reject role,
// and focus returns to where it came from when the dialog closes. A dialog with
// its pointer policy disabled remains fully operable. Nothing depends on the
// mouse, and no control is reachable only by clicking.
//
// # Menu, MenuBar and MenuItem
//
// A Menu owns a MODEL and paints it. Its rows are [MenuItemModel] VALUES, not
// mounted children — a hundred-row menu costs a hundred struct values rather
// than a hundred nodes, and replacing the whole thing is one assignment.
//
// That choice has a consequence worth knowing, because it explains the rest of
// the design: a row has no node, so the runtime cannot hit-test it, cannot arm
// it, and cannot tell two rows of one Menu apart. Menu therefore declares an
// anchor region per visible row during layout — the same rectangle it
// hit-tests through — and runs its own press-arm / release-activate machine
// over them. Both the mouse and the keyboard end at [MenuActivateAction], so
// they cannot come to mean different things.
//
// A Menu's levels are ANCHORED OVERLAY LAYERS, so it must be mounted inside an
// [OverlayHost] — the NEAREST enclosing one holds them, which is what lets two
// independent hosts, or a host nested inside another, each serve the menus that
// live in it. [Menu.Open] returns an error matching [ErrAnchorUnusable] when
// there is no host above it, rather than silently opening nothing.
//
// The executor below is the dispatch seam: a plain function the consumer owns,
// taking the runtime's own invocation and reporting whether it handled it.
//
//	// commands is the application's own dispatch table — any function with
//	// this shape will do; the package neither supplies nor requires one.
//	commands := map[tui.ActionID]func() error{
//		"file.new":  editor.NewFile,
//		"view.wrap": editor.ToggleWrap,
//		"app.quit":  app.Quit,
//	}
//
//	menu := widget.NewMenu(
//		widget.WithActionExecutor(func(inv tui.ActionInvocation) bool {
//			run, ok := commands[inv.Action.ActionID()]
//			if !ok {
//				return false // unhandled: the menu stays open
//			}
//			return run() == nil
//		}))
//
//	if err := menu.SetModel([]widget.MenuItemModel{
//		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
//			widget.NewCommand("new", "New", NewFileAction{}),
//			widget.NewSeparator("s1"),
//			widget.NewCheck("wrap", "Wrap lines", ToggleWrapAction{}),
//		}),
//		widget.NewCommand("quit", "Quit", QuitAction{}),
//	}); err != nil {
//		return err
//	}
//
//	// Optional: lay the menu along an edge. The bar wraps a menu that already
//	// exists, and the host must enclose whichever of the two is mounted.
//	bar := widget.NewMenuBar(menu, widget.WithBarPlacement(widget.BarPlacementTop))
//	root := widget.NewOverlayHost(bar)
//
// THE RENDERER SEAM. [RowRenderer] is handed a [RowView] and a [RowState] whose
// four flags — Selected, Armed, Focused, Open — are INDEPENDENT. That is what
// lets a custom painter draw a selected row that owns the open submenu
// differently from one that does not, or draw the selection faintly while the
// menu has lost focus. Disabled is deliberately absent: it is derivable from
// [RowView] (Enabled, and the kind for a separator), and a flag duplicating a
// value in the same call is a second copy of one fact.
//
// THE MODEL. [ItemKind] is deliberately CLOSED — Command, Submenu, Separator,
// Check, Radio — because a new kind needs either a switch edit or a polymorphic
// row seam, and this package provides the seam rather than pretending the set is
// open: [RowRenderer] supplies appearance and Action supplies behaviour. A zero
// [MenuItemModel] is inert (a disabled, invisible command), so a row somebody
// forgot to fill in shows nothing rather than appearing as a blank clickable
// entry; the constructors above opt a row in.
//
// IDs are unique RECURSIVELY, across every level. Every operation here names a
// row by ID alone, so a repeated ID would make selection and mutation ambiguous,
// and an ambiguous mutation silently picks one. [Menu.SetModel] returns a typed
// error matching [ErrInvalidMenuModel] and a specific sentinel, and on error
// NOTHING changes — not the model, not the open levels, not the selection.
//
// The model is a VALUE on both sides: SetModel deep-copies on ingest and
// [Menu.Model] returns a copy, so a caller mutating its own slice cannot reach
// inside a mounted widget.
//
// ACTIVATION is state, then action, then close. A check toggles before its
// action runs, so a handler reading its own row sees the change that triggered
// it; a radio also clears every other member of its Group, at any depth. The
// menu closes only when the action was HANDLED, because a command that could not
// run must not look like one that did. [MenuActivatedEvent] is emitted exactly
// once per activation including when it was not handled, and carries that flag —
// rows need their own event because [tui.ControlActivatedEvent] identifies a
// node, and every row of one Menu shares it.
//
// Menu copies the trusted incoming invocation and replaces only its Action
// before calling the executor, so Origin and Source survive end to end and the
// Menu cannot claim a keypress produced what a click did.
//
// KEYS follow the layout. A vertical menu steps with Up/Down and opens to the
// side; a [MenuBar] steps with Left/Right and opens away from its edge. Escape
// closes every level, Left closes the deepest and returns the selection to the
// row that opened it, Enter and Space activate, and a row's Hotkey activates it
// from anywhere in the level. Everything the pointer can do, the keyboard can
// do.
//
// LEVELS are anchored overlay layers held by the nearest enclosing
// [OverlayHost], and opening one is a SYNCHRONOUS TRANSACTION against that one
// host: resolve it, check the anchor, mount, and only then record the level.
// [Menu.Open] returns the host's error, so a caller is never told a popup
// appeared when none did.
//
// CLOSING IS SYNCHRONOUS TOO, whoever causes it. A level is exactly a mounted
// popup, so the Menu learns it is gone from that popup's own unmount — not from
// a bus event delivered later. That matters within a single turn: an
// application can call [OverlayHost.CloseAnchored] and reopen the row in the
// same update and get a popup, where a Menu reconciling on the program lane
// would still have been counting the level it had just lost and would have
// treated the reopen as a duplicate. Three things close a level and one
// mechanism covers all three — a row that stops being a visible, enabled
// submenu, the host's anchor-loss commit when the row is no longer laid out,
// and an explicit CloseAnchored. [OverlayDismissedEvent] is still published,
// for observers rather than for this. Unmounting the Menu closes every level it
// owns: the levels are the host's children, so nothing else would.
//
// A CLIPPED ROW IS NOT A ROW, and that is ONE set of rows rather than three. A
// row outside the rect the Menu's parent allowed declares no anchor region, gets
// no hit rectangle, and is not painted — so it cannot be clicked, cannot be
// opened, and never reaches a consumer's [RowRenderer]. A menu squeezed to one
// line would otherwise hang a popup off a row nobody can see, and a bar narrower
// than its rows would run consumer rendering code for a row with no cells.
//
// MENUBAR is a placement shell over one Menu, not a parallel widget: the model,
// the selection and the open/close lifecycle all stay on the [Menu], reachable
// through [MenuBar.Menu], so there is one lifecycle rather than two that can
// disagree. It does NOT position itself — a component's parent decides where it
// goes — so [BarPlacement] means orientation and drop direction, and a bar
// belongs at the bottom of the screen by being at the bottom of the layout that
// owns it. The orientation it applies lasts exactly as long as the bar is
// MOUNTED: constructing a bar changes nothing, and a Menu taken out of one is a
// vertical menu again.
//
// MENUITEM is the standalone leaf: a mountable, focusable control for ordinary
// layouts. The distinction from [MenuItemModel] earns the two names —
// MenuItemModel is inert data a Menu owns, while MenuItem is a node, so the
// runtime's generic recogniser arms it and it emits [tui.ControlActivatedEvent]
// because Owner identifies it.
//
// # Resizable, and the Split divider
//
// Two widgets change a size interactively, and they are deliberately not the
// same widget. [Resizable] SIZES ONE BOX; a [Split] divider REDISTRIBUTES ONE
// SHARED EXTENT between two panes. Expressing the second as the first would
// make every pane's minimum a negotiation with its sibling through a wrapper
// that cannot see it.
//
// RESIZABLE IS A WRAPPER, not a capability a widget opts into. Anything can be
// made resizable without knowing it is, which is what keeps the feature from
// reappearing in every widget that ever wants it:
//
//	box := widget.NewResizable(widget.NewBox(tree, widget.WithTitle("Files")),
//		widget.WithMinSize(tui.Size{W: 12, H: 4}),
//		widget.WithMaxSize(tui.Size{W: 60, H: 30}),
//		widget.WithHandles(widget.HandleBottomRight, widget.HandleRight),
//		widget.WithResizeStep(5, widget.StepPercent))
//
// It has two modes, reported by [SizeMode]. [SizeAuto] tracks the child's
// intrinsic size on EVERY pass — it is continuous, not a one-shot adoption, so a
// child that grows is followed. [SizeExplicit] holds a requested size, which
// [Resizable.SetSize] records and [Resizable.RequestedSize] reports. What the
// wrapper actually reached is [Resizable.Size]; the two differ whenever a parent,
// a min or a max had something to say, and a wrapper inside a fixed-rect
// [Float] tells the truth rather than being a special case.
//
// Each handle in [WithHandles] is a real component, so the runtime hit-tests,
// styles and captures it instead of the wrapper doing coordinate arithmetic by
// hand. [WithHandlePlacement] chooses whether a grip costs the child cells
// ([PlacementReserve]) or paints over its edge ([PlacementOverlay]); reserve is
// DIRECTIONAL, taking its band off the side each handle is on and offsetting
// the child accordingly, so a top-left grip never sits on the child's first
// cell. A grip whose own rectangle will not fit is dropped for the frame — a
// two-column glyph needs two columns, and one placed in a single column renders
// nothing at all. [WithHandleGlyph] therefore rejects anything but a single
// grapheme of display width one or two, at construction.
//
// NEITHER THE GRIPS NOR THE WRAPPER ARE TAB STOPS: wrapping arbitrary content
// changes traversal in no way. Keyboard resizing still works, because resolvers
// run at every node on the bubble path and an unhandled Shift-arrow from a
// focused DESCENDANT meets the wrapper's resolver on its way up. Two
// compositions do not get that — content with nothing focusable inside it, and
// content that CONSUMES the keys, as an [Editor] or [TextArea] does for
// selection — and those drive resizing through the grips, through
// [tui.Context.DoAction], or from a focus owner of their own.
//
// ONE VOCABULARY SERVES BOTH WIDGETS. [ResizeBeginAction],
// [ResizeUpdateAction], [ResizeStepAction], [ResizeSetAction],
// [ResizeEndAction] and [ResizeCancelAction] are interpreted by Resizable and by
// Split alike, so a consumer binding a key to a resize action need not know
// which widget will answer it. A Split selects the divider handle for its own
// axis — [HandleVerticalDivider] for a horizontal split — and ignores the axis
// it does not have; its arrows are Alt-bound along that axis only, because
// Alt-Left on a vertical split is a different gesture rather than a smaller
// step. [Handle] is a closed set of ten: four edges, four corners, two
// dividers. An unknown or unsupported handle is refused with no gesture stored,
// so the update and end that follow are inert too.
//
// GEOMETRY TRUTH. Both widgets separate what was asked for from what is on
// screen, and publish only the latter:
//
//	[Split.RequestedRatio]  the last explicit request, unclamped — what to PERSIST
//	[Split.Ratio]           the effective division, as the last commit stored it
//	[Split.Cells]           that same answer in integers, plus whether it exists yet
//
// Persisting the effective value is the bug this prevents: a split clamped by a
// min size on a narrow terminal would save the clamp, and every restore at that
// width would walk the divider a little further.
//
// Both publish from the COMMIT phase (see [tui.Context.AfterLayout]) rather than
// from their setters, which fixes the timing rather than merely the value.
// [SplitResizedEvent] and [ResizedEvent] carry what the geometry reached, and the
// rules are the same for both: nothing on the first layout, one event when the
// cells actually move — including when a terminal resize moved them and nobody
// called a setter — and nothing at all for a request that changes no cells,
// however far the pointer travelled.
//
// A drag holds the POINTER CAPTURE, so motion and the release keep arriving once
// the pointer has left the widget's rect; without it a drag froze at the edge
// and never saw its own release. Cancelling (Escape) restores the REQUEST that
// was in force when the gesture began — not the effective value, or a cancel on
// a narrow terminal would quietly commit the clamp as the user's choice. A
// capture the RUNTIME revokes is not a cancellation: the size reached stands,
// and only the gesture ends.
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
