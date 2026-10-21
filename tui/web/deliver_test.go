package web

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/tui"
)

// attachedSession is a session of loopFor's manager with one client attached.
func attachedSession(t *testing.T, m *Manager) *Session {
	t.Helper()
	s, err := m.Create(context.Background(), &auth.Identity{Subject: "alice"}, hello())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Attach(s.ID(), &auth.Identity{Subject: "alice"}, hello()); err != nil {
		t.Fatal(err)
	}
	return s
}

// An input event waits for the limiter before it is submitted: with the bucket empty, a delivery
// whose context ends during that wait submits nothing.
func TestDeliverWaitsForTheLimiterBeforeSubmitting(t *testing.T) {
	t.Parallel()
	l, m := loopFor(t, testPolicy(t), nil, BackendOptions(EventQueue(8)))
	l.limits = DefaultLimits().normalize()
	s := attachedSession(t, m)
	at := time.Now()
	limiter := newBucket(1, 1, func() time.Time { return at }) // a frozen clock: no refill
	limiter.take()                                             // the one token gone
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := l.deliver(ctx, &fakeConn{}, s, limiter, tui.KeyEvent{Code: 'z'}); err != nil {
		t.Fatalf("deliver = %v, want nil once the context ends in the wait", err)
	}
	for {
		select {
		case ev := <-s.Backend().Events():
			if k, ok := ev.(tui.KeyEvent); ok && k.Code == 'z' {
				t.Fatal("the event was submitted before the limiter let it through")
			}
			continue
		default:
		}
		break
	}
}

// auditLog records the sessionAudit reasons a loop logs.
type auditLog struct {
	mu      sync.Mutex
	reasons []string
}

func (a *auditLog) Log(_ logger.Severity, payload any) {
	if p, ok := payload.(sessionAudit); ok {
		a.mu.Lock()
		a.reasons = append(a.reasons, p.Reason)
		a.mu.Unlock()
	}
}

// An overload that outlasts its grace closes the connection, and the audit names what overflowed:
// input or a resize.
func TestOverloadAuditNamesWhatOverflowed(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what    string
		deliver func(*sessionLoop, context.Context, conn, *Session, *bucket) error
	}{
		{"input overload", func(l *sessionLoop, ctx context.Context, c conn, s *Session, b *bucket) error {
			return l.deliver(ctx, c, s, b, tui.KeyEvent{Code: 'y'})
		}},
		{"resize overload", func(l *sessionLoop, ctx context.Context, c conn, s *Session, b *bucket) error {
			return l.deliverResize(ctx, c, s, b, 40, 12)
		}},
	} {
		l, m := loopFor(t, testPolicy(t), nil, BackendOptions(EventQueue(1)))
		l.limits = DefaultLimits().normalize()
		l.limits.OverloadGrace = 0
		l.sleep = func(context.Context, time.Duration) {} // retry at once
		audit := &auditLog{}
		l.log = audit
		s := attachedSession(t, m)
		for s.Backend().Submit(tui.KeyEvent{Code: 'x'}) == nil { // fill the queue; nothing drains it
		}
		fc := &fakeConn{}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := c.deliver(l, ctx, fc, s, newBucket(1e9, 1e9, nil))
		cancel()
		if !errors.Is(err, ErrEventOverflow) {
			t.Errorf("%s: delivery returned %v, want ErrEventOverflow", c.what, err)
		}
		audit.mu.Lock()
		reasons := append([]string(nil), audit.reasons...)
		audit.mu.Unlock()
		if len(reasons) != 1 || reasons[0] != c.what {
			t.Errorf("%s: audited %q, want one %q", c.what, reasons, c.what)
		}
		fc.mu.Lock()
		closed := fc.closed
		fc.mu.Unlock()
		if !closed {
			t.Errorf("%s: the connection was not closed", c.what)
		}
	}
}
