package gui

import (
	"strings"
	"sync"

	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/text"
)

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
//	│                    sync.Pool by Scale                       │
//	│  - Reuses pre-warmed [TextShaper] instances                 │
//	│  - Avoids re-loading system font collections per task       │
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

	// Wrap splits text string s into lines no wider than width (in logical pixels), breaking on
	// whitespace. Returns the wrapped line slices and the total bounding [Size].
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
//     release when finished.
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

// Measure shapes string s with font f and returns its width and ascent.
func (m *measurer) Measure(s string, f Font) Measured { return m.t.Measure(s, f) }

// Wrap measures and breaks string s across lines constrained to the given width.
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
