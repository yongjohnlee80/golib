# Implementing in golib/tui

The rules every change to `tui` and its sub-packages (`decl`, `widget`, `style`, `term`, `web`, `vt`,
`pty`, `key`, `keymap`, `i18n`, …) follows. They are firm: "must" and "never" admit no exceptions
without an ADR that names the rule it overrides. Where code today breaks a rule, the code is wrong,
not the rule; fix it when you touch it, and never copy the broken pattern.

The design behind these rules is in `docs/tui/adr-0001` … `adr-0011`; `README.md` is the user guide.

## 1. Layers and dependencies

```
cmd / apps ─▶ tui/decl ─▶ tui/widget ─▶ tui (core) ─▶ tui/style, tui/key, tui/keymap, tui/i18n
                                             ▲
                           tui/term, tui/web ┘   (backends implement tui.Backend; nothing imports them back)
```

- A package imports only what is to its right. `tui` never imports `widget`; `widget` never imports
  `decl`; nothing below `decl` knows QML exists.
- A backend reaches the core only through `tui.Backend` and its optional capabilities. No package
  outside a backend names its concrete type (`*term.Backend`, `*web.Backend`).
- Production code never imports `testing` or a test helper. Test doubles live in a `…test`
  package (`decl/decltest`) or in `_test.go` files.
- A dependency is on the narrowest interface that serves: depend on the method you call, not the
  type that happens to have it (`Translator` with `Lookup`, never `*i18n.Set`).

## 2. Capabilities are interfaces

A capability is something a component, backend or model can do or needs: draw, measure, focus,
scroll, take keys, report a cursor, host a layer, copy, translate, tier itself above another.

- **Declare it as a small interface named for the capability** (`Focusable`, `Scroller`,
  `KeyBinder`, `CursorShaper`). One to three methods. Never a fat interface whose implementers stub
  methods.
- **Discover it by type assertion on the interface, at the point of use.** Absence is the "no".
  ```go
  if s, ok := n.comp.(tui.Scroller); ok { … }
  ```
- **Never reach a capability through a concrete type**: no `x.(*widget.Button)`, no `switch x.(type)`
  over concrete widgets, layers, canvases or nodes, no anonymous `interface{ … }` asserted inline.
  If you need one, the interface is missing: declare it, implement it, assert it.
- **A capability asked in more than one place gets one predicate** (`hidden(c)`, `RoleOf(c)`); the
  logic of "is X able / allowed" is never repeated inline.
- **"Can it" and "may it now" are separate.** Implementing the interface is the capability; its
  method returns current state. Never a constant-false implementation, never `Supports*`/`Enabled*`
  booleans.
- **Embedding:** a component capability embeds `tui.Component`; a sub-capability of another
  capability does not.
- **Every implementation is asserted at compile time**, next to the type:
  `var _ tui.Scroller = (*List)(nil)`.
- **Consumer-defined interfaces:** the package that uses a dependency declares the interface it
  needs (`widget.TerminalProcess` over `*pty.PTY`); the producer stays concrete.
- **New capabilities are listed** in the capability table of `doc.go` for their package, grouped by
  concern. An unlisted capability is unfinished.

## 3. One home per mechanism (DRY)

The same logic exists once. Before writing a mechanism, find its home; if the home does not exist
yet, create it there, in the package every user can reach, and move the existing copies onto it
in the same change. Never add a second copy "for now".

