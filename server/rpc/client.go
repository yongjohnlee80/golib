package rpc

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"sync"
	"time"

	"github.com/yongjohnlee80/golib/logger"
)

// Terminal client errors.
var (
	// ErrClientClosed is the terminal cause after a local Close.
	ErrClientClosed = errors.New("rpc: client closed")
	// ErrMsgIDExhausted poisons the client when the monotonic uint32 msgid
	// space runs out — ids are NEVER reused within a connection (ADR-0009);
	// reconnect for a fresh id domain.
	ErrMsgIDExhausted = errors.New("rpc: msgid space exhausted; reconnect")
	// ErrNotificationOverflow poisons the client when the bounded
	// notification queue overflows (the alternative is unbounded memory or
	// a deadlocked reader).
	ErrNotificationOverflow = errors.New("rpc: notification queue overflow")
)

// ClientOption configures a Client.
type ClientOption func(*clientConfig)

type clientConfig struct {
	logger       logger.Logger
	dialer       *net.Dialer
	maxMsgBytes  int64
	writeTimeout time.Duration
	notifBuffer  int
	onNotif      func(method string, params []any)
}

// ClientLogger sets the client's logger (default Nop).
func ClientLogger(l logger.Logger) ClientOption {
	return func(c *clientConfig) { c.logger = l }
}

// WithDialer replaces the net.Dialer used by Dial.
func WithDialer(d *net.Dialer) ClientOption {
	return func(c *clientConfig) { c.dialer = d }
}

// ClientMaxMessageBytes bounds a single message in BOTH directions
// (default 16 MiB): the inbound read window AND the staged outbound frame.
func ClientMaxMessageBytes(n int64) ClientOption {
	return func(c *clientConfig) { c.maxMsgBytes = n }
}

// ClientWriteTimeout bounds each frame's network write (default 30s).
func ClientWriteTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) { c.writeTimeout = d }
}

// NotificationBuffer sizes the bounded notification queue (default 128).
func NotificationBuffer(n int) ClientOption {
	return func(c *clientConfig) { c.notifBuffer = n }
}

// OnNotification installs the server-push callback, served by ONE dispatch
// goroutine in arrival order. Reentrancy is a supported contract — the
// reader never blocks on the callback, so a handler may Call. Each
// invocation runs inside a recover boundary (R16): a panic is logged and
// poisons the client. Without this option, notifications drop with a log
// line.
func OnNotification(fn func(method string, params []any)) ClientOption {
	return func(c *clientConfig) { c.onNotif = fn }
}

// Client is the Go side of the wire (ADR-0009): concurrent Calls with
// msgid correlation over one connection, fire-and-forget Notify, a bounded
// notification queue, and a Done/Err terminal-state signal the consumer's
// reconnect loop is driven by. No auto-reconnect: session state belongs to
// the consumer.
type Client struct {
	cfg   clientConfig
	codec Codec
	conn  net.Conn
	lim   *windowReader
	br    *bufio.Reader

	// Staged writes (the server's discipline): encode fully under the
	// capped writer, then one bounded network write.
	writeSem chan struct{}
	scratch  bytes.Buffer
	capw     cappedWriter
	scratchW *bufio.Writer

	mu      sync.Mutex
	nextID  uint64 // monotonic; ids above MaxUint32 poison
	pending map[uint32]chan clientResp
	termErr error

	done     chan struct{}
	doneOnce sync.Once
	notifCh  chan clientNotif
}

type clientResp struct {
	errVal any
	result any
}

type clientNotif struct {
	method string
	params []any
}

