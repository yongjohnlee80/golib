package gui

import (
	"strings"
	"sync"

	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/golib/gui/internal/linebreak"
)

// LineHeight is the height of a line of text as a multiple of its font size: what a measurer
// sizes wrapped text at and a painter places its lines at, so the two agree.
const LineHeight = 1.2

// Thread-isolated typographical measurement and line wrapping for background workers.
//
// Because Gio's [text.Shaper] is not safe for concurrent use, background worker goroutines
// (such as asynchronous diagram layout engines, markdown formatters, and table calculators)
// cannot access the UI event loop's [Canvas.Text] shaper.
//
// # Concurrency Architecture: Off-Loop Measurement Pooling
//
// [Measurer] provides thread-safe measurement isolation by maintaining thread-local shaper
// instances pooled by display scale factor:
//
//	┌─────────────────────────────────────────────────────────────┐
//	│                    Background Worker Task                   │
//	│       (e.g., Mermaid diagram parser, Markdown layout)       │
//	└──────────────────────────────┬──────────────────────────────┘
//	                               │
//	       1. Acquire:             ▼ AcquireMeasurer(scale)
//	┌─────────────────────────────────────────────────────────────┐
//	│            A bounded pool by scale (measurerMax)            │
//	│  - Reuses pre-warmed [TextShaper] instances, held strongly  │
//	│  - Each shaper sets up its fonts once, not after every GC   │
//	│  - Waits for a release once measurerMax are leased          │
//	└──────────────────────────────┬──────────────────────────────┘
//	                               │
//	       2. Measure/Wrap:        ▼ Measurer.Measure(s, f) / Wrap(s, f, w)
//	┌─────────────────────────────────────────────────────────────┐
//	│                 Isolated Measurer Instance                  │
//	│  - Calculates identical cluster metrics as main UI loop     │
//	│  - Returns numerical bounds ([Measured], [Size]) only       │
//	└──────────────────────────────┬──────────────────────────────┘
//	                               │
//	       3. Release:             ▼ release()
//	                               Returned to pool for next task

// Measurer calculates typographical bounds and wraps multi-line text on background goroutines.
//
// Invariants:
//   - Safe for use on a single goroutine at a time.
//   - Returns numerical measurements only; does not generate GPU drawing operations.
type Measurer interface {
	// Measure computes grapheme cluster coordinates and line ascent/descent for a single line of text.
	Measure(s string, f Font) Measured

	// Wrap splits text string s into lines no wider than width (in logical pixels), breaking where
	// flow's paragraphs do: at white space, and after a CJK character or a hyphen. A word with
	// neither overflows its line. Returns the wrapped lines and their bounding [Size].
	Wrap(s string, f Font, width float32) (lines []string, size Size)
}

// AcquireMeasurer leases a [Measurer] configured for the specified display scale.
//
// Parameters:
//   - scale: physical device pixels per logical pixel (dpi ratio). Non-positive values default to 1.0.
//
// Returns:
//   - m: thread-isolated [Measurer] instance.
//   - release: cleanup callback that returns m to the internal scale pool. The caller MUST invoke
//     release when finished; a second call does nothing.
//
// A scale keeps at most [measurerMax] measurers, made as they are first needed and kept for the
// process: each one's shaper sets up its fonts once. With all of them leased, AcquireMeasurer
// waits for a release. Measuring is CPU work, so more at once would not finish sooner.
//
// It is for worker goroutines. Never call it on a UI loop: it can wait there for as long as a
// worker's layout takes (a large diagram, say), and the window stalls meanwhile. Code that measures
// on the loop keeps a [TextShaper] of its own ([NewTextShaper]), made once and used only there.
func AcquireMeasurer(scale float32) (m Measurer, release func()) {
	if scale <= 0 {
		scale = 1
	}
	measurers.mu.Lock()
	p := measurers.pools[scale]
	if p == nil {
		p = &measurerPool{}
		p.cond.L = &measurers.mu
		measurers.pools[scale] = p
	}
	for len(p.idle) == 0 && p.made >= measurerMax {
		p.cond.Wait()
	}
	var mm *measurer
	if n := len(p.idle); n > 0 {
		mm, p.idle = p.idle[n-1], p.idle[:n-1]
	} else {
		p.made++
	}
	measurers.mu.Unlock()
	if mm == nil {
		mm = &measurer{t: NewTextShaper(scale)} // outside the lock: setting up fonts takes a while
	}
	var once sync.Once
	return mm, func() {
		once.Do(func() {
			measurers.mu.Lock()
			p.idle = append(p.idle, mm)
			measurers.mu.Unlock()
			p.cond.Signal()
		})
	}
}

// measurerMax is how many measurers a scale keeps, leased and idle together. A sync.Pool dropped
// its idle ones at each garbage collection, and a document of 30 charts laid out at once made 30:
// AutoDoc made 76 text shapers in 6 minutes that way, each loading its own font state.
const measurerMax = 4

// measurerPool is one scale's measurers: made counts every one made, idle are those not leased.
type measurerPool struct {
	cond sync.Cond
	made int
	idle []*measurer
}

var measurers = struct {
	mu    sync.Mutex
	pools map[float32]*measurerPool
}{pools: map[float32]*measurerPool{}}

type measurer struct{ t *TextShaper }

// Measure shapes string s with font f and returns its width and ascent.
func (m *measurer) Measure(s string, f Font) Measured { return m.t.Measure(s, f) }

// Wrap measures and breaks string s across lines constrained to the given width: the shaper's
// Wrap.
func (m *measurer) Wrap(s string, f Font, width float32) ([]string, Size) {
	return m.t.Wrap(s, f, width)
}

var (
	_ Measurer = (*measurer)(nil)
	// A canvas's shaper measures as a leased Measurer does. It belongs to the UI loop, which draws
	// with it: work off the loop leases its own with AcquireMeasurer.
	_ Measurer = (*TextShaper)(nil)
)

// Wrap splits s into lines no wider than width (logical pixels) in f, as [Measurer.Wrap] does: at
// white space, and after a CJK character or a hyphen, a word with neither overflowing its line.
// It answers the lines and their bounding size.
func (t *TextShaper) Wrap(s string, f Font, width float32) ([]string, Size) {
	var lines []string
	var w float32
	widthOf := func(s string) float32 { return t.Measure(s, f).Width() }
	for _, para := range strings.Split(s, "\n") {
		units := linebreak.Units(para)
		if len(units) == 0 {
			lines = append(lines, "")
			continue
		}
		line := units[0].Text
		for _, u := range units[1:] {
			try := line + u.Text
			if u.Spaced {
				try = line + " " + u.Text
			}
			if width <= 0 || widthOf(try) <= width {
				line = try
				continue
			}
			lines = append(lines, line)
			w = max(w, widthOf(line))
			line = u.Text
		}
		lines = append(lines, line)
		w = max(w, widthOf(line))
	}
	return lines, Size{W: w, H: float32(len(lines)) * f.Size * LineHeight}
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
