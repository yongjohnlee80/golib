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

// DiagramLanguages is a Diagrammer that names the languages it draws. An HTMLView draws a <pre>
// as a picture when one of its classes is such a language; with a Diagrammer that does not name
// them, the native ones (mermaid, vega-lite and vegalite). Diagrams names its engines', then its
// fallback's.
type DiagramLanguages interface {
	// DiagramLanguage is the language a fence's info or a pre's class spells as name, by its
	// engine's own name (vegalite is vega-lite), and whether it is one.
	DiagramLanguage(name string) (lang string, ok bool)
}

// DiagramEngine lays out a diagram language for [Diagrams], which gives it the cache, one job per
// diagram whoever waits for it, and the fallback. golib's are Mermaid and Vega-Lite; a host adds
// its own, or replaces one, with Diagrams' engines.
type DiagramEngine interface {
	// Langs are the language's spellings, as a fence's info or a pre's class gives them; the
	// first is its name.
	Langs() []string
	// Lay lays req out on a worker, measuring with m, which is the job's until Lay returns. ctx
	// is done when no one waits for the diagram any more. An error that is a
	// *[github.com/yongjohnlee80/golib/parse/mermaid.SyntaxError] is drawn in place, as a picture
	// of the error and its line.
	Lay(ctx context.Context, req DiagramRequest, m gui.Measurer) (LaidPicture, error)
	// Declines reports whether err hands the diagram to the fallback: one the engine does not
	// draw, or one over a limit. Another error draws the source as code.
	Declines(err error) bool
}

// LaidPicture is a diagram or chart laid out: what it paints, and its size. A [DiagramEngine]
// returns one; mermaid.Lay's and vegalite.Lay's are ones.
type LaidPicture interface {
	gui.View
	Size() gui.Size
}

// DiagramRequest is a diagram block to draw. Its Theme's fonts name their families as the
// window resolves them (no "" or [gui.CellFamily]): a diagram is laid out on a worker, whose shaper
// knows no window, and must be measured in the family the window draws it in.
type DiagramRequest struct {
	Lang, Src string
	Width     float32
	Scale     float32
	Theme     Theme
	// Declined is why golib's engine handed the block to the fallback: the error it declined it
	// with (vegalite's ErrUnsupported "a rule coloured by a field", say), for a fallback that says
	// why it draws what it draws. nil when no engine of golib's lays the language out.
	Declined error
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
