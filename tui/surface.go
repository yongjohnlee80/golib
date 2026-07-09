package tui

import (
	"github.com/yongjohnlee80/golib/tui/internal/grapheme"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Surface is what components render onto: a clipped, offset view into the
// frame's cell buffer, carrying the style-resolution context
// (ADR-0003 §2.4 — the second portability seam, ADR-0001 §2.4 #5).
type Surface interface {
	// SetCell writes one grapheme cluster at surface-local (x, y).
	// content must be a single cluster; if it contains more than one,
	// only the first is written (callers use Graphemes to iterate text).
	// Width is measured internally (ADR-0003 §2.7), under the Surface's
	// width policy, and cached on the Cell.
	// Writes outside the clip are silently dropped (W3 applies).
	SetCell(x, y int, content string, st style.Style)

	// Fill sets every cell in r (clipped) to content/st. Fill with a
	// width-2 cluster fills in steps of two columns; a trailing odd
	// column, if any, is filled with a SPACE cell in st — never left
	// untouched (ADR-0003 §2.4 rev 1; W3 still forbids a half-painted
	// cluster).
	Fill(r Rect, content string, st style.Style)

	// Sub returns a child Surface clipped to r ∩ bounds, with r's origin
	// as the child's (0,0). Sub of Sub composes; the style context flows
	// to the child unchanged. Cheap: a view header, no cell copying.
	Sub(r Rect) Surface

	Size() Size

	// StringWidth measures s under the App-configured width policy
	// (WithWidthPolicy, ADR-0005). NORMATIVE: components MUST measure
	// text through the Surface (or Context) — never the package-level
	// default — so the per-App policy is honored (ADR-0003 §2.7).
	StringWidth(s string) int

	// Resolution context (ADR-0006): the theme, the negotiated terminal
	// capabilities, and the width policy travel WITH the surface, so
	// components and style resolution need no globals and tests can
	// inject all three.
	Theme() *style.Theme
	Caps() Capabilities
}

// renderContext is the shared style-resolution context every Surface view of
// one frame carries (ADR-0003 §2.4, ADR-0006 §2.6): theme, capabilities,
// width policy, theme generation, and the bounded resolution cache. It is
// owned by the runtime and touched only on the loop goroutine.
type renderContext struct {
	theme    *style.Theme
	caps     Capabilities
	policy   WidthPolicy
	themeGen uint32
	res      resolver
}

// newRenderContext builds a render context. theme may not be nil in normal
// operation (the runtime always installs one); a nil theme resolves tokens
// to the terminal default.
func newRenderContext(theme *style.Theme, caps Capabilities, policy WidthPolicy) *renderContext {
	return &renderContext{theme: theme, caps: caps, policy: policy}
}

// resolve runs st through the cached resolver under this context.
func (rc *renderContext) resolve(st style.Style) CellAttrs {
	return rc.res.resolve(st, ResolveContext{Theme: rc.theme, Caps: rc.caps, ThemeGen: rc.themeGen})
}

// bufSurface is the buffer-backed Surface implementation: a view header
// (origin + clip) over the frame's cell buffer. Sub composes by intersecting
// clips; no cells are copied.
type bufSurface struct {
	buf        *buffer
	ctx        *renderContext
	orgX, orgY int  // buffer coordinates of surface-local (0, 0)
	w, h       int  // nominal size (Size(); may exceed the clip)
	clip       Rect // buffer-coordinate clip: this view ∩ every ancestor
}

var _ Surface = (*bufSurface)(nil)

// newRootSurface returns the frame's root Surface: the whole buffer, clip =
// the full grid.
func newRootSurface(buf *buffer, ctx *renderContext) *bufSurface {
	return &bufSurface{
		buf:  buf,
		ctx:  ctx,
		w:    buf.w,
		h:    buf.h,
		clip: Rect{X: 0, Y: 0, W: buf.w, H: buf.h},
	}
}

// firstCluster returns the first grapheme cluster of s ("" when s is empty).
func firstCluster(s string) string {
	for c := range grapheme.Clusters(s) {
		return c
	}
	return ""
}

// measure returns the cell width (1 or 2) of cluster under the surface's
// policy. A cluster of only zero-width runes (a lone combining mark or ZWJ
// written standalone) still occupies the cell it was written to, so 0
// clamps to 1.
func (s *bufSurface) measure(cluster string) uint8 {
	w := grapheme.ClusterWidth(cluster, s.ctx.policy.ambiguousWide())
	if w < 1 {
		return 1
	}
	if w > 2 {
		return 2
	}
	return uint8(w)
}

// SetCell implements Surface.
func (s *bufSurface) SetCell(x, y int, content string, st style.Style) {
	cluster := firstCluster(content)
	if cluster == "" {
		return
	}
	s.set(x, y, cluster, s.measure(cluster), s.ctx.resolve(st))
}

// set writes one measured, resolved cell at surface-local (x, y), enforcing
// the clip (out-of-clip writes are dropped silently) and W3 against the clip
// edge (a wide cell whose continuation would leave the clip is dropped
// whole). The buffer's own write path re-enforces W1/W3 against the grid.
func (s *bufSurface) set(x, y int, content string, width uint8, attrs CellAttrs) {
	ax, ay := s.orgX+x, s.orgY+y
	if !s.clip.Contains(ax, ay) {
		return
	}
	if width == 2 && !s.clip.Contains(ax+1, ay) {
		return // W3 at the clip edge: never half-painted
	}
	s.buf.setCell(ax, ay, Cell{Content: content, Width: width, Attrs: attrs})
}

// Fill implements Surface.
func (s *bufSurface) Fill(r Rect, content string, st style.Style) {
	cluster := firstCluster(content)
	if cluster == "" {
		return
	}
	width := s.measure(cluster)
	attrs := s.ctx.resolve(st)

	// Effective area: r ∩ clip, in surface-local coordinates.
	local := Rect{X: s.clip.X - s.orgX, Y: s.clip.Y - s.orgY, W: s.clip.W, H: s.clip.H}
	area := r.Intersect(local)
	if area.Empty() {
		return
	}
	for y := area.Y; y < area.Y+area.H; y++ {
		if width == 1 {
			for x := area.X; x < area.X+area.W; x++ {
				s.set(x, y, cluster, 1, attrs)
			}
			continue
		}
		// Width-2 cluster: steps of two; the trailing odd column, if any,
		// is a space cell in the fill's style (ADR-0003 §2.4 rev 1) — never
		// left untouched, and never a half-painted cluster (W3).
		x := area.X
		for ; x+1 < area.X+area.W; x += 2 {
			s.set(x, y, cluster, 2, attrs)
		}
		if x < area.X+area.W {
			s.set(x, y, " ", 1, attrs)
		}
	}
}

// Sub implements Surface.
func (s *bufSurface) Sub(r Rect) Surface {
	w, h := max(r.W, 0), max(r.H, 0)
	abs := Rect{X: s.orgX + r.X, Y: s.orgY + r.Y, W: w, H: h}
	return &bufSurface{
		buf:  s.buf,
		ctx:  s.ctx, // the style context flows to the child unchanged
		orgX: abs.X,
		orgY: abs.Y,
		w:    w,
		h:    h,
		clip: s.clip.Intersect(abs),
	}
}

// Size implements Surface.
func (s *bufSurface) Size() Size { return Size{W: s.w, H: s.h} }

// StringWidth implements Surface: measurement under the App-configured
// width policy (ADR-0003 §2.4/§2.7).
func (s *bufSurface) StringWidth(str string) int {
	return grapheme.StringWidth(str, s.ctx.policy.ambiguousWide())
}

// Theme implements Surface.
func (s *bufSurface) Theme() *style.Theme { return s.ctx.theme }

// Caps implements Surface.
func (s *bufSurface) Caps() Capabilities { return s.ctx.caps }
