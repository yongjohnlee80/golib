package web

import (
	"context"
	"errors"
	"fmt"
	"time"

	"net/http"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/server/ws"
	"github.com/yongjohnlee80/golib/tui"
)

// Close codes this package uses. RFC 6455 §7.4.
const (
	closeNormal      = ws.StatusNormalClosure
	closeGoingAway   = ws.StatusGoingAway
	closeTooBig      = ws.StatusMessageTooBig
	closePolicy      = ws.StatusCode(1008) // Policy Violation
	closeUnsupported = ws.StatusCode(1003) // Unsupported Data
)

// conn is the transport surface a session loop needs.
//
// An interface, not *ws.Session, so the loop is testable without a real socket.
// The parts under test here — the authenticate-then-attach ordering, the rate
// limits, the overload close — are logic, and a real WebSocket would only add
// ways for those tests to be slow and flaky.
type conn interface {
	ReadJSON(ctx context.Context, v any) error
	WriteJSON(ctx context.Context, v any) error
	Close(code ws.StatusCode, reason string) error
}

// requestInfo carries the HTTP context of the handshake into the session loop.
//
// A struct rather than a bare *http.Request so the loop's dependency is explicit
// and a test can supply one without constructing a server.
type requestInfo struct {
	http *http.Request
}

// sessionLoop runs one authenticated attach: read pump, write pump, teardown.
type sessionLoop struct {
	cfg     Config
	mgr     *Manager
	log     logger.Logger
	limits  Limits
	now     func() time.Time
	sleep   func(context.Context, time.Duration)
	decoder *decoder

	// limiter is created by readPump and shared with deliver, so one connection
	// has one token bucket rather than one per event batch.
	limiter *bucket
}

// serve handles one connection from handshake to teardown.
//
// The order is the security contract of §2.8 and it is deliberate:
//
//  1. handshake checks — Origin and Host, credential-free, so a cross-origin
//     probe never reaches the auth machinery;
//  2. the FIRST message, which carries credentials and measurements;
//  3. Policy.Authenticate;
//  4. only then create or attach a session, which is where an App first exists.
//
// No App is created and no input is accepted before step 3 completes. The
// Manager enforces that structurally by requiring an *auth.Identity, so this
// ordering cannot be inverted by a refactor without the compiler objecting.
func (l *sessionLoop) serve(ctx context.Context, c conn, req requestInfo) error {
	if err := l.cfg.checkHandshake(req.http); err != nil {
		logHandshakeDenial(l.log, req.http, err)
		// Deliberately uninformative to the client: a handshake refusal that
		// explained itself would tell a prober which control it tripped.
		_ = c.Close(closePolicy, "forbidden")
		return err
	}

	// Step 2. The first message must be a hello; anything else is a protocol
	// error, not an opportunity to guess what the client meant.
	var first clientMessage
	if err := c.ReadJSON(ctx, &first); err != nil {
		_ = c.Close(closeUnsupported, "malformed hello")
		return fmt.Errorf("web: reading hello: %w", err)
	}
	if first.T != msgHello {
		_ = c.Close(closePolicy, "expected hello")
		return fmt.Errorf("web: first message was %q, want hello", sanitizeHeader(first.T))
	}
	h := first.hello()
	if !h.valid() {
		_ = c.Close(closePolicy, "unmeasured client")
		return errors.New("web: hello carried no usable size or font metrics")
	}

	// Step 3. Every attach re-runs the completed policy from scratch. Nothing is
	// resurrected: a reconnect authenticates again, and on the ticket branch
	// auth/token performs the atomic consume, so a replayed ticket is refused by
	// the credential layer rather than by anything here.
	// A per-request sink, so a refused attach can hand the user a correlation ID.
	// The client-visible error says nothing by design, which leaves a user with
	// nothing to quote; the ID is random and outcome-independent, so it is safe to
	// return and it is the only thing that makes a refusal diagnosable.
	var attemptID string
	authCtx := auth.WithAttemptSink(ctx, func(a auth.Attempt) { attemptID = a.ID })
	identity, err := l.cfg.Policy.Authenticate(authCtx, authRequest(req.http, first))
	if err != nil {
		logger.Notice(l.log, sessionAudit{Kind: "denied", Reason: "authentication failed",
			ID: attemptID})
		// One uniform refusal. Which factor failed is the audit record's
		// business, never the client's — but the attempt ID is safe to return.
		_ = c.Close(closePolicy, "unauthorized ref="+sanitizeHeader(attemptID))
		return err
	}

	// Step 4. An App may now exist.
	sess, err := l.bind(ctx, first, identity, h)
	if err != nil {
		_ = c.Close(closePolicy, "unavailable")
		return err
	}
	// Lease-scoped, so a slow teardown cannot detach a connection that has
	// already replaced this one.
	lease := sess.Lease()
	defer l.mgr.Detach(sess.ID(), lease)

	if err := c.WriteJSON(ctx, serverMessage{T: msgReady, Session: sess.ID()}); err != nil {
		return err
	}

	return l.pump(ctx, c, sess)
}

