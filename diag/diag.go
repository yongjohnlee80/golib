package diag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Option configures a Monitor under Start.
type Option func(*config)

type config struct {
	every  time.Duration
	step   uint64
	top    int
	socket bool
}

const (
	defaultEvery = 5 * time.Second
	defaultStep  = 64 << 20
	defaultTop   = 15
)

// minEvery is the shortest sample period: sampling reads every memory class, and a tighter
// period measures the monitor. A var so the package's tests can sample faster.
var minEvery = time.Second

// WithEvery sets the sample period; default 5s, at least one second.
func WithEvery(d time.Duration) Option { return func(c *config) { c.every = d } }

// WithStep sets the live-heap growth that takes a snapshot; default 64 MiB.
func WithStep(bytes uint64) Option { return func(c *config) { c.step = bytes } }

// WithTop sets how many allocation sites a snapshot lists; default 15.
func WithTop(n int) Option { return func(c *config) { c.top = n } }

// WithSocket serves the profiles on the directory's pprof.sock, for Fetch.
func WithSocket() Option { return func(c *config) { c.socket = true } }

// Monitor is a running memory monitor: Start makes one, Close ends it.
type Monitor struct {
	dir    string
	c      config
	lock   *dirLock
	cancel context.CancelFunc
	done   chan struct{} // closed when the sampler returns

	mu   sync.Mutex // the file and the snapshot state, shared by the sampler and Close
	f    *os.File
	enc  *json.Encoder
	n    int    // the last snapshot's number
	last uint64 // the live heap at the last snapshot; 0 before the first
	err  error  // the first write that failed; the monitor keeps going

	srv *socketServer // nil without WithSocket

	once     sync.Once
	closeErr error
}

const (
	samplesName = "samples.jsonl"
	socketName  = "pprof.sock"
	lockName    = "diag.lock"
)

// Start begins a monitor writing into dir, which it owns until Close: a second monitor on dir is
// refused. ctx's end also stops the sampler; Close still releases the directory. Start writes a
// first sample before it returns.
func Start(ctx context.Context, dir string, opts ...Option) (*Monitor, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("diag: the directory is empty")
	}
	c := config{every: defaultEvery, step: defaultStep, top: defaultTop}
	for _, o := range opts {
		o(&c)
	}
	c.every = max(c.every, minEvery)
	if c.step == 0 {
		c.step = defaultStep
	}
	if c.top <= 0 {
		c.top = defaultTop
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("diag: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("diag: %w", err)
	}
	lock, err := lockDir(filepath.Join(abs, lockName))
	if err != nil {
		return nil, err
	}
	m := &Monitor{dir: abs, c: c, lock: lock, done: make(chan struct{})}
	fail := func(err error) (*Monitor, error) {
		if m.srv != nil {
			m.srv.close()
		}
		m.lock.release()
		return nil, err
	}
	if m.n, err = lastSnapshot(abs); err != nil {
		return fail(err)
	}
	if c.socket {
		if m.srv, err = serveSocket(filepath.Join(abs, socketName)); err != nil {
			return fail(err)
		}
	}
	f, err := os.OpenFile(filepath.Join(abs, samplesName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fail(fmt.Errorf("diag: %w", err))
	}
	m.f, m.enc = f, json.NewEncoder(f)
	m.tick()
	ctx, m.cancel = context.WithCancel(ctx)
	go m.run(ctx)
	return m, nil
}

// snapshotFile matches the snapshots a directory already holds.
var snapshotFile = regexp.MustCompile(`^heap-([0-9]+)\.pb\.gz$`)

// lastSnapshot is the highest snapshot number in dir, 0 for none: a reused directory continues
// past it, and never overwrites one.
func lastSnapshot(dir string) (int, error) {
	es, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("diag: %w", err)
	}
	n := 0
	for _, e := range es {
		if m := snapshotFile.FindStringSubmatch(e.Name()); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				n = max(n, v)
			}
		}
	}
	return n, nil
}

// Dir is the directory the monitor owns, absolute.
func (m *Monitor) Dir() string { return m.dir }

// Socket is the path of the profile socket, "" without WithSocket.
func (m *Monitor) Socket() string {
	if m.srv == nil {
		return ""
	}
	return m.srv.path
}

// run samples every period until ctx ends.
func (m *Monitor) run(ctx context.Context) {
	defer close(m.done)
	t := time.NewTicker(m.c.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.tick()
		}
	}
}

// tick writes one sample, and a snapshot when the live heap has grown by a step since the last.
func (m *Monitor) tick() {
	s := Read()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.enc == nil {
		return // closed
	}
	m.write(s)
	if s.HeapLive >= m.last+m.c.step {
		m.snapshot(s)
	}
}

// write appends one line, keeping the first error for Close.
func (m *Monitor) write(v any) {
	if err := m.enc.Encode(v); err != nil && m.err == nil {
		m.err = fmt.Errorf("diag: writing %s: %w", samplesName, err)
	}
}

// Close stops the sampler and the socket, writes a last sample and a final snapshot, removes the
// socket and releases the directory. It returns the first write that failed; a second Close
// returns the same.
func (m *Monitor) Close() error {
	m.once.Do(func() {
		m.cancel()
		<-m.done
		if m.srv != nil {
			m.srv.close() // under the lock still: only this monitor's socket is removed
		}
		s := Read()
		m.mu.Lock()
		m.write(s)
		m.snapshot(s)
		if err := m.f.Close(); err != nil && m.err == nil {
			m.err = fmt.Errorf("diag: %w", err)
		}
		m.enc = nil
		m.closeErr = m.err
		m.mu.Unlock()
		m.lock.release()
	})
	return m.closeErr
}

// FromEnv reads a wrapper's switch, NAME=DIR: ok is false when it is unset or blank. A program
// starts the monitor with Start(ctx, dir, WithSocket()).
func FromEnv(name string) (dir string, ok bool) {
	dir = strings.TrimSpace(os.Getenv(name))
	return dir, dir != ""
}
