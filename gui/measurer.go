package gui

import (
	"strings"
	"sync"

	"gioui.org/font/gofont"
	"gioui.org/text"
)

// Measurer measures text with a shaper of its own: the same font collection as every window's
// (the Go fonts, then the system's), at one scale, so a measure equals what the loop's shaper
// draws. It returns numbers only, and is for one goroutine at a time: a worker laying out a
// diagram off the UI loop, where the Canvas's shaper may not be used.
type Measurer interface {
	// Measure is TextShaper.Measure: s on one line, in f.
	Measure(s string, f Font) Measured
	// Wrap breaks s into lines no wider than width at its spaces (a word wider than width is a
	// line of its own), and reports the lines and the size they take, each line f.Size × 1.2
	// tall. A '\n' in s breaks a line.
	Wrap(s string, f Font, width float32) (lines []string, size Size)
}

// AcquireMeasurer takes a measurer for scale (device pixels per logical pixel) for one
// goroutine's use; release gives it back. Measurers are pooled by scale: a shaper loads its fonts
// once, so a worker reuses one rather than paying that each time. The font set is the same for
// every window and never changes while the program runs, so the scale is all a measurer differs
// by.
func AcquireMeasurer(scale float32) (m Measurer, release func()) {
	if scale <= 0 {
		scale = 1
	}
	measurers.mu.Lock()
	p := measurers.pools[scale]
	if p == nil {
		p = &sync.Pool{New: func() any {
			return &measurer{t: NewTextShaper(scale)}
		}}
		measurers.pools[scale] = p
	}
	measurers.mu.Unlock()
	mm := p.Get().(*measurer)
	return mm, func() { p.Put(mm) }
}

var measurers = struct {
	mu    sync.Mutex
	pools map[float32]*sync.Pool
}{pools: map[float32]*sync.Pool{}}

type measurer struct{ t *TextShaper }

func (m *measurer) Measure(s string, f Font) Measured { return m.t.Measure(s, f) }

func (m *measurer) Wrap(s string, f Font, width float32) ([]string, Size) {
	var lines []string
	var w float32
	widthOf := func(s string) float32 {
		x := m.t.Measure(s, f).X
		return x[len(x)-1]
	}
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if try := line + " " + word; width <= 0 || widthOf(try) <= width {
				line = try
				continue
			}
			lines = append(lines, line)
			w = max(w, widthOf(line))
			line = word
		}
		lines = append(lines, line)
		w = max(w, widthOf(line))
	}
	return lines, Size{W: w, H: float32(len(lines)) * f.Size * 1.2}
}

// NewTextShaper is a shaper of its own at scale (device pixels per logical pixel), for text laid
// out away from a window: on a worker, or in a test. It loads its fonts, so make one and keep it.
// A scale that is not positive is 1.
func NewTextShaper(scale float32) *TextShaper {
	if scale <= 0 {
		scale = 1
	}
	return &TextShaper{s: text.NewShaper(text.WithCollection(gofont.Collection())), scale: scale}
}
