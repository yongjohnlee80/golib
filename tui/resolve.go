package tui

import "github.com/yongjohnlee80/golib/tui/style"

// ResolveContext carries the inputs style resolution is pure over
// (ADR-0006 §2.6): the active theme, the negotiated terminal capabilities,
// and the theme generation counter the runtime bumps on App.SetTheme.
type ResolveContext struct {
	Theme    *style.Theme
	Caps     Capabilities
	ThemeGen uint32
}

// dark is the effective background darkness for adaptive picks: the theme's
// WithDark override when forced, else the probed Capabilities.DarkBackground
// (ADR-0006 §2.4/§2.5).
func (rc ResolveContext) dark() bool {
	if rc.Theme != nil {
		if dark, forced := rc.Theme.Dark(); forced {
			return dark
		}
	}
	return rc.Caps.DarkBackground
}

// resolveStyle is the pure resolution function (ADR-0006 §2.6):
// (Style, theme generation, capability profile, dark) → CellAttrs. The chain
// per color is token lookup → adaptive pick → downsample to the profile
// (ADR-0006 §2.4). It allocates nothing; the cached entry point is
// resolver.resolve.
func resolveStyle(st style.Style, rc ResolveContext) CellAttrs {
	var a CellAttrs
	dark := rc.dark()
	if fg, ok := st.GetForeground(); ok {
		a.FG = resolveColor(fg, rc.Theme, rc.Caps.ColorProfile, dark)
	}
	if bg, ok := st.GetBackground(); ok {
		a.BG = resolveColor(bg, rc.Theme, rc.Caps.ColorProfile, dark)
	}
	// Attributes survive every profile including ProfileMono — the mono
	// step of the chain drops color and keeps attributes (ADR-0006 §2.4).
	if v, ok := st.GetBold(); ok && v {
		a.Mask |= AttrBold
	}
	if v, ok := st.GetFaint(); ok && v {
		a.Mask |= AttrFaint
	}
	if v, ok := st.GetItalic(); ok && v {
		a.Mask |= AttrItalic
	}
	if v, ok := st.GetUnderline(); ok && v {
		a.Mask |= AttrUnderline
	}
	if v, ok := st.GetBlink(); ok && v {
		a.Mask |= AttrBlink
	}
	if v, ok := st.GetReverse(); ok && v {
		a.Mask |= AttrReverse
	}
	if v, ok := st.GetStrikethrough(); ok && v {
		a.Mask |= AttrStrikethrough
	}
	return a
}

// resolveColor runs one color through the documented chain (ADR-0006 §2.4):
//
//  1. token → look up the active Theme → a concrete Color;
//  2. adaptive → pick light or dark leaf by background darkness;
//  3. downsample to the profile: TrueColor → ANSI256 (nearest in the 6×6×6
//     cube plus the 24-step gray ramp, Euclidean in RGB) → ANSI16 (static
//     256→16 mapping table) → Mono (drop color, keep attributes). A color
//     never upsamples: an ANSI-16 color on a truecolor terminal keeps its
//     ANSI-16 form so the user's palette renders it.
func resolveColor(c style.Color, th *style.Theme, profile ColorProfile, dark bool) CellColor {
	if tok, ok := c.Token(); ok {
		if th == nil {
			return CellColor{} // no theme: a token degrades to the terminal default
		}
		c = th.Color(tok) // concrete or adaptive, never a token (enforced at Theme construction)
	}
	if light, darkLeaf, ok := c.AdaptivePair(); ok {
		if dark {
			c = darkLeaf
		} else {
			c = light
		}
	}
	switch {
	case c.IsDefault():
		return CellColor{}
	case profile == ProfileMono:
		return CellColor{} // drop color, keep attributes
	}
	if n, ok := c.ANSIIndex(); ok {
		return CellColor{Kind: CellColorANSI, Index: uint8(n)}
	}
	if n, ok := c.ANSI256Index(); ok {
		if profile >= ProfileANSI256 {
			return CellColor{Kind: CellColorANSI256, Index: uint8(n)}
		}
		return CellColor{Kind: CellColorANSI, Index: ansi256To16[n]}
	}
	if r, g, b, ok := c.RGBValues(); ok {
		switch profile {
		case ProfileTrueColor:
			return CellColor{Kind: CellColorRGB, R: r, G: g, B: b}
		case ProfileANSI256:
			return CellColor{Kind: CellColorANSI256, Index: rgbToANSI256(r, g, b)}
		default: // ProfileANSI16
			return CellColor{Kind: CellColorANSI, Index: ansi256To16[rgbToANSI256(r, g, b)]}
		}
	}
	return CellColor{} // unreachable: exactly one accessor reports ok per Color
}

// --- the bounded resolution cache (ADR-0006 §2.6) ---

// resolveKey is the cache key: legal as a map key precisely because
// style.Style is comparable (ADR-0006 §2.1; styles carrying a non-nil extras
// pointer key by pointer identity — extras are never consulted by the core
// resolver, so cache correctness doesn't depend on their contents).
type resolveKey struct {
	st       style.Style
	themeGen uint32
	profile  ColorProfile
	dark     bool
}

