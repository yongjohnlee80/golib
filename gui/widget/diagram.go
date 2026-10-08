package widget

import (
	"context"

	"github.com/yongjohnlee80/golib/gui"
)

// Diagrammer draws a diagram block (a Mermaid fence) as a picture for HTMLView and the Editor's
// Rendered mode. golib's native Mermaid implements it, chained to an application's fallback.
type Diagrammer interface {
	// Diagram answers req. Pending: ready is called at most once, from any goroutine, when a new
	// answer for req exists; the caller marshals it to its loop (tui.Context.Post) and asks
	// again. ctx's cancellation withdraws ready: it is then never called.
	Diagram(ctx context.Context, req DiagramRequest, ready func()) DiagramAnswer
}

// DiagramRequest is a diagram block to draw.
type DiagramRequest struct {
	Lang, Src string
	Width     float32
	Scale     float32
	Theme     Theme
}

// DiagramAnswer is a Diagrammer's answer.
type DiagramAnswer struct {
	State DiagramState
	Pic   gui.View // Ready: the picture. Pending: the block's last Ready picture, or nil
	Size  gui.Size // Ready: its size. Pending with nil Pic: an estimate
}

// DiagramState is how far a diagram's answer has come.
type DiagramState uint8

const (
	Declined DiagramState = iota // draw the block as code
	Pending
	Ready
)
