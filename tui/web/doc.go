// Package web renders an existing golib TUI in a browser (ADR-0009).
//
// It implements [github.com/yongjohnlee80/golib/tui.Backend] over a server-side
// cell grid: each Flush diff is applied to the grid, and the grid is emitted as
// HTML. The browser is a dumb surface — it displays cells and reports input. No
// Component, layout or widget code changes.
//
//	backend := web.New()
//	app := tui.NewApp(root, tui.WithBackend(backend))
//
// # What this is for, and what it is not
//
// A user who can already reach a CLI server gets its TUI in a browser without a
// second UI being written. It is deliberately not a web application: an app that
// wants a web front end should use a real front-end framework, and this package
// would be the wrong foundation for one.
//
// # Serving it
//
// [Handler.Serve] is the supported path, because it builds the listener from the
// [Config] that was validated — so a non-loopback bind without TLS cannot happen.
// [Handler.Mount] composes into a server you built, and then the bind is yours.
//
//	mgr, err := web.NewManager(func(b *web.Backend) web.Runner {
//	    return tui.NewApp(buildRoot(), tui.WithBackend(b))
//	})
//	h, err := web.NewHandler(web.Config{
//	    Addr:           "127.0.0.1:8080",
//	    Policy:         attachPolicy,
//	    AllowedOrigins: []string{"https://tui.example.com"},
//	    ExpectedHost:   "tui.example.com",
//	}, mgr)
//	return h.Serve(ctx)
//
// The documented primary deployment is an SSH local-forward against a loopback
// bind — no open port, and the SSH hop is already authenticated and encrypted.
//
// # The interesting parts are not rendering
//
// Painting a character grid as HTML is straightforward. The parts that took the
// design work, and where the bugs were:
//
//   - the frame aggregate that survives a slow client without diverging. Two
//     grids are kept — current server truth and the last baseline the client
//     ACKNOWLEDGED — and each frame is their difference. A frame that merely
//     replaces a pending one loses data permanently, because frames carry only
//     dirty cells. See [Frame].
//   - the input contract, which turns browser events into tui.Event values
//     without inventing or duplicating them. The resolution order lives in Go so
//     it is testable against the real event structs; the client owns only
//     preventDefault, which must be synchronous, and the capture-element text
//     machine, which is DOM state. Both read tables exported from here.
//   - sessions and authentication, since this exposes a terminal to a network.
//     See [Manager] and [Handler.ServeLogin].
//
// # Authentication is mandatory
//
// There is no unauthenticated mode, not even on loopback. [Config.Policy] is
// required and every attach re-runs it from scratch.
//
// A reusable secret is never an attach credential: the attach protocol carries no
// password fields at all. A password converts to a single-use ticket at
// [Handler.ServeLogin]; mTLS and the SSH challenge attach on their own.
//
// # Security notes worth reading before deploying
//
//   - A non-loopback bind without TLS refuses to start. An unspecified address is
//     not loopback.
//   - Origin validation denies by default, matches exactly, and denies an absent
//     Origin. It is enforced before the WebSocket upgrade.
//   - ExpectedHost is required and never inferred from the request.
//   - Reserved browser shortcuts are neither forwarded nor preventDefault'ed, so
//     an app cannot see them. The README lists which.
//
// # Seam report
//
// ADR-0009's second purpose was to test whether tui.Backend was drawn in the
// right place. The README carries the result in full; the summary is that the
// seam held with two costs, both in the contract's SILENCE rather than its shape:
// [Backend.Err] conflates a transport failure with a backend failure, which for a
// reconnectable backend are different events; and Flush says nothing about what
// delivery means, so every backend must invent its own guarantees. Neither
// required a change to tui.
package web
