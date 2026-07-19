// Package tui is the core of golib's minimal-dependency, retained-mode
// terminal UI framework (ADR-0001): the driver seam ([Backend], [Capabilities],
// [TestBackend]), the grapheme-cluster cell buffer and render pipeline ([Cell],
// [CellAttrs], [Surface]), the concrete [Event] set, and the layout value types
// ([Rect], [Size], [Constraints]).
//
// New to the package? Start with the tutorials in tui/tutorial/ — they walk
// from a first program through widgets, focus, async tasks, and modals, and
// catalog the real-world pitfalls (tutorial/07-pitfalls.md).
//
// The runtime — App, Bus, Context, the component tree, focus, and layout
// mechanics — is specified by ADR-0004/ADR-0005 and is forthcoming; the
// concrete terminal driver lives in tui/term (ADR-0002) and is likewise
// forthcoming. The invariants below are stated here because every type in this
// package is designed around them.
//
// # The loop-goroutine invariant (ADR-0005 §2.3, normative)
//
// INVARIANT. All component state — the tree, every component's fields, focus,
// layout rects, the cell buffer — is owned by the loop goroutine. Init,
// Layout, Render, HandleEvent, bus handlers, and queued closures execute ONLY
// there. The only operations legal from other goroutines are App.Post,
// App.Update, App.Go, Bus.Publish, and Context.Post/Context.Go — all of which
// enqueue and return.
//
// Convention: code already running in a handler on the loop goroutine does not
// need App.Update — it owns the state and mutates directly; an fn enqueued
// from a handler runs in a later drain, before the next frame.
//
// # The two-seam portability contract (ADR-0001 §2.4 #5)
//
// Portability is two seams. [Backend] (what a terminal is) and [Surface] (what
// components draw on). Test/SSH/web backends are cheap and ship or are
// trivially possible; a pixel driver is possible behind the same seams but
// explicitly out of scope for v1 (ADR-0001 §1.3 N1, §4.6). Component and
// runtime code never names a platform: a real terminal, the in-memory
// [TestBackend], and any future driver all satisfy the same two interfaces.
package tui
