package web

import "github.com/yongjohnlee80/golib/tui"

// The transport methods below are package web's connection side (sessionTransport). They were
// exported, which let an App call Submit or Fail on its own session.

// Attach binds a client to this backend.
//
// Deprecated: the connection loop's, not an App's. Not maintained; removed in the next minor version.
func (b *Backend) Attach(h Hello) error { return b.attach(h) }

// Detach records that the client went away.
//
// Deprecated: the connection loop's, not an App's. Not maintained; removed in the next minor version.
func (b *Backend) Detach() { b.detach() }

// Resize records a client-reported size change and emits the event.
//
// Deprecated: the connection loop's, not an App's. Not maintained; removed in the next minor version.
func (b *Backend) Resize(cols, rows int) error { return b.resize(cols, rows, Metrics{}) }

// Submit queues one decoded event.
//
// Deprecated: the connection loop's, not an App's. Not maintained; removed in the next minor version.
func (b *Backend) Submit(ev tui.Event) error { return b.submit(ev) }

// AckFrame records the client's acknowledgement of a revision.
//
// Deprecated: the connection loop's, not an App's. Not maintained; removed in the next minor version.
func (b *Backend) AckFrame(rev uint64) { b.ackFrame(rev) }

// NextFrame returns the frame to send, if any.
//
// Deprecated: the connection loop's, not an App's. Not maintained; removed in the next minor version.
func (b *Backend) NextFrame() (Frame, bool) { return b.nextFrame() }

// PendingFrame reports whether a frame is waiting to go out.
//
// Deprecated: the connection loop's, not an App's. Not maintained; removed in the next minor version.
func (b *Backend) PendingFrame() bool { return b.pendingFrame() }

// Fail records the transport error that ended the session, then stops.
//
// Deprecated: the connection loop's, not an App's. Not maintained; removed in the next minor version.
func (b *Backend) Fail(err error) { b.fail(err) }
