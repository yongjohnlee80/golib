package term

import (
	"errors"
	"os"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// Typed sentinels (golib convention: fail loud, typed;).
var (
	// ErrNotTerminal reports that one of the configured file descriptors is
	// not a terminal.
	ErrNotTerminal = errors.New("term: file descriptor is not a terminal")

	// ErrClosed reports use of a stopped backend.
	ErrClosed = errors.New("term: backend is stopped")

	// ErrConsoleTooOld reports a Windows console without VT processing.
	// The supported floor is Windows 10 1809+; there is no non-VT rendering
	// path.
	ErrConsoleTooOld = errors.New("term: console lacks VT processing (Windows 10 1809+ required)")
)

const (
	defaultProbeTimeout = 250 * time.Millisecond // ADR-0002 §2.6 (rev 1)
	minProbeTimeout     = 50 * time.Millisecond
	maxProbeTimeout     = time.Second
	defaultEscTimeout   = 35 * time.Millisecond // the vim ttimeoutlen band

	// eventBufferSize is the Events() channel buffer: a small, documented
	// decoupling buffer, not a policy queue. If the consumer
	// stalls, the reader blocks; events are never dropped.
	eventBufferSize = 64

	// quietPeriod is the input-idle span after which the first new input
	// triggers an opportunistic size re-check, because a KVM switch or a
	// detached multiplexer can eat SIGWINCH.
	quietPeriod = 2 * time.Second
)

type config struct {
	in, out      *os.File
	probeTimeout time.Duration
	escTimeout   time.Duration
	altScreen    bool
	mouse        bool
	env          func(string) (string, bool)

	// sizeFn overrides the fd size query — the harness seam for pty-less
	// tests (nil means query the output fd).
	sizeFn func() (tui.Size, error)
}

func defaultConfig() config {
	return config{
		in:           os.Stdin,
		out:          os.Stdout,
		probeTimeout: defaultProbeTimeout,
		escTimeout:   defaultEscTimeout,
		altScreen:    true,
		mouse:        true,
		env:          os.LookupEnv,
	}
}

func (c *config) normalize() {
	c.probeTimeout = min(max(c.probeTimeout, minProbeTimeout), maxProbeTimeout)
	if c.escTimeout <= 0 {
		c.escTimeout = defaultEscTimeout
	}
	if c.env == nil {
		c.env = os.LookupEnv
	}
}

// Option configures Open.
type Option func(*config)

// WithTTY sets the terminal file pair. Default os.Stdin, os.Stdout.
func WithTTY(in, out *os.File) Option {
	return func(c *config) { c.in, c.out = in, out }
}

// WithProbeTimeout bounds the startup capability probe.
// Default 250ms, clamped to [50ms, 1s]. The DA1 fence means healthy terminals
// return far earlier; the timeout is only the backstop for terminals and
// multiplexers that never answer DA1.
func WithProbeTimeout(d time.Duration) Option {
	return func(c *config) { c.probeTimeout = d }
}

// WithEscTimeout sets the legacy ESC disambiguation hold.
// Default 35ms; non-positive values restore the default. Only active when
// the kitty keyboard protocol is NOT negotiated — kitty flag 1 removes the
// ambiguity entirely. Over slow SSH links, split packets can misfire a lone
// ESC before its continuation arrives; raising this timeout is the remedy.
func WithEscTimeout(d time.Duration) Option {
	return func(c *config) { c.escTimeout = d }
}

// WithoutAltScreen selects inline mode: the alternate screen (?1049) is never
// entered.
func WithoutAltScreen() Option {
	return func(c *config) { c.altScreen = false }
}

// WithoutMouse disables mouse reporting: ?1002/?1006 are never enabled.
// The capability probe still reports the terminal's actual
// SGR-mouse support in Capabilities.
func WithoutMouse() Option {
	return func(c *config) { c.mouse = false }
}

// WithEnv overrides environment lookup for the capability pre-seed.
// Default os.LookupEnv; a test seam.
func WithEnv(lookup func(string) (string, bool)) Option {
	return func(c *config) { c.env = lookup }
}