// Dial connects to addr and starts the reader (and, when OnNotification is
// set, the dispatcher).
func Dial(ctx context.Context, addr string, codec Codec, opts ...ClientOption) (*Client, error) {
	if codec == nil {
		panic("rpc.Dial: nil Codec")
	}
	cfg := clientConfig{
		logger:       logger.Nop{},
		dialer:       &net.Dialer{},
		maxMsgBytes:  16 << 20,
		writeTimeout: 30 * time.Second,
		notifBuffer:  128,
	}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if cfg.maxMsgBytes <= 0 {
		panic(fmt.Sprintf("rpc.Dial: ClientMaxMessageBytes must be positive, got %d", cfg.maxMsgBytes))
	}
	if cfg.writeTimeout <= 0 {
		panic(fmt.Sprintf("rpc.Dial: ClientWriteTimeout must be positive, got %v", cfg.writeTimeout))
	}
	if cfg.notifBuffer <= 0 {
		panic(fmt.Sprintf("rpc.Dial: NotificationBuffer must be positive, got %d", cfg.notifBuffer))
	}
	if cfg.logger == nil {
		cfg.logger = logger.Nop{}
	}
	conn, err := cfg.dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	lim := &windowReader{src: conn}
	c := &Client{
		cfg:      cfg,
		codec:    codec,
		conn:     conn,
		lim:      lim,
		br:       bufio.NewReader(lim),
		writeSem: make(chan struct{}, 1),
		nextID:   1,
		pending:  make(map[uint32]chan clientResp),
		done:     make(chan struct{}),
		notifCh:  make(chan clientNotif, cfg.notifBuffer),
	}
	c.capw.buf = &c.scratch
	c.scratchW = bufio.NewWriter(&c.capw)
	go c.readLoop()
	go c.dispatchNotifications()
	return c, nil
}

// Done closes exactly once when the client reaches its terminal state —
// transport poison, msgid exhaustion, or Close.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err returns the terminal cause after Done closes (nil before).
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.termErr
}

// Close is idempotent; pending and future calls fail with ErrClientClosed.
func (c *Client) Close() error {
	c.poison(ErrClientClosed)
	return nil
}

// poison publishes the terminal cause exactly once: the connection closes
// (unblocking the reader), Done closes, and every waiter observes Err.
func (c *Client) poison(cause error) {
	c.mu.Lock()
	if c.termErr == nil {
		c.termErr = cause
	}
	c.mu.Unlock()
	c.doneOnce.Do(func() {
		_ = c.conn.Close()
		close(c.done)
	})
}

// Call sends one request and blocks for its response, ctx, or the client's
// terminal state. A structured server error returns as *Error
// (errors.As-able); any other failure is the transport/terminal cause.
func (c *Client) Call(ctx context.Context, method string, params ...any) (any, error) {
	id, ch, err := c.register()
	if err != nil {
		return nil, err
	}
	defer c.unregister(id)

	if params == nil {
		params = []any{}
	}
	if err := c.send(ctx, &Message{Kind: KindRequest, ID: id, Method: method, Params: params}); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.errVal != nil {
			return nil, wireErrToError(r.errVal)
		}
		return r.result, nil
	case <-ctx.Done():
		// Abandons the WAIT, not the request: the id is never reused, and
		// a late response to it is dropped by the reader.
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.Err()
	}
}

// Notify sends a fire-and-forget notification, bounded by ctx and the
// write timeout like any other frame.
func (c *Client) Notify(ctx context.Context, method string, params ...any) error {
	if params == nil {
		params = []any{}
	}
	return c.send(ctx, &Message{Kind: KindNotification, Method: method, Params: params})
}