| Mechanism | Its one home |
|---|---|
| Text width, cells, graphemes | `tui.StringWidth` / `Surface.StringWidth`; `Base.measure` outside Render |
| A backend's latched cursor and images | `tui.CursorLatch` and `tui.ImageLatch` (`backend.go`), held as fields under the backend's own lock; a backend filters what it cannot show before it latches |
| CR/LF and paste normalization | `tui.NormalizeNewlines` (`clipboard.go`), and `tui.NewPasteEvent` for a backend's paste, called at every entry |
| Painting and wrapping text | `tui.DrawText` / `tui.WrapText` (`textpaint.go`): a zero-width cluster takes no column, and a cluster that does not fit whole is not painted |
| Colour palette, xterm-256 cube, luminance, legibility | `tui/style` only |
| Attribute bits, cursor shapes, xterm key/SGR tables | `tui/style` and one internal xterm table package, shared by `term`, `vt`, `web` |
| The backend cell grid (wide-cell continuation) | one exported grid in `tui`, used by every backend and `TestBackend` |
| Hit-testing, bubbling to the modal ceiling, focus candidacy | `hitTestNode` (an optional admit check), `pathTo` (the walk to the ceiling), `isFocusable` / `acceptsFocus` / `trapsFocus` / `hiddenWithin` in `tui` core |
| Screen ↔ local coordinates | `Rect.Translate`/`RelativeTo`/`Origin`, `Point.Translate`/`RelativeTo`, `MouseEvent.localTo`; never hand-rolled arithmetic |
| Row viewport: scroll, page, wheel step, reveal, clipping, row painting | `widget/rowviewport.go`: `revealRow`, `clampTop`, `paintRows`, `rowsWidth`, the named wheel steps; `ListStyles.cursorLook` for the cursor row. A menu level's window is `levelView` (`menulevel.go`) over the same helpers: it declares and paints the rows it shows, so what is painted is what is addressable |
| Row-view capabilities | `ContextMenuRequester` (List, Tree: ask the owner for a menu), `MenuOpener` (a menu a view opens itself: DataTable's; `*EditorMenu` is one), `RowStyler` (List, Table, Tree: `SetStyles`/`ResetStyles`); a DataTable is a `tui.Scroller` |
| The file views' source and panes | `FileSource` (rooting, `kind`, reads, `goRead` off the loop) in `widget/filesource.go`; pane framing (`FilePaneStyles`, `newFilePane`, `stylePane`) in `widget/filelist_pane.go`; restyling through `FileChooserStyler` (`SetPaneStyles`, `SetPreviewSourceHighlighting`) |
| A Tree's visible rows | `Tree.flatten`, cached and dropped by `rowsChanged` at every write to what they are made of (roots, a node's children, loaded or expanded) |
| A view delegate's Text template (decl) | `cellTemplate` in `decl/tableview_cells.go` (parse, check, present), shared by TableView and TreeView |
| A picker dialog (decl) | `newChooserDialog` in `decl/filedialog.go`, over the `chooserSpec` FileDialog and FolderDialog read |
| Press pairing (double/triple click on the same target); a positional view's row identity across owner callbacks | `pressPairer` (`Press`, `Survives`) built on `rowsEpoch` in `widget/presspair.go` (List, Tree, `TableCore.press`; FileList holds its own `rowsEpoch`) |
| Drag state machine | `widget/drag.go`: `pointerDrag[S]` (begin, capture, end, lost), `resizeGesture`, `stepCellsOf`; WindowCore's richer gesture (focus entry, gesture observers, checkpoint) is its own |
| Borders and frames | `widget/frame.go`: `paintFrame` (per edge), `drawFrame` (whole), honouring the `style.BorderStyle` a style carries; no hard-coded glyphs |
| Scrims and overlay layering | one scrim and one layer-tier capability on `OverlayHost` |
| A popup: a panel over the host, closed by Esc or a press outside, trapping focus | `popupLayer` (`popup.go`, opened with `openPopupAt`): EditorMenu's menu, the tab strip's menu, and Select's options (`selectPanel`) |
| Floating placement (rect, percent, clamp) | `Float.place` / `fixedRect`; the managed (WindowMod) layout adds only its work area and maximize |
| Text viewport (scroll, reveal, caret placement, paint loop) | one text viewport shared by TextInput, TextArea, Editor, CommandLine |
| Buffer edits | the line primitives in `textbuffer.go` (`setLine`, `insertLines`, `removeLines`, `replaceLines`, `setLines`); no other file writes `.textLines` (internal/audit) |
| Pointer policy, focus state, publishing | `widget.Base` |
| A widget's shown text, plain or a catalog message | `uiText` (`plainText`, `messageText`, `inLanguage`) in `widget/message.go`, read where it is measured and painted, never resolved into a field in Layout |
| Fatal construction errors | `fatalOf` → `errs.Fatal` |
| Parsing (expressions, SQL-ish where/order) | golib `parse` first |

