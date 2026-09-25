// Package decl instantiates a declarative UI schema and owns the rules that
// make that instantiation predictable. It knows nothing about any UI toolkit.
//
// # Architectural Overview
//
// A parsed schema is purely data: a tree of typed nodes with properties, handler
// names, signal subscriptions, and child definitions. Turning that static declaration
// into a live, interactive UI requires three responsibilities that are independent of
// any UI toolkit:
//
//  1. Stable identity management for every node across reloads and model updates.
//  2. Deterministic, document-order property application and dependency resolution.
//  3. A robust, re-entrant, and cycle-detecting signal/handler dispatch contract.
//
// These three responsibilities live entirely within this package. Creating widgets,
// applying concrete properties, invoking layout passes, and invalidating surfaces live
// in an [Adapter] implementation (such as golib/tui/decl).
//
//	┌────────────────────────────────────────────────────────┐
//	│ QML Schema / Source (*.qml)                            │
//	└───────────────────────────┬────────────────────────────┘
//	                            │ parse/qml + parse/js
//	                            ▼
//	┌────────────────────────────────────────────────────────┐
//	│ qml.SpecTree (typed AST, SpecNode, SpecProp, Handlers) │
//	└───────────────────────────┬────────────────────────────┘
//	                            │ Mount / Reconcile
//	                            ▼
//	┌────────────────────────────────────────────────────────┐
//	│ decl.Tree (engine: identities, binding graph, cycles)  │
//	│  ├─ Expands Modules, Components, Repeaters/Models      │
//	│  ├─ Compiles Handlers (methods, params, property reads)│
//	│  └─ Reconciles structural edits via Restructurer       │
//	└───────────────────────────┬────────────────────────────┘
//	                            │ decl.Adapter Seam
//	                            │ (Create, Apply, Destroy)
//	                            ▼
//	┌────────────────────────────────────────────────────────┐
//	│ Concrete Toolkit (e.g. tui/decl -> golib/tui widgets)  │
//	└────────────────────────────────────────────────────────┘
//
// # The Seam: Values over Invalidation Notifications
//
// The engine's output across the [Adapter] seam is an [Application]: "node N's property P
// now has value V". It is deliberately NOT a "something changed, please repaint" notification.
//
// That distinction is what makes the seam truly portable:
//   - A generic invalidation notification forces the adapter to guess what changed and
//     re-inspect state, leading to redundant queries or toolkit-specific invalidation logic.
//   - Real widgets already know best how to respond to property mutations: one property setter
//     might trigger an intrinsic size recalculation and layout invalidation, another might
//     only require a cell repaint, and a third might update focus ring geometry.
//   - Handing the adapter the typed [qml.SpecValue] allows individual widget setters to make
//     that judgement locally and idempotently.
//
// # Reload is a Reconcile
//
// [Tree.Reload] and [Tree.Reconcile] patch a mounted tree in-place to match a new schema rather
// than destroying and rebuilding the hierarchy.
//
// In-place reconciliation preserves runtime toolkit state:
//   - A node that keeps its identity retains everything the underlying toolkit attached to it,
//     such as scroll offsets, active selection, half-typed form inputs, focused widgets,
//     and in-flight asynchronous tasks.
//   - Identity is determined primarily by the declared `id:`, and secondarily by positional
//     index among remaining sibling nodes.
//   - Non-patchable edits (type changes, constructor-only property modifications, removed
//     properties without reset capability, new signals on static widgets, or child changes on
//     nodes lacking restructuring capability) trigger isolated subtree rebuilds, which are
//     explicitly catalogued and reported in [Result.Rebuilt] alongside their causes.
//
// # Optional Adapter Capabilities
//
// While [Adapter] defines the minimal lifecycle seam ([Adapter.Create], [Adapter.Apply],
// [Adapter.Destroy]), adapters can implement optional capability interfaces discovered via
// runtime type assertion:
//
//	Capability          Method(s)                          Purpose
//	──────────────────  ─────────────────────────────────  ──────────────────────────────────────────
//	[Classifier]        ClassifyProperty(type, prop)       Distinguishes runtime vs ctor-only props.
//	[Restructurer]      CanRestructure, Insert, Remove, Move Enables in-place child splicing.
//	[Resetter]          Resettable, Reset                  Restores removed properties to defaults.
//	[RootVetter]        VetRoot                            Validates if a node type can be root.
//	[Vocabulary]        TypeNames                          Prevents component name collisions.
//	[Methods]           MethodsOf, Invoke                  Allows handlers to call node methods by id.
//	[SignalParameters]  SignalParametersOf                 Names arguments carried by emitted signals.
//	[PropertyReader]    ReadablesOf, ReadProperty          Allows handlers to read node props by id.
//
// # Concurrency Model and Thread Ownership
//
// A [Tree] is single-threaded and NOT safe for concurrent use across multiple goroutines.
// It is designed to reside entirely on the UI/event-loop goroutine demanded by the underlying
// adapter and toolkit. Callers must marshal model mutations directly to the tree's owner
// goroutine, as change subscribers execute synchronously on the mutating goroutine.
//
// When repeaters re-expand, the engine uses the scheduler supplied via [WithScheduler] to schedule
// the subsequent reconciliation pass onto the owner goroutine.
package decl