// bind creates a new session or attaches to an existing one.
//
// A client that presents a session id is reconnecting; one that does not wants a
// new session. Both paths have already authenticated, and the attach path
// additionally verifies the principal OWNS that session.
func (l *sessionLoop) bind(ctx context.Context, m clientMessage, id *auth.Identity, h Hello) (*Session, error) {
	if m.Session != "" {
		s, err := l.mgr.Attach(m.Session, id, h)
		if err == nil {
			return s, nil
		}
		if !errors.Is(err, ErrUnknownSession) {
			// A mismatch is a refusal, not a reason to hand out a fresh session:
			// silently creating one would turn an authorization failure into a
			// success from the client's point of view.
			return nil, err
		}
		// The session expired while the client was away. A new one is the right
		// answer, and the client learns its new id from the ready message.
	}
	s, err := l.mgr.Create(ctx, id, h)
	if err != nil {
		return nil, err
	}
	if _, err := l.mgr.Attach(s.ID(), id, h); err != nil {
		l.mgr.Close(s.ID())
		return nil, err
	}
	return s, nil
}

// pump runs the read and write loops until either ends.
func (l *sessionLoop) pump(ctx context.Context, c conn, sess *Session) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errs := make(chan error, 2)
	go func() { errs <- l.readPump(ctx, c, sess) }()
	go func() { errs <- l.writePump(ctx, c, sess) }()

	// Whichever direction fails first ends the session: a half-open connection
	// is worse than a closed one, because the App keeps rendering into a void.
	err := <-errs
	cancel()
	_ = c.Close(closeNormal, "bye")
	// Drain the other direction so its goroutine cannot outlive this function.
	<-errs
	return err
}

// readPump decodes client messages into events.
func (l *sessionLoop) readPump(ctx context.Context, c conn, sess *Session) error {
	backend := sess.Backend()
	l.limiter = newBucket(l.limits.EventsPerSecond, l.limits.Burst, l.now)

	for {
		var m clientMessage
		if err := c.ReadJSON(ctx, &m); err != nil {
			if ctx.Err() != nil {
				return nil // our own shutdown, not a failure
			}
			// A read error is a CONNECTION failure, not a session failure. Calling
			// Fail here stopped the Backend and so killed the App, which made the
			// detach window unreachable — a dropped socket destroyed the user's
			// work (lector r1). The session survives; the manager evicts it if
			// nobody comes back.
			logger.Info(l.log, protocolNote{What: "read", Reason: err.Error()})
			return err
		}

		// An ack costs nothing and is not rate limited: throttling it would slow
		// the very mechanism that lets frames flow.
		if m.T == msgAck {
			backend.AckFrame(m.Rev)
			continue
		}
		// A resize is state, not input, and is likewise not metered — a client
		// dragging a window edge must not be treated as a flood.
		if m.T == msgResize {
			if err := backend.Resize(m.Cols, m.Rows); err != nil {
				if errors.Is(err, ErrGridTooLarge) {
					// A client asking for a grid we will not allocate is a
					// protocol violation, not a transient condition: it will
					// keep asking.
					logger.Notice(l.log, protocolNote{What: msgResize, Reason: err.Error()})
					_ = c.Close(closePolicy, "grid too large")
					return err
				}
				if errors.Is(err, ErrEventOverflow) {
					// The App has not drained; the resize is retried by the
					// client's next report rather than applied half-way.
					logger.Info(l.log, protocolNote{What: msgResize, Reason: "app busy"})
					continue
				}
				return err
			}
			continue
		}

		events, err := l.translate(m)
		if err != nil {
			// An unknown message type is dropped rather than fatal: a newer
			// client talking to an older server should degrade, not disconnect.
			logger.Info(l.log, protocolNote{What: sanitizeHeader(m.T), Reason: err.Error()})
			continue
		}

		for _, ev := range events {
			if err := l.deliver(ctx, c, sess, ev); err != nil {
				return err
			}
			if ctx.Err() != nil {
				return nil
			}
		}
	}
}