Two widgets that look alike must share their mechanism, and their copies must not "disagree on
purpose": a difference in behaviour is a parameter of the shared code, named and documented.

## 4. Responsibility and size (SRP)

- **A type has one responsibility.** When it grows a second, the second becomes a module held as a
  field (never embedded, so nothing is promoted past an override). `App`, `Context`, `EditorCore`,
  `TableCore` and `term.Backend` are split this way; new code never adds to them a concern that is
  not theirs.
- **A function does one thing and fits on a screen: at most ~80 lines.** A function that switches
  on an event or command kind dispatches to one function per kind.
- **One feature per file**, opened by an all-caps banner that states the problem, the rule, and the
  rejected alternatives. Aspects shared by many widgets have their own files (`roles.go`,
  `getters.go`, `events.go`, `restyle.go`).
- **No god objects behind accessors:** an escape hatch such as `Context.App()` is not a way around a
  missing method; add the narrow method.

## 5. Extension without modification (OCP)

- **A kind is a value in a table, not a case in a switch.** Adding an action, a widget type, a
  layer, a key command or a theme means adding a value; nothing central is edited.
  - Editor actions: one spec table (`actionSpecs`) holding name, description, modes, mutation, and
    executor; never one switch per attribute.
  - decl: one `decl.Type` value holds a type's whole QML contract; registration, constructor
    properties and signals are derived from it, never copied by hand.
  - Key commands: per-widget command lists registered by the widget; `KeyCommands()` is derived,
    never a hand-maintained list.
- **Stacking, styling, layering and roles are capabilities** a type implements, never a switch the
  host maintains.
- **Base types stay closed.** `Base`, `Component`, `Backend` change only to serve every implementer;
  a need of one widget is its own capability interface.

## 6. Lifecycle, phases and threading

- **Layout measures; Render draws; neither changes state.** Geometry-derived state and events go
  through `ctx.AfterLayout(CommitKey, fn)`. A scroll offset, a span cache, a cursor clamp is never
  written in `Layout` or `Render`, and never from a function those call.
- **`Init` is re-entrant:** it resets per-mount state and registers its cleanup with `OnUnmount`.
  Setters are safe before mount; an operation that needs a mount panics with its rule.
- **One owning goroutine (the loop).** Cross-goroutine APIs enqueue and say so in their doc; a
  same-turn variant is named `Apply*`. Every type documents its concurrency model.
- **Async results carry a generation or epoch** and are dropped when stale.
- **Phase legality is checked by identity** (`handlerNode == c.node.id`), never by a boolean.
- **The runtime's state has one owner per concern.** Each is a named `App` field of its own type,
  declared in the file of its concern, which holds its rules. The table says where each part is
  written; a new writer goes in one of those files, or the table changes with it:

  | Concern | State (`App` field) | Written in |
  |---|---|---|
  | The tree | `nodes`, `byComp` | mount and unmount (`app_tree.go`); routing only reads them |
  | Focus | `focus` (`focusState`) | `focus.go` (`setFocus`, the ring), `focus_repair.go`, `focus_into.go`, and the tree and batching paths that defer or start a repair (`app_tree.go`, `context.go`, `app.go`) |
  | Pointer capture | `capture` (`captureState`) | `capture.go`, and the recogniser's state in `gesture.go` |
  | Rendering | `buf`, `native`, `images`, `faces` | the frame, between `beginFrame` and `presentFrame`; the face table (`face.go`) and `RenderOffscreen`'s face scope (`offscreen.go`) |
  | Delivery phase | `phase` (`handlerPhase`) | the phase marks around Init, Layout, Render and an Update (`app_tree.go`, `app_layout.go`, `app_render.go`, `app.go`), and `enterDelivery`/`leaveDelivery` around each handler (`action.go`, `app_routing.go`) |
  | Settings | `live` (`appLive`); `cfg` stays as configured | `SetTheme`, the `Apply*` calls, `SetGestureRecognizer` |

