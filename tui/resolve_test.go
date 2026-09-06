package tui

import (
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

func capsWith(p ColorProfile, dark bool) Capabilities {
	return Capabilities{ColorProfile: p, DarkBackground: dark}
}

// TestResolveDownsampling is the downsampling table: for each ColorProfile
// × each Color kind (default/ANSI/256/RGB/adaptive/token), the resolved
// output form matches the documented chain: token lookup →
// adaptive pick → truecolor→256→16→mono downsample; ANSI-16 colors are never
// upsampled; mono drops color and keeps attributes.
func TestResolveDownsampling(t *testing.T) {
	theme := style.NewTheme(style.RGB(255, 0, 0)) // TokenPrimary = pure red

	tests := []struct {
		name    string
		color   style.ColorSpec
		profile ColorProfile
		dark    bool
		want    CellColor
	}{
		// default (unresolved zero Color): stays terminal default everywhere
		{"default/truecolor", style.Default(), ProfileTrueColor, true, CellColor{}},
		{"default/mono", style.Default(), ProfileMono, true, CellColor{}},

		// ANSI-16: never upsampled — emitted as ANSI-16 on every colored profile
		{"ansi16/truecolor keeps palette form", style.ANSI(9), ProfileTrueColor, true, CellColor{Kind: CellColorANSI, Index: 9}},
		{"ansi16/256", style.ANSI(9), ProfileANSI256, true, CellColor{Kind: CellColorANSI, Index: 9}},
		{"ansi16/16", style.ANSI(9), ProfileANSI16, true, CellColor{Kind: CellColorANSI, Index: 9}},
		{"ansi16/mono drops color", style.ANSI(9), ProfileMono, true, CellColor{}},

		// ANSI-256: kept at 256+, static table to 16, dropped at mono
		{"ansi256/truecolor", style.ANSI256(196), ProfileTrueColor, true, CellColor{Kind: CellColorANSI256, Index: 196}},
		{"ansi256/256", style.ANSI256(196), ProfileANSI256, true, CellColor{Kind: CellColorANSI256, Index: 196}},
		{"ansi256/16 via static table", style.ANSI256(196), ProfileANSI16, true, CellColor{Kind: CellColorANSI, Index: 9}}, // 196 = (255,0,0) → bright red
		{"ansi256/mono", style.ANSI256(196), ProfileMono, true, CellColor{}},

		// RGB: truecolor kept; 256 via cube; gray via ramp; 16 via chained table; mono dropped
		{"rgb/truecolor", style.RGB(255, 0, 0), ProfileTrueColor, true, CellColor{Kind: CellColorRGB, R: 255}},
		{"rgb/256 nearest cube", style.RGB(255, 0, 0), ProfileANSI256, true, CellColor{Kind: CellColorANSI256, Index: 196}},
		{"rgb/256 gray ramp", style.RGB(8, 8, 8), ProfileANSI256, true, CellColor{Kind: CellColorANSI256, Index: 232}},
		{"rgb/16 chained", style.RGB(255, 0, 0), ProfileANSI16, true, CellColor{Kind: CellColorANSI, Index: 9}},
		{"rgb/mono", style.RGB(255, 0, 0), ProfileMono, true, CellColor{}},

		// adaptive: picked by DarkBackground, then downsampled
		{"adaptive/dark picks dark leaf", style.Adaptive(style.ANSI(15), style.ANSI(0)), ProfileTrueColor, true, CellColor{Kind: CellColorANSI, Index: 0}},
		{"adaptive/light picks light leaf", style.Adaptive(style.ANSI(15), style.ANSI(0)), ProfileTrueColor, false, CellColor{Kind: CellColorANSI, Index: 15}},
		{"adaptive/rgb leaf downsamples", style.Adaptive(style.RGB(0, 0, 0), style.RGB(255, 0, 0)), ProfileANSI16, true, CellColor{Kind: CellColorANSI, Index: 9}},

		// token: theme lookup then the same chain
		{"token/truecolor", style.TokenPrimary, ProfileTrueColor, true, CellColor{Kind: CellColorRGB, R: 255}},
		{"token/256", style.TokenPrimary, ProfileANSI256, true, CellColor{Kind: CellColorANSI256, Index: 196}},
		{"token/16", style.TokenPrimary, ProfileANSI16, true, CellColor{Kind: CellColorANSI, Index: 9}},
		{"token/mono", style.TokenPrimary, ProfileMono, true, CellColor{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := style.New().Foreground(tt.color)
			rc := ResolveContext{Theme: &theme, Caps: capsWith(tt.profile, tt.dark)}
			got := resolveStyle(st, rc)
			if got.FG != tt.want {
				t.Errorf("resolved FG = %+v, want %+v", got.FG, tt.want)
			}
		})
	}
}

// TestResolveThemeDarkOverride: a Theme's WithDark forces adaptivity instead
// of the probed Capabilities.DarkBackground.
func TestResolveThemeDarkOverride(t *testing.T) {
	adaptive := style.Adaptive(style.ANSI(15), style.ANSI(0))
	theme := style.NewTheme(style.ANSI(4), style.WithDark(false))
	st := style.New().Foreground(adaptive)
	// Caps say dark; theme forces light: the light leaf must win.
	rc := ResolveContext{Theme: &theme, Caps: capsWith(ProfileANSI16, true)}
	if got := resolveStyle(st, rc); got.FG != (CellColor{Kind: CellColorANSI, Index: 15}) {
		t.Errorf("WithDark(false) override ignored: FG = %+v", got.FG)
	}
}

// TestResolveAttributes: set attributes map to mask bits; unset and
// explicitly-false attributes do not; mono keeps attributes while dropping
// color.
func TestResolveAttributes(t *testing.T) {
	st := style.New().
		Foreground(style.ANSI(1)).
		Bold(true).Faint(true).Italic(true).Underline(true).
		Blink(true).Reverse(true).Strikethrough(true)
	rc := ResolveContext{Caps: capsWith(ProfileMono, true)}
	got := resolveStyle(st, rc)
	wantMask := AttrBold | AttrFaint | AttrItalic | AttrUnderline | AttrBlink | AttrReverse | AttrStrikethrough
	if got.Mask != wantMask {
		t.Errorf("mask = %b, want %b", got.Mask, wantMask)
	}
	if got.FG != (CellColor{}) {
		t.Errorf("mono kept color: %+v", got.FG)
	}
	if got := resolveStyle(style.New().Bold(false), rc); got.Mask != 0 {
		t.Errorf("Bold(false) contributed bits: %b", got.Mask)
	}
}

// TestResolverCache covers the cache contract: hit/miss,
// flush on theme-generation bump, and the size bound.
func TestResolverCache(t *testing.T) {
	theme := style.DefaultTheme()
	rc := ResolveContext{Theme: &theme, Caps: capsWith(ProfileTrueColor, true)}
	var r resolver

	t.Run("miss then hit", func(t *testing.T) {
		st := style.New().Foreground(style.RGB(1, 2, 3)).Bold(true)
		first := r.resolve(st, rc)
		if len(r.cache) != 1 {
			t.Fatalf("cache size = %d after first resolve, want 1", len(r.cache))
		}
		second := r.resolve(st, rc)
		if first != second {
			t.Fatalf("cache hit returned %+v, want %+v", second, first)
		}
		if len(r.cache) != 1 {
			t.Fatalf("cache size = %d after hit, want 1 (no new entry)", len(r.cache))
		}
	})

	t.Run("distinct styles are distinct entries", func(t *testing.T) {
		r.resolve(style.New().Foreground(style.RGB(9, 9, 9)), rc)
		if len(r.cache) != 2 {
			t.Fatalf("cache size = %d, want 2", len(r.cache))
		}
	})

	t.Run("theme generation bump flushes", func(t *testing.T) {
		bumped := rc
		bumped.ThemeGen = rc.ThemeGen + 1
		r.resolve(style.New().Foreground(style.RGB(1, 2, 3)), bumped)
		if len(r.cache) != 1 {
			t.Fatalf("cache size = %d after generation bump, want 1 (flushed)", len(r.cache))
		}
	})

	t.Run("bounded size", func(t *testing.T) {
		var r resolver
		for i := 0; i < maxResolveCacheEntries+100; i++ {
			st := style.New().Foreground(style.RGB(uint8(i), uint8(i>>8), uint8(i>>4))).Width(i)
			r.resolve(st, rc)
		}
		if len(r.cache) > maxResolveCacheEntries {
			t.Fatalf("cache size = %d, want <= %d (bounded)", len(r.cache), maxResolveCacheEntries)
		}
	})
}

// TestResolveExtrasIgnored: a style with Ext(...) set resolves identically —
// extras are never consulted by the core resolver.
func TestResolveExtrasIgnored(t *testing.T) {
	rc := ResolveContext{Caps: capsWith(ProfileTrueColor, true)}
	plain := style.New().Foreground(style.RGB(1, 2, 3)).Bold(true)
	extended := plain.Ext(style.ExtKey{Pkg: "acme/tuix", Name: "underline-color"}, "red")
	if a, b := resolveStyle(plain, rc), resolveStyle(extended, rc); a != b {
		t.Errorf("extras changed resolution: %+v vs %+v", a, b)
	}
}

// TestANSI256To16Identity: the static table maps the base 16 to themselves.
func TestANSI256To16Identity(t *testing.T) {
	for i := 0; i < 16; i++ {
		if got := ansi256To16[i]; got != uint8(i) {
			t.Errorf("ansi256To16[%d] = %d, want identity", i, got)
		}
	}
}

// TestRGBToANSI256Exact: cube corners and gray-ramp values map to their
// exact palette entries.
func TestRGBToANSI256Exact(t *testing.T) {
	tests := []struct {
		r, g, b uint8
		want    uint8
	}{
		{0, 0, 0, 16},        // cube (0,0,0)
		{255, 255, 255, 231}, // cube (5,5,5)
		{255, 0, 0, 196},     // cube (5,0,0)
		{0, 255, 0, 46},      // cube (0,5,0)
		{0, 0, 255, 21},      // cube (0,0,5)
		{8, 8, 8, 232},       // gray ramp bottom
		{238, 238, 238, 255}, // gray ramp top
		{128, 128, 128, 244}, // mid gray: ramp 128≈(8+10·12)
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("(%d,%d,%d)", tt.r, tt.g, tt.b), func(t *testing.T) {
			if got := rgbToANSI256(tt.r, tt.g, tt.b); got != tt.want {
				t.Errorf("rgbToANSI256 = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestResolveAllocs: the full resolve is allocation-free, and so is a cache
// hit.
func TestResolveAllocs(t *testing.T) {
	theme := style.DefaultTheme()
	rc := ResolveContext{Theme: &theme, Caps: capsWith(ProfileANSI256, true)}
	st := style.New().Foreground(style.TokenPrimary).Background(style.RGB(30, 30, 46)).Bold(true)

	if a := testing.AllocsPerRun(200, func() { resolveStyle(st, rc) }); a != 0 {
		t.Errorf("resolveStyle allocates %.1f/op, want 0", a)
	}

	var r resolver
	r.resolve(st, rc) // prime
	if a := testing.AllocsPerRun(200, func() { r.resolve(st, rc) }); a != 0 {
		t.Errorf("cache hit allocates %.1f/op, want 0", a)
	}
}

// BenchmarkResolve is the full-resolve benchmark (no cache).
func BenchmarkResolve(b *testing.B) {
	theme := style.DefaultTheme()
	rc := ResolveContext{Theme: &theme, Caps: capsWith(ProfileANSI256, true)}
	st := style.New().Foreground(style.TokenPrimary).Background(style.RGB(30, 30, 46)).Bold(true)
	b.ReportAllocs()
	for b.Loop() {
		resolveStyle(st, rc)
	}
}

// BenchmarkResolveCacheHit measures the cached path (~0 allocs on hit).
func BenchmarkResolveCacheHit(b *testing.B) {
	theme := style.DefaultTheme()
	rc := ResolveContext{Theme: &theme, Caps: capsWith(ProfileANSI256, true)}
	st := style.New().Foreground(style.TokenPrimary).Background(style.RGB(30, 30, 46)).Bold(true)
	var r resolver
	r.resolve(st, rc)
	b.ReportAllocs()
	for b.Loop() {
		r.resolve(st, rc)
	}
}