// maxResolveCacheEntries bounds the cache. Real apps use a few dozen
// distinct styles per theme generation; on overflow the whole map is
// flushed (cheap, rare) rather than tracking an eviction order.
const maxResolveCacheEntries = 1024

// resolver is the render layer's bounded resolution cache. It is owned by
// the render context and touched only on the loop goroutine (ADR-0005 §2.3),
// so it is deliberately unsynchronized.
type resolver struct {
	cache   map[resolveKey]CellAttrs
	lastGen uint32
}

// resolve returns the resolved CellAttrs for st under rc, consulting the
// cache. The cache is flushed on a theme-generation bump (stale generations
// can never hit again — flushing reclaims their space immediately) and when
// it grows past maxResolveCacheEntries. A cache hit allocates nothing.
func (r *resolver) resolve(st style.Style, rc ResolveContext) CellAttrs {
	if rc.ThemeGen != r.lastGen {
		clear(r.cache)
		r.lastGen = rc.ThemeGen
	}
	key := resolveKey{st: st, themeGen: rc.ThemeGen, profile: rc.Caps.ColorProfile, dark: rc.dark()}
	if a, ok := r.cache[key]; ok {
		return a
	}
	a := resolveStyle(st, rc)
	if len(r.cache) >= maxResolveCacheEntries {
		clear(r.cache)
	}
	if r.cache == nil {
		r.cache = make(map[resolveKey]CellAttrs)
	}
	r.cache[key] = a
	return a
}

// --- downsampling tables (deterministic table math, no color-science
//     dependency — ADR-0006 §2.4) ---

// cubeLevels are the xterm 6×6×6 color-cube component levels.
var cubeLevels = [6]uint8{0, 95, 135, 175, 215, 255}

// ansi16RGB is the reference RGB of the 16 base SGR colors (xterm defaults),
// used only to build the static 256→16 table — emitted ANSI-16 output always
// stays palette-indexed (never upsampled).
var ansi16RGB = [16][3]uint8{
	{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0},
	{0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
	{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

// ansi256To16 is the static 256→16 mapping table (ADR-0006 §2.4): identity
// for the base 16, nearest base color (squared Euclidean in RGB, lowest
// index wins ties) for the cube and gray ramp.
var ansi256To16 = buildANSI256To16()

func buildANSI256To16() (t [256]uint8) {
	for i := range t {
		if i < 16 {
			t[i] = uint8(i)
			continue
		}
		r, g, b := ansi256RGB(uint8(i))
		best, bestDist := 0, int(^uint(0)>>1)
		for j, ref := range ansi16RGB {
			if d := sqDist(r, g, b, ref[0], ref[1], ref[2]); d < bestDist {
				best, bestDist = j, d
			}
		}
		t[i] = uint8(best)
	}
	return t
}

// ansi256RGB returns the xterm RGB value of palette index n (n >= 16: the
// 6×6×6 cube then the 24-step gray ramp).
func ansi256RGB(n uint8) (r, g, b uint8) {
	if n < 16 {
		c := ansi16RGB[n]
		return c[0], c[1], c[2]
	}
	if n < 232 {
		c := int(n) - 16
		return cubeLevels[c/36], cubeLevels[c/6%6], cubeLevels[c%6]
	}
	gray := uint8(8 + 10*(int(n)-232))
	return gray, gray, gray
}

// rgbToANSI256 maps a truecolor value to the nearest ANSI-256 index: the
// nearest 6×6×6 cube entry compared against the nearest 24-step gray-ramp
// entry, squared Euclidean in RGB; the cube wins ties (ADR-0006 §2.4).
func rgbToANSI256(r, g, b uint8) uint8 {
	ri, gi, bi := nearestCubeLevel(r), nearestCubeLevel(g), nearestCubeLevel(b)
	cubeIdx := uint8(16 + 36*ri + 6*gi + bi)
	cubeDist := sqDist(r, g, b, cubeLevels[ri], cubeLevels[gi], cubeLevels[bi])

	// Nearest gray-ramp level to the average component: gray_i = 8 + 10i.
	avg := (int(r) + int(g) + int(b)) / 3
	gi24 := (avg - 3) / 10 // round((avg-8)/10) for avg in [0, 255]
	gi24 = min(max(gi24, 0), 23)
	gray := uint8(8 + 10*gi24)
	grayIdx := uint8(232 + gi24)
	grayDist := sqDist(r, g, b, gray, gray, gray)

	if grayDist < cubeDist {
		return grayIdx
	}
	return cubeIdx
}

// nearestCubeLevel returns the index into cubeLevels closest to v (lowest
// index wins ties).
func nearestCubeLevel(v uint8) int {
	best, bestDist := 0, int(^uint(0)>>1)
	for i, l := range cubeLevels {
		if d := sqDist(v, 0, 0, l, 0, 0); d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// sqDist is the squared Euclidean distance between two RGB triples.
func sqDist(r1, g1, b1, r2, g2, b2 uint8) int {
	dr, dg, db := int(r1)-int(r2), int(g1)-int(g2), int(b1)-int(b2)
	return dr*dr + dg*dg + db*db
}