// deliver submits one event, waiting for the App rather than discarding it.
//
// "Un-coalesced" in the Backend contract means nothing is silently dropped, so
// this RETRIES until the event is accepted, the context ends, or the overload
// grace elapses — at which point the connection closes and the client is told.
// An earlier version retried once and then advanced past the event, which lost
// input while still claiming an ordered un-coalesced stream (lector r1).
func (l *sessionLoop) deliver(ctx context.Context, c conn, sess *Session, ev tui.Event) error {
	backend := sess.Backend()
	over := newOverload(l.limits.OverloadGrace, l.now)
	for {
		if wait := l.limiter.take(); wait > 0 {
			// Backpressure, not a drop.
			l.pause(ctx, wait)
			if ctx.Err() != nil {
				return nil
			}
		}
		err := backend.Submit(ev)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, ErrEventOverflow):
			if over.full() {
				logger.Notice(l.log, sessionAudit{Kind: "closed", ID: sess.ID(),
					Subject: sess.Subject(), Reason: "input overload"})
				_ = c.Close(closePolicy, "input overload")
				return ErrEventOverflow
			}
			l.pause(ctx, overloadRetry)
			if ctx.Err() != nil {
				return nil
			}
		default:
			return err // stopped
		}
	}
}

// overloadRetry is how long the read pump waits for the App to drain before
// retrying a submission.
const overloadRetry = 5 * time.Millisecond

// pause sleeps, respecting cancellation.
func (l *sessionLoop) pause(ctx context.Context, d time.Duration) {
	if l.sleep != nil {
		l.sleep(ctx, d)
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// translate maps one client message to events.
func (l *sessionLoop) translate(m clientMessage) ([]tui.Event, error) {
	switch m.T {
	case msgKey:
		if ev, ok := l.decoder.decodeKey(m.keyReport()); ok {
			return []tui.Event{ev}, nil
		}
		// A dropped key is a DECISION, not a gap: §2.9's table says so for every
		// shape that lands here.
		return nil, nil
	case msgText:
		return decodeText(m.Text), nil
	case msgPaste:
		return []tui.Event{decodePaste(m.Text)}, nil
	case msgMouse:
		if ev, ok := decodeMouse(m.mouseReport()); ok {
			return []tui.Event{ev}, nil
		}
		return nil, nil
	case msgFocus:
		return []tui.Event{decodeFocus(m.Gained)}, nil
	case msgHello:
		// A second hello on a live connection is refused rather than
		// re-authenticating mid-stream: re-authentication happens by
		// reconnecting, which is a path that already exists and is already
		// tested.
		return nil, errors.New("unexpected second hello")
	}
	return nil, errors.New("unknown message type")
}

// writePump sends frames as they become available.
func (l *sessionLoop) writePump(ctx context.Context, c conn, sess *Session) error {
	backend := sess.Backend()
	ticker := time.NewTicker(framePoll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-sess.Done():
			_ = c.WriteJSON(ctx, serverMessage{T: msgBye, Reason: "session ended"})
			return nil
		case <-ticker.C:
		}
		for {
			fr, ok := backend.NextFrame()
			if !ok {
				break
			}
			if err := c.WriteJSON(ctx, encodeFrame(fr)); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				// Also a connection failure. The frame stays in the aggregate
				// because the baseline never advanced, so a reconnecting client
				// receives it.
				logger.Info(l.log, protocolNote{What: "write", Reason: err.Error()})
				return err
			}
			// Only ONE frame is in flight, so this loop sends at most one per
			// tick; the break is what the next iteration finds.
		}
	}
}

// framePoll is how often the write pump looks for a new frame.
//
// Polling rather than a condition variable: the App already decides when to
// paint, one frame is in flight at a time, and a 4ms poll costs nothing next to
// the render it is waiting for. A signal channel would add a wakeup path that
// has to be correct under Stop, for no measurable gain.
const framePoll = 4 * time.Millisecond

// protocolNote records a message the server chose not to act on.
type protocolNote struct{ What, Reason string }

func (p protocolNote) String() string {
	return "web protocol note type=" + quoteEmpty(p.What) + " reason=" + p.Reason
}
