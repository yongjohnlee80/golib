package widget

import (
	"context"

	"github.com/yongjohnlee80/golib/gui"
)

// Asynchronous Diagram Rendering Pipeline:
//
//	[ Markdown / HTML Parser encounters Mermaid Fence ]
//	                       |
//	                       v
//	[ Diagrammer.Diagram(ctx, req, ready) ]
//	      /                |               \
//	     v                 v                v
//	 [ Ready ]         [ Pending ]     [ Declined ]
//	 Render Pic        Invoke ready()  Fall back to raw code block
//	 vector view       when background
//	                   job finishes
//
// Diagrammer draws a diagram block (a Mermaid fence) as a picture for HTMLView and the Editor's
// Rendered mode. golib's native Mermaid implements it, chained to an application's fallback.
type Diagrammer interface {
	// Diagram answers req. Pending: ready is called at most once, from any goroutine, when a new
	// answer for req exists; the caller marshals it to its loop (tui.Context.Post) and asks
	// again. ctx's cancellation withdraws ready: it is then never called.
	Diagram(ctx context.Context, req DiagramRequest, ready func()) DiagramAnswer
}

// DiagramRequest is a diagram block to draw. Its Theme's fonts name their families as the
// window resolves them (no "" or [gui.CellFamily]): a diagram is laid out on a worker, whose shaper
// knows no window, and must be measured in the family the window draws it in.
type DiagramRequest struct {
	Lang, Src string
	Width     float32
	Scale     float32
	Theme     Theme
}

// diagramFonts is th with its fonts' families as fonts resolves them: "" is the window's prose,
// [gui.CellFamily] its cells' face. A family th names stays, and one fonts leaves unset stays as
// it was.
func diagramFonts(th Theme, fonts gui.Fonts) Theme {
	resolve := func(f gui.Font) gui.Font {
		switch {
		case f.Family == "" && fonts.Prose != "":
			f.Family = fonts.Prose
		case f.Family == gui.CellFamily && fonts.Mono != "":
			f.Family = fonts.Mono
		}
		return f
	}
	th.Prose, th.Mono = resolve(th.Prose), resolve(th.Mono)
	return th
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