// register allocates the next msgid — strictly monotonic, never wrapped.
func (c *Client) register() (uint32, chan clientResp, error) {
	c.mu.Lock()
	if c.termErr != nil {
		err := c.termErr
		c.mu.Unlock()
		return 0, nil, err
	}
	if c.nextID > math.MaxUint32 {
		c.mu.Unlock()
		c.poison(ErrMsgIDExhausted)
		return 0, nil, ErrMsgIDExhausted
	}
	id := uint32(c.nextID)
	c.nextID++
	ch := make(chan clientResp, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	return id, ch, nil
}

func (c *Client) unregister(id uint32) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// send stages the frame under the outbound cap and writes it as one
// bounded network write. Admission (the write slot) selects on ctx; the
// network write is bounded by the write timeout; a frame that MAY have
// partially reached the wire poisons the connection (ADR-0009).
func (c *Client) send(ctx context.Context, m *Message) error {
	select {
	case c.writeSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.Err()
	}
	defer func() { <-c.writeSem }()

	if err := ctx.Err(); err != nil {
		return err
	}
	c.scratch.Reset()
	c.capw.remaining = c.cfg.maxMsgBytes
	c.scratchW.Reset(&c.capw)
	if err := c.codec.Write(c.scratchW, m); err != nil {
		return fmt.Errorf("rpc: encode: %w", err) // nothing reached the wire
	}
	if err := c.scratchW.Flush(); err != nil {
		return fmt.Errorf("rpc: encode: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err // staged but unsent: stream intact
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(c.cfg.writeTimeout))
	n, err := c.conn.Write(c.scratch.Bytes())
	_ = c.conn.SetWriteDeadline(time.Time{})
	if c.scratch.Cap() > scratchRetainMax {
		c.scratch = bytes.Buffer{}
		c.capw.buf = &c.scratch
	}
	if err != nil {
		if n > 0 {
			// A half-written frame is unrecoverable stream state.
			c.poison(fmt.Errorf("rpc: partial frame written: %w", err))
			return c.Err()
		}
		return fmt.Errorf("rpc: write: %w", err)
	}
	return nil
}

// readLoop demultiplexes responses to their pending calls and feeds the
// bounded notification queue. Hostile-message taxonomy (ADR-0009): the
// single tolerated anomaly is a well-formed response with no pending id
// (an abandoned wait) — dropped and logged; everything else — malformed
// frames, oversized messages, server-initiated requests — poisons.
func (c *Client) readLoop() {
	defer close(c.notifCh) // the reader is the only sender
	for {
		c.lim.reset(c.cfg.maxMsgBytes)
		m, err := c.codec.Read(c.br)
		if err != nil {
			select {
			case <-c.done:
				// Already terminal (Close/poison); keep the original cause.
			default:
				c.poison(fmt.Errorf("rpc: read: %w", err))
			}
			return
		}
		switch m.Kind {
		case KindResponse:
			c.mu.Lock()
			ch, ok := c.pending[m.ID]
			if ok {
				delete(c.pending, m.ID)
			}
			c.mu.Unlock()
			if !ok {
				c.cfg.logger.Log(logger.SeverityDebug, map[string]any{
					"rpc": "client", "event": "late response dropped", "msgid": m.ID,
				})
				continue
			}
			ch <- clientResp{errVal: m.Err, result: m.Result}
		case KindNotification:
			select {
			case c.notifCh <- clientNotif{method: m.Method, params: m.Params}:
			default:
				c.poison(ErrNotificationOverflow)
				return
			}
		default:
			// A server-initiated request is a peer contract violation for
			// this client: continuing would hide drift.
			c.poison(fmt.Errorf("rpc: peer sent a request frame (method %q)", m.Method))
			return
		}
	}
}

// dispatchNotifications serves the queue on one goroutine in arrival
// order, each callback inside an R16 recover boundary.
func (c *Client) dispatchNotifications() {
	for n := range c.notifCh {
		if c.cfg.onNotif == nil {
			c.cfg.logger.Log(logger.SeverityDebug, map[string]any{
				"rpc": "client", "event": "notification dropped (no handler)", "method": n.method,
			})
			continue
		}
		c.runNotifCallback(n)
	}
}

func (c *Client) runNotifCallback(n clientNotif) {
	defer func() {
		if rec := recover(); rec != nil {
			c.cfg.logger.Log(logger.SeverityError, map[string]any{
				"rpc": "client", "event": "notification handler panic",
				"method": n.method, "recover": rec,
			})
			c.poison(fmt.Errorf("rpc: notification handler panic on %q", n.method))
		}
	}()
	c.cfg.onNotif(n.method, n.params)
}

// wireErrToError converts a wire error value: the {code, message} shape
// becomes *Error (errors.As-able); anything else is stringified.
func wireErrToError(v any) error {
	if m, ok := v.(map[string]any); ok {
		code, cok := m["code"].(int64)
		msg, mok := m["message"].(string)
		if cok && mok {
			return &Error{Code: code, Message: msg}
		}
	}
	return fmt.Errorf("rpc: server error: %v", v)
}
