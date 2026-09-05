package tui

import (
	"errors"
	"fmt"
	"time"

	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/tui/style"
)

// PanicPolicy selects what Run does after a loop/handler panic has been
// recovered and the terminal restored.
type PanicPolicy uint8

const (
	// PanicRepanic (the default) propagates the recovered panic with its
	// original value after the terminal is restored — golib fail-loud.
	PanicRepanic PanicPolicy = iota
	// PanicReturn converts the recovered panic into an error wrapping
	// ErrPanic returned from Run.
	PanicReturn
)

// ErrPanic is wrapped by Run's returned error under PanicReturn.
var ErrPanic = errors.New("tui: recovered panic")

// ErrTaskPanic is wrapped by TaskResult.Err when the task panicked.
var ErrTaskPanic = errors.New("tui: task panicked")

// appConfig collects the option-set construction state of an App
// (pattern: server/scaffold.go:21-28's scaffoldConfig).
type appConfig struct {
	backend           Backend
	theme             *style.Theme
	minFrameInterval  time.Duration
	doubleClickWindow time.Duration
	panicPolicy       PanicPolicy
	inputQueueSize    int
	eventQueueLimit   int
	taskPoolSize      int
	widthPolicy       WidthPolicy
	taskDrainTimeout  time.Duration
	logger            logger.Logger
	trace             TraceFunc
}

// defaultAppConfig returns the documented defaults.
func defaultAppConfig() appConfig {
	return appConfig{
		minFrameInterval:  16 * time.Millisecond,
		doubleClickWindow: 400 * time.Millisecond,
		panicPolicy:       PanicRepanic,
		inputQueueSize:    256,
		eventQueueLimit:   0, // unlimited
		taskPoolSize:      16,
		widthPolicy:       WidthPolicyDefault,
		taskDrainTimeout:  5 * time.Second,
		logger:            logger.Nop{},
	}
}

// AppOption configures an App under construction.
type AppOption func(*appConfig)

// WithBackend sets the driver (REQUIRED). There is no default: the core tui
// package cannot construct term.Backend (tui/term imports tui, not vice
// versa.2), and a hidden registry/init() default is forbidden by golib
// philosophy. Real apps pass term.New(...); tests pass
// tui.NewTestBackend().
func WithBackend(b Backend) AppOption {
	return func(c *appConfig) { c.backend = b }
}

// WithTheme sets the initial style.Theme. Default: style.DefaultTheme().
func WithTheme(t *style.Theme) AppOption {
	return func(c *appConfig) { c.theme = t }
}

// WithDoubleClickWindow sets how long after a press a second press on the SAME
// cell with the SAME button still counts as a double-click (ADR-0010 §2.5).
// Default 400ms. Zero or negative disables multi-click entirely: every press
// reports Count 1.
//
// The option is as much for tests as for taste. A generous window makes the
// positive case deterministic without giving App an injectable clock, and a 1ns
// window makes the negative case deterministic the same way.
func WithDoubleClickWindow(d time.Duration) AppOption {
	return func(c *appConfig) { c.doubleClickWindow = d }
}

// WithMinFrameInterval caps the render rate: dirty marks arriving faster
// are coalesced into one frame per interval. Default 16ms (~60fps cap).
// This is a CAP, not a ticker — no dirt, no frame, no wakeup. Zero disables
// the cap (every drain with dirt renders).
func WithMinFrameInterval(d time.Duration) AppOption {
	if d < 0 {
		panic("tui: WithMinFrameInterval: negative interval")
	}
	return func(c *appConfig) { c.minFrameInterval = d }
}

// WithPanicPolicy selects what Run does after a loop/handler panic has been
// recovered and the terminal restored: PanicRepanic (default) or
// PanicReturn.
func WithPanicPolicy(p PanicPolicy) AppOption {
	return func(c *appConfig) { c.panicPolicy = p }
}

// WithInputQueueSize sets the capacity of the App-owned input intake queue
// (lane A) fed from backend.Events() (default 256; ADR-0005 §2.4). Rev 1:
// this queue — and all coalescing/overflow policy — belongs to the App, not
// the backend.
func WithInputQueueSize(n int) AppOption {
	if n < 1 {
		panic(fmt.Sprintf("tui: WithInputQueueSize: n must be >= 1 (got %d)", n))
	}
	return func(c *appConfig) { c.inputQueueSize = n }
}

// WithEventQueueLimit sets an OPTIONAL hard ceiling on pending lane-B
// program events (default: unlimited; ADR-0005 §2.4). Exceeding it PANICS
// with "tui: program event queue exceeded N — runaway producer": lane-B
// growth past any sane bound is an app bug, and apps preferring fail-fast
// crash detection over memory growth opt in here. (Rev 1, Lector Q1.)
func WithEventQueueLimit(n int) AppOption {
	if n < 1 {
		panic(fmt.Sprintf("tui: WithEventQueueLimit: n must be >= 1 (got %d)", n))
	}
	return func(c *appConfig) { c.eventQueueLimit = n }
}

// WithTaskPoolSize bounds concurrently RUNNING tasks (default 16;
// ADR-0005 §2.8).
func WithTaskPoolSize(n int) AppOption {
	if n < 1 {
		panic(fmt.Sprintf("tui: WithTaskPoolSize: n must be >= 1 (got %d)", n))
	}
	return func(c *appConfig) { c.taskPoolSize = n }
}

// WithWidthPolicy fixes the App-wide grapheme width policy (ADR-0003 §2.4:
// WidthPolicyDefault = East Asian Ambiguous narrow; WidthPolicyAmbiguousWide
// for CJK-legacy contexts). The policy travels with every Surface's
// resolution context; components measure via Surface.StringWidth to respect
// it. Default: WidthPolicyDefault.
func WithWidthPolicy(p WidthPolicy) AppOption {
	return func(c *appConfig) { c.widthPolicy = p }
}

// WithTaskDrainTimeout bounds how long Run waits for in-flight tasks after
// the tree unmounts at shutdown (default 5s; ADR-0005 §2.2 step T2).
func WithTaskDrainTimeout(d time.Duration) AppOption {
	if d < 0 {
		panic("tui: WithTaskDrainTimeout: negative timeout")
	}
	return func(c *appConfig) { c.taskDrainTimeout = d }
}

// WithLogger sets the diagnostics logger for queue high-water marks, dropped
// events, dead-lettered results, and recovered task panics (default
// logger.Nop{}; precedent server/scaffold.go:49-51's ScaffoldLogger).
func WithLogger(l logger.Logger) AppOption {
	return func(c *appConfig) { c.logger = l }
}
