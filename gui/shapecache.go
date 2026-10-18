package gui

import (
	"github.com/yongjohnlee80/gio/op/clip"
	"github.com/yongjohnlee80/gio/text"
	"golang.org/x/image/math/fixed"
)

// shapeCache turns a run of glyphs into its outline path once, and hands the same path back
// while the run stays on screen. Gio's own path cache is one LRU of a fixed size: a frame that
// shapes more runs than that, in the same order each frame, misses on every one, and each miss
// rebuilds the outline. This cache keeps every run a recent frame used, however many there are,
// and drops a run no frame has used for keepFrames: its size follows the screen, not a constant.
//
// A run is keyed as Gio keys it: its glyphs' IDs (which carry the face and the size) and their X
// offsets from the first, so the same text at another place on the screen is the same run. Owned
// by the UI loop, as the shaper it fronts.
type shapeCache struct {
	s      *text.Shaper
	runs   map[uint64][]*shapedRun // by runHash; a collision is chained, and told apart by sameRun
	frame  uint64                  // the frame now being drawn: endFrame advances it
	shaped int                     // runs shaped (cache misses), for the package's tests
}

// keepFrames is how many frames a run is kept after the last one that used it: a view hidden for
// a frame does not lose its runs.
const keepFrames = 2

type shapedRun struct {
	glyphs []runGlyph
	path   clip.PathSpec
	used   uint64 // the last frame that asked for it
}

type runGlyph struct {
	id text.GlyphID
	x  fixed.Int26_6 // from the run's first glyph
}

func newShapeCache(s *text.Shaper) *shapeCache {
	return &shapeCache{s: s, runs: map[uint64][]*shapedRun{}}
}

// Shape is gs's outline path, as text.Shaper.Shape draws it: the glyphs placed by their X from
// the first one's, on one baseline at the path's origin.
func (c *shapeCache) Shape(gs []text.Glyph) clip.PathSpec {
	h := runHash(gs)
	for _, r := range c.runs[h] {
		if sameRun(r.glyphs, gs) {
			r.used = c.frame
			return r.path
		}
	}
	c.shaped++
	r := &shapedRun{glyphs: runOf(gs), path: c.s.Shape(gs), used: c.frame}
	c.runs[h] = append(c.runs[h], r)
	return r.path
}

// endFrame closes a frame: a run no frame has used for keepFrames is dropped.
func (c *shapeCache) endFrame() {
	for h, rs := range c.runs {
		kept := rs[:0]
		for _, r := range rs {
			if c.frame-r.used < keepFrames {
				kept = append(kept, r)
			}
		}
		if len(kept) == 0 {
			delete(c.runs, h)
		} else {
			c.runs[h] = kept
		}
	}
	c.frame++
}

// clear drops every run: the window's face or size changed, so no run will be asked for again.
func (c *shapeCache) clear() { clear(c.runs) }

func runHash(gs []text.Glyph) uint64 {
	if len(gs) == 0 {
		return 0
	}
	h := uint64(14695981039346656037)
	first := gs[0].X
	for _, g := range gs {
		h ^= uint64(g.X - first)
		h *= 1099511628211
		h ^= uint64(g.ID)
		h *= 1099511628211
	}
	return h
}

func runOf(gs []text.Glyph) []runGlyph {
	out := make([]runGlyph, len(gs))
	for i, g := range gs {
		out[i] = runGlyph{id: g.ID, x: g.X - gs[0].X}
	}
	return out
}

func sameRun(r []runGlyph, gs []text.Glyph) bool {
	if len(r) != len(gs) {
		return false
	}
	for i, g := range gs {
		if r[i].id != g.ID || r[i].x != g.X-gs[0].X {
			return false
		}
	}
	return true
}
