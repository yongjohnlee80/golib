// Package decl adapts golib/decl onto golib/tui: it turns a parsed declarative
// schema into a live tree of real widgets and satisfies [decl.Adapter].
//
// # Architectural Separation
//
// The core declarative engine ([github.com/yongjohnlee80/golib/decl]) owns identity,
// traversal ordering, reactive propagation, and signal/handler dispatch contracts;
// it knows nothing about terminal cells, widgets, or surfaces.
//
// This package owns the terminal-specific translation layer:
//
//   - Mapping QML type names (e.g. "Button", "Editor", "Split", "Dialog") to concrete widgets.
//
//   - Mapping declarative property assignments to concrete widget methods and setters.
//
//   - Wiring widget events, user keystrokes, and activations to the engine's emitters.
//
//   - Managing layout docking ([tui.Dock]), overlay presentation, and dialog lifecycles.
//
//     ┌────────────────────────────────────────────────────────┐
//     │ QML Layout (*.qml)                                     │
//     │ Window { MenuBar { ... } Editor { ... } Dialog { ... } }│
//     └───────────────────────────┬────────────────────────────┘
//     │ parse/qml & decl.Tree
//     ▼
//     ┌────────────────────────────────────────────────────────┐
//     │ tui/decl.Adapter (implements decl.Adapter)             │
//     │  ├─ Registry & Builders: StdRegistry()                 │
//     │  ├─ Model/View Adapters: ListModel, TreeListModel      │
//     │  ├─ Dialog Lifecycles: Modal, DialogButtonBox          │
//     │  └─ Palette Roles: Propagation & Restyling             │
//     └───────────────────────────┬────────────────────────────┘
//     │ mounts & mutates
//     ▼
//     ┌────────────────────────────────────────────────────────┐
//     │ golib/tui Widget Tree (tui.App event loop)             │
//     │  *widget.Box, *widget.Editor, *widget.Modal, etc.      │
//     └────────────────────────────────────────────────────────┘
//
// # The Builder Pattern: Why Not Reflection or Post-Configuration?
//
// Real widget constructors in a terminal toolkit are heterogeneous by design:
//   - Containers like [widget.Split] require their orientation and BOTH child widgets
//     at construction time, providing no zero-argument constructor or deferred attachment.
//   - Interactive widgets like [widget.Button] take their activation callback option
//     primarily at construction.
//   - Generic widgets (such as list and select controls) cannot be safely instantiated
//     via string reflection without violating the library's zero-reflection policy.
//
// To resolve this, [Registry] maps type names to pure [Builder] functions. A builder
// receives a [Build] context containing:
//  1. All declared properties in document order.
//  2. All child components already constructed (bottom-up construction).
//  3. Signal emitters wired to the engine's dispatch graph.
//
// The builder consumes whichever properties it requires for initialization and returns
// their names. The engine only applies the remaining unconsumed properties via runtime
// setters, avoiding redundant or non-idempotent setter invocations.
//
// # Model/View Architecture
//
// [tui/decl] provides Qt-style model/view decoupling:
//   - Data models ([ItemModel], [TreeModel]) are implemented in Go and supplied as reactive sources.
//   - Declarative views ([ListView], [ComboBox], [TableView], [TreeView]) bind to models via `model:`.
//   - Views subscribe to model mutations ([decl.Reset], [decl.Inserted], [decl.Removed], [decl.Changed])
//     and update terminal layouts incrementally without re-parsing or rebinding QML.
//   - Row selection tracking uses stable keys ([ItemModel.Key]), ensuring that the user's active
//     selection is preserved even when surrounding rows are inserted or deleted.
//
// # Dialogs and Button Roles
//
// [Dialog] and [FileDialog] manage their own modal lifecycles and input traps. Initial focus lands
// on the first control in Tab order that takes focus (the body's first field, else the first enabled
// button, else the dialog node itself). Dialog actions are declared via [DialogButtonBox] or standard
// buttons bitmasks:
//   - Buttons carry Qt button roles ([widget.ButtonRoleAccept], [widget.ButtonRoleReject],
//     [widget.ButtonRoleDestructive]).
//   - Enter that the focused control leaves unclaimed answers only the button explicitly named by
//     `defaultButton` (no standard button is a default implicitly, and a [DialogButtonBox] declares none).
//   - Space activates the focused button; Escape or RejectRole buttons emit `rejected` and dismiss the dialog.
//   - DestructiveRole buttons dismiss the dialog cleanly without firing `accepted` or `rejected`.
//
// # Palette Roles and Theming
//
// Colours are specified using standard QPalette role conventions (`palette.window`,
// `palette.base`, `palette.highlight`, `palette.accent`, `palette.text`). Palette roles
// automatically propagate down the component hierarchy from parents to children, allowing
// entire dialogs or subtrees to adopt contextual themes without per-widget styling code.
//
// # Hot Reloading and Testing
//
// Applications can be run via [Program] with file-watching hot reload enabled ([HotReload]).
// When QML source files change on disk, [Program.Reload] parses the new layout and performs an
// in-place reconciliation, retaining widget focus, text cursor positions, and scroll offsets.
//
// For test suites, [Check] and the companion package [github.com/yongjohnlee80/golib/tui/decl/decltest]
// provide static validation ("qmllint") and headless virtual terminal testing.
package decl
