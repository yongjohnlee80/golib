// Package picture is what a laid-out diagram or chart keeps to paint itself: the scale that fits it
// to the width asked, and its labels, shaped on the loop the first time they are drawn.
package picture

import (
	"sync"

	"github.com/yongjohnlee80/gio/f32"

	"github.com/yongjohnlee80/golib/gui"
)

// FitTo is size scaled down to width when it is wider, and the scale: 1 when it fits, or when
// width is not positive.
func FitTo(size gui.Size, width float32) (gui.Size, float32) {
	if width > 0 && size.W > width {
		s := width / size.W
		return gui.Size{W: width, H: size.H * s}, s
	}
	return size, 1
}

// Push scales c by scale about the origin, and is the Pop that undoes it; a scale of 1 pushes
// nothing. Paint defers its result: defer picture.Push(c, l.fit)().
func Push(c gui.Canvas, scale float32) (pop func()) {
	if scale == 1 {
		return func() {}
	}
	c.PushTransform(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(scale, scale)))
	return c.Pop
}

// Labels shapes each label once, on the canvas it is first drawn on. Its zero value is ready, and
// it is safe for concurrent use: a laid picture is shared between widgets.
type Labels struct {
	mu     sync.Mutex
	shaped map[labelKey]*gui.TextLayout
}

type labelKey struct {
	text string
	font gui.Font
}

// Shape is s in f, shaped by c's text shaper the first time it is asked for.
func (l *Labels) Shape(c gui.Canvas, s string, f gui.Font) *gui.TextLayout {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := labelKey{s, f}
	if t, ok := l.shaped[k]; ok {
		return t
	}
	if l.shaped == nil {
		l.shaped = map[labelKey]*gui.TextLayout{}
	}
	t := c.Text().Layout(s, f, 0)
	l.shaped[k] = t
	return t
}
