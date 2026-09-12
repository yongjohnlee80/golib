package tui

import "errors"

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

var (
	// ErrPanic is wrapped by Run's returned error under PanicReturn.
	ErrPanic = errors.New("tui: recovered panic")

	// ErrTaskPanic is wrapped by TaskResult.Err when the task panicked.
	ErrTaskPanic = errors.New("tui: task panicked")
)
