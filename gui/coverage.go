package gui

import (
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"unicode"
	"weak"

	"github.com/go-text/typesetting/fontscan"
	"github.com/yongjohnlee80/gio/font"
	"github.com/yongjohnlee80/gio/text"
	"golang.org/x/image/math/fixed"
)

// Coverage fallback: a rune no family of a typeface has draws from an installed font that has it.
//
// Gio's shaper falls back by coverage only among the faces loaded into it. An installed font is
// reached only by a family the typeface names, or by a substitution of a generic family
// ("monospace" → DejaVu Sans Mono, which has braille on Linux; Menlo, which has none, on macOS).
// go-text's search of the installed fonts by script never runs: Gio does not tell it the script.
// A rune none of those fonts has drew as .notdef, a box: a braille spinner on macOS, ⏵ (U+23F5) in
// a terminal.
//
// covered names, after the typeface, an installed family that has each such rune, found in the
// index of installed fonts Gio's shaper reads (fontscan.SystemFonts, cached on disk). It runs
// before a shape, so a measure and a layout of the same text agree. Its answers are cached per
// typeface and rune, misses too, so a frame never searches.

// installedFonts is the index of installed fonts: each one's family (normalized: "applebraille")
// and the runes it has. It is read once, on the first rune that needs it.
var installedFonts = sync.OnceValue(func() []fontscan.Footprint {
	dir, _ := os.UserCacheDir()
	fonts, _ := fontscan.SystemFonts(log.New(io.Discard, "", 0), dir)
	return fonts
})

// coverage is the cache of covered's answers.
var coverage = struct {
	sync.Mutex
	family map[rune]string // the installed family that has a rune, "" for none
	// lacks is whether a typeface draws a rune as .notdef, by shaper: the answer depends on its
	// fonts. A shaper is held weakly, and its answers are dropped once it is collected.
	lacks map[weak.Pointer[text.Shaper]]map[typefaceRune]bool
}{family: map[rune]string{}, lacks: map[weak.Pointer[text.Shaper]]map[typefaceRune]bool{}}

type typefaceRune struct {
	tf font.Typeface
	r  rune
}

// shaperLacks is s's answers, made the first time it is asked of; the lock is held.
func shaperLacks(s *text.Shaper) map[typefaceRune]bool {
	w := weak.Make(s)
	m, ok := coverage.lacks[w]
	if !ok {
		m = map[typefaceRune]bool{}
		coverage.lacks[w] = m
		runtime.AddCleanup(s, forgetShaper, w)
	}
	return m
}

// forgetShaper drops a collected shaper's answers.
func forgetShaper(w weak.Pointer[text.Shaper]) {
	coverage.Lock()
	defer coverage.Unlock()
	delete(coverage.lacks, w)
}

// covered is tf with, after it, an installed family for each rune of str that tf draws as .notdef.
// tf is returned as it is when it draws every rune, or no installed font has the ones it lacks.
func covered(s *text.Shaper, tf font.Typeface, str string) font.Typeface {
	var add []string
	for _, r := range str {
		if !mayLack(r) {
			continue
		}
		if f := familyFor(s, tf, r); f != "" && !namesFamily(string(tf), f) && !namesFamily(strings.Join(add, ","), f) {
			add = append(add, f)
		}
	}
	if len(add) == 0 {
		return tf
	}
	return font.Typeface(string(tf) + ", " + strings.Join(add, ", "))
}

// mayLack reports whether r is a rune a font may lack and that draws: ASCII is in every font, and
// spaces, controls, joiners and marks draw nothing of their own.
func mayLack(r rune) bool {
	return r >= 0x80 && !unicode.IsSpace(r) && !unicode.In(r, unicode.Cc, unicode.Cf, unicode.Mn, unicode.Me, unicode.Variation_Selector)
}

// familyFor is the installed family that draws r when tf draws it as .notdef, "" otherwise.
func familyFor(s *text.Shaper, tf font.Typeface, r rune) string {
	k := typefaceRune{tf, r}
	coverage.Lock()
	lacks, known := shaperLacks(s)[k]
	coverage.Unlock()
	if !known {
		lacks = notdef(s, tf, r)
		coverage.Lock()
		shaperLacks(s)[k] = lacks
		coverage.Unlock()
	}
	if !lacks {
		return ""
	}
	return installedFamily(r)
}

// notdef reports whether s shapes r, in tf, to .notdef: glyph 0 of its face. Gio packs a glyph id
// as face<<48 | ppem<<32 | glyph (gioui.org v0.10.3).
func notdef(s *text.Shaper, tf font.Typeface, r rune) bool {
	s.LayoutString(text.Parameters{Font: font.Font{Typeface: tf}, PxPerEm: fixed.I(16), MaxWidth: 1 << 20}, string(r))
	missing := false
	for g, ok := s.NextGlyph(); ok; g, ok = s.NextGlyph() {
		if uint32(g.ID) == 0 {
			missing = true
		}
	}
	return missing
}

// installedFamily is the first installed family, in the index's order, that has r; "" for none.
// macOS's hidden fonts (".LastResort", ".SF NS") are passed over: LastResort has every rune, as a
// box naming its block, which is the box this fallback replaces.
func installedFamily(r rune) string {
	coverage.Lock()
	f, known := coverage.family[r]
	coverage.Unlock()
	if known {
		return f
	}
	for _, fp := range installedFonts() {
		if fp.Runes.Contains(r) && fp.Family != "" && !strings.HasPrefix(fp.Family, ".") {
			f = fp.Family
			break
		}
	}
	coverage.Lock()
	coverage.family[r] = f
	coverage.Unlock()
	return f
}