## 7. Input

- **Keys are keymap commands.** A widget declares its scope (`KeyScope()`), its bindings
  (`KeyBindings` via `tui.BindingsOf`) and dispatches with `ctx.KeyCommand(e, cmds…)`. No key is
  hard-coded, including prefixes, text objects and too-small-screen keys; a scheme must be able to
  move every one.
- **Unhandled input bubbles:** return false for anything not used, including a read-only refusal.
- **Pointer pairing, drag and wheel use the shared mechanisms** (§3); wheel steps and page sizes
  are the viewport's, not the widget's.
- **One key system per widget:** never a private key table beside the keymap.

## 8. Notification

- **The owner hears synchronously:** `WithOnX` callbacks for cursor, activate, change, submit.
- **An owner callback ends the operation, or the operation checks the rows survived it.** A row
  view's owner may replace or refresh the rows from its callback. "Last" is per notifying step: a
  step that tells the owner makes its own writes and its bus publish first, and the owner hook is
  that step's last act. What the operation may do after the step depends on the view's kind.
  - **Positional views** (List, Tree, TableCore, DataTable, FileList: an index names a row). A call
    that can reach an owner hook is one of three shapes:
    - the operation's last step (tail position);
    - a step run inside the view's `rowsEpoch.Survives` or `pressPairer.Press`, whose answer is
      checked before any further row-dependent effect (TableCore's Cut copies inside `Survives`,
      then cuts only if the rows survived);
    - a call to one of the view's own methods whose result is declared `live bool` (it wraps one
      of the two above), checked by the caller.

    `Survives` and `Press` are the authority whatever their results are named; the `live` name is
    required of a view's own methods only, so a survival result is told apart from any other bool
    (a yank's ok says nothing about the rows). A new source, a navigation's start (FileList's
    `loadThen`), a new listing and an owner's refresh (`List.RefreshSource`, `TableCore.Changed`)
    advance the epoch; the core's own edits arrange the view without advancing it.
  - **Keyed views** (Menu, by `ItemID`) hold no epoch. After an owner call they find the row again
    by its key (`findItem`) and recheck the state the continuation needs (`selectable`: still
    there, visible and enabled), not merely that the key exists.
  - `internal/audit/owner_callback_test.go` enforces the positional shapes; the callback-replacement
    tests, the behaviour.
- **The bus is for app-wide listeners only:** an `XEvent` with `Owner tui.NodeID` first, published
  through `Base.publish`. Never both a callback and a bus event for the same fact to the same
  listener; never subscribe app-wide to hear your own child.
- **An edit notification reports user edits only** (`ChangeEvent`, `WithOnEdit`): a programmatic
  `SetValue` never fires it. Every other notification — an operation's result, a state change, a
  layout change — reports whatever caused it, a programmatic call included, exactly as its API
  documents (`Split.SetRatio` publishes `SplitResizedEvent` once its layout commits).
- **Programmatic state changes reset dependent state through one function** (a pending operator, a
  selection anchor, a press pair), called from every entry point that changes the document.

## 9. Options, errors, enums, styles

- **Functional options:** `type XOption func(*X)`, constructors `NewX(required…, opts ...XOption)`
  that apply defaults first, skip nil options, then normalize. Names are `WithX`; a widget-specific
  option that would collide is `With<Widget>X`, consistently.
- **Programmer errors fail at construction** with `fatalOf`/`errs.Fatal{Op, Rule, Detail}`, never a
  bare string panic. **Runtime setters normalize** (clamp), or return a typed error. Never panic at
  runtime on user data.
- **Runtime errors are sentinels wrapping golib `errs` categories** (`errs.Sentinel`,
  `errs.Wrap`), lower-case, prefixed with the package (`"widget: …"`). Never `errors.New` or a
  format-less `fmt.Errorf` for an error a caller may test.
- **Enums** are `uint8` iota types with `String()` (returns `"unknown"` for out-of-range) and
  `Valid()` where they cross an API. Never hand-number values that must be unique.
- **Enum values are referenced through named constants, never string literals.** A closed set of
  values (a mode, kind, role, state, scope, wrap mode, provider kind, event kind) is a typed constant
  set; call sites, switch cases and tables name the constants. A literal of an enum value appears
  only where the constant is declared, and where outside text (QML, a config file) is parsed into it.
- **Styles** are an `XStyles` struct whose zero fields keep defaults, merged with `Inherit`, with
  `SetStyles`/`ResetStyles` at runtime. Style objects are nil-safe and immutable (`With*` returns a
  clone). Every drawn part is styleable; nothing paints a fixed colour or glyph.
- **Use the standard library:** `strconv.Itoa`, not a hand-written `itoa`.

## 10. The declarative layer (`decl`)

- One `decl.Type` per QML type, in its widget's file, as the single source of its contract.
- Properties are read only through `setter`, `into` and `readProps`; `readProps` refuses a property
  the type does not declare. A builder never hand-rolls property reading.
- Properties every item has (`visible`, `toolTipText`, palette roles, `focus`, `keyScope`, …) are
  one table, applied in one place.
- decl reaches widgets through capability interfaces only; a styled replacement of any type must
  work everywhere the original does. A type still read by its concrete type is listed in
  `readPrivately`, with its reader, and cannot be replaced; a style decorates it through
  `Style.Wrap`. Hooks check their widget (`getter`, `method`, `destroyer`, …) and refuse a foreign
  one rather than panic.
- Signals are fetched at construction through `b.Emitter`; handler errors go to the sink, never
  dropped.
- The `controls` package is written only against the exported API; a gap it needs is an exported-API
  change, not a private shortcut.

## 11. Backends (`term`, `web`, `vt`, `pty`)

- Implement `tui.Backend` plus the optional capabilities you can verify; report a capability only
  when probed (`Tri`), never by assumption.
- Output is latched and written once per frame; `Stop` is idempotent and panic-safe.
- File descriptors are reached only through `SyscallConn().Control`, never `Fd()`.
- Transport details stay private behind an internal interface; app factories receive `tui.Backend`.
- Security-relevant projections (auth requests, peer addresses) are built by one function from
  `auth`/`authhttp`, never re-assembled per route.

## 12. Tests

- Tests sit beside the feature (`capture_test.go`); black-box `package x_test` by default,
  `*_internal_test.go` for unexported behaviour, `*_keys_internal_test.go` for key tables.
- Drive the real App through the test backend or `decltest`; assert layout honesty and write
  discipline with the test backend's checks.
- **Every defect fixed gets the regression test that would have caught it**, written first and seen
  failing.
- A test file opens with a sentence stating the behaviour it pins; test names are sentences.

## 13. Comments and docs

- Comments explain **why**: the invariant, the incident, the rejected alternative. Never what the
  code says, never revision history ("an earlier version…", review round numbers): that belongs in
  the ADR.
- Exported widgets carry `# Architectural Invariants`, `# Concurrency Model` and `# Usage Examples`
  doc sections; examples compile.
- A doc comment sits on the declaration it describes and is updated in the same change as the code.
- A change to an API, a pattern or a behaviour updates, in the same commit, every user-facing
  document that shows it: the package `README.md` and `USAGE.md`, `doc.go`, the `tutorial/`
  chapters and `examples/`. No document may teach an outdated usage. A deprecated declaration
  appears only in its `_deprecated.go`, never in a document as something to write. ADRs are the
  record of a decision and are not rewritten.

## 14. Before you open a PR

- [ ] No new copy of an existing mechanism (§3); no concrete-type assertion or type switch to reach
      a capability (§2); every new capability interface listed and compile-time asserted.
- [ ] No function over ~80 lines, no new responsibility on an existing type (§4).
- [ ] A new kind added as a table value, not a switch case (§5).
- [ ] Layout/Render pure; side effects in `AfterLayout` (§6).
- [ ] Keys through the keymap; unhandled input bubbles (§7).
- [ ] Errors, options, enums and styles per §9.
- [ ] A regression test for every behaviour fixed; docs updated with the code.
