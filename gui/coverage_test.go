package gui

import (
	"runtime"
	"strings"
	"testing"
	"time"
	"weak"

	"gioui.org/font/gofont"
	"gioui.org/text"
	"github.com/go-text/typesetting/fontscan"
)

// withInstalled replaces the index of installed fonts with families, each having its runes, and
// empties covered's cache, for the test. It returns how many times the index was read.
func withInstalled(t *testing.T, families map[string]string) *int {
	t.Helper()
	reads := new(int)
	var fonts []fontscan.Footprint
	for family, runes := range families {
		var set fontscan.RuneSet
		for _, r := range runes {
			set.Add(r)
		}
		fonts = append(fonts, fontscan.Footprint{Family: family, Runes: set})
	}
	was := installedFonts
	installedFonts = func() []fontscan.Footprint { *reads++; return fonts }
	resetCoverage()
	t.Cleanup(func() { installedFonts = was; resetCoverage() })
	return reads
}

func resetCoverage() {
	coverage.Lock()
	defer coverage.Unlock()
	coverage.family = map[rune]string{}
	coverage.lacks = map[weak.Pointer[text.Shaper]]map[typefaceRune]bool{}
}

func TestARuneTheTypefaceLacksNamesAnInstalledFamilyThatHasIt(t *testing.T) {
	reads := withInstalled(t, map[string]string{"braillefont": "⠋⠙", "playfont": "⏵"})
	s := newTestShaper()
	if got := covered(s, "Go Mono", "⠋ loading"); got != "Go Mono, braillefont" {
		t.Errorf("covered(⠋ loading) = %q; want Go Mono, braillefont", got)
	}
	if got := covered(s, "Go Mono", "⏵⏵ ⠙"); got != "Go Mono, playfont, braillefont" {
		t.Errorf("covered(⏵⏵ ⠙) = %q; want each family once, in the text's order", got)
	}
	covered(s, "Go Mono", "⠋⠙⏵")
	if *reads > 3 {
		t.Errorf("the index was read %d times for three runes; each rune's family is cached", *reads)
	}
}

func TestATypefaceThatHasEveryRuneIsLeftAsItIs(t *testing.T) {
	withInstalled(t, map[string]string{"otherfont": "aé─"})
	s := newTestShaper()
	for _, str := range []string{"plain ASCII", "café ─", "é ‍️", ""} {
		if got := covered(s, "Go Mono", str); got != "Go Mono" {
			t.Errorf("covered(%q) = %q; Go Mono has these runes", str, got)
		}
	}
}

func TestNoInstalledFontHasTheRuneLeavesTheTypeface(t *testing.T) {
	reads := withInstalled(t, map[string]string{"braillefont": "⠋"})
	s := newTestShaper()
	for range 3 {
		if got := covered(s, "Go Mono", "⏵"); got != "Go Mono" {
			t.Errorf("covered(⏵) = %q; no installed font has it", got)
		}
	}
	if *reads != 1 {
		t.Errorf("the index was read %d times for one missing rune; a miss is cached too", *reads)
	}
}

func TestMacOSHiddenFontsAreNotAFallback(t *testing.T) {
	withInstalled(t, map[string]string{".lastresort": "⠋⏵", "applebraille": "⠋"})
	s := newTestShaper()
	if got := covered(s, "Go Mono", "⠋⏵"); got != "Go Mono, applebraille" {
		t.Errorf("covered(⠋⏵) = %q; LastResort's box is what the fallback replaces", got)
	}
}

func TestWhetherATypefaceLacksARuneIsAskedOfItsShaper(t *testing.T) {
	withInstalled(t, map[string]string{"braillefont": "⠋"})
	bare := newTestShaper()
	if got := covered(bare, "Go Mono", "⠋"); got != "Go Mono, braillefont" {
		t.Fatalf("covered(⠋) = %q; Go Mono lacks braille", got)
	}
	coverage.Lock()
	n := len(coverage.lacks)
	coverage.Unlock()
	other := text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts())
	covered(other, "Go Mono", "⠋")
	coverage.Lock()
	defer coverage.Unlock()
	if len(coverage.lacks) != n+1 {
		t.Errorf("another shaper reused the first one's answer; its fonts may differ")
	}
}

// The real fonts: a braille dot, which the macOS spinner drew as LastResort's box, draws from
// an installed font that has it.
func TestTheCellsDrawABrailleDotFromAnInstalledFont(t *testing.T) {
	has := false
	for _, fp := range installedFonts() {
		if fp.Runes.Contains('⠋') && !strings.HasPrefix(fp.Family, ".") {
			has = true
		}
	}
	if !has {
		t.Skip("no installed font has braille here")
	}
	resetCoverage()
	s := text.NewShaper(text.WithCollection(gofont.Collection()))
	if !glyphless(s, "Go Mono", "⠋") {
		t.Skip("Go Mono draws braille here without the fallback")
	}
	r := newRenderer(s, "Go Mono", DefaultTheme())
	r.m.ppem = 16
	gs := r.shape("⠋", false, false, 0)
	if len(gs.glyphs) == 0 || uint32(gs.glyphs[0].ID) == 0 {
		t.Errorf("⠋ drew as .notdef; an installed font has it")
	}
	ts := &TextShaper{s: s, scale: 1, fonts: Fonts{Mono: "Go Mono"}}
	if tf := ts.font("⠋ loading", Font{Family: CellFamily}).Typeface; tf == "Go Mono" {
		t.Errorf("prose text names no family for ⠋: %q", tf)
	}
}

// A shaper's answers do not keep it: one dropped is collected, and its answers with it.
func TestADroppedShaperIsCollectedWithItsAnswers(t *testing.T) {
	withInstalled(t, map[string]string{"braillefont": "⠋"})
	var gone []weak.Pointer[text.Shaper]
	for range 5 {
		s := NewTextShaper(1)
		s.Measure("café ⠋", Font{Size: 12, Family: CellFamily})
		gone = append(gone, weak.Make(s.s))
	}
	coverage.Lock()
	asked := len(coverage.lacks)
	coverage.Unlock()
	if asked < len(gone) {
		t.Fatalf("%d shapers asked of, %d kept: the test asks nothing", len(gone), asked)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		runtime.GC()
		live := 0
		for _, w := range gone {
			if w.Value() != nil {
				live++
			}
		}
		coverage.Lock()
		kept := len(coverage.lacks)
		coverage.Unlock()
		if live == 0 && kept == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after the shapers were dropped: %d still alive, %d shapers' answers kept", live, kept)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
