// Package decl instantiates a declarative UI schema and owns the rules that
// make that instantiation predictable. It knows nothing about any UI toolkit.
//
// # What this package is for
//
// A parsed schema is data: a tree of typed nodes with properties, handler names
// and children. Turning that into a live interface needs three things that have
// nothing to do with which toolkit renders it — identity for every node, a
// deterministic order in which properties are applied, and a contract for what
// happens when a handler fires. Those live here. Creating a widget and setting a
// property live in an [Adapter].
//
// # The seam, and why it is an application rather than a notification
//
// The engine's output is an [Application]: node N's property P now has value V.
// It is NOT a "something changed, please repaint" notification.
//
// That distinction is the whole reason the seam is portable. A notification
// leaves the adapter to work out what to do, which means encoding toolkit
// knowledge somewhere; worse, it invites the adapter to invalidate on the
// engine's behalf. Real widgets already decide that for themselves, and they
// decide differently: one setter schedules a relayout because the value changes
// the widget's intrinsic size, another repaints only, another additionally
// repairs focus. Handing the adapter the VALUE lets the widget keep that
// judgement, which is the only place it can be correct.
//
// # Ownership
//
// The engine owns node identity, mount order, property application order, and
// signal emission. The adapter owns widget construction, property setters,
// handler resolution, and every consequence of a set — including whatever
// redraw the toolkit needs.
//
// # Concurrency
//
// A [Tree] is not safe for concurrent use and does not try to be. It is expected
// to live on whatever goroutine its adapter's toolkit requires, and an adapter
// whose toolkit owns state on a single goroutine is responsible for ensuring
// calls arrive there.
package decl
