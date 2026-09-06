// Command gen regenerates the grapheme package's tables.go from Unicode
// Character Database (UCD) files.
//
// It reads the UCD source files from a pinned local mirror (gen/testdata by
// default, committed to the repository so regeneration needs no network) and
// emits sorted, non-overlapping rune-range tables with binary-search lookups —
// the runewidth/uniseg recipe, hand-rolled to keep golib/tui zero-dep
// (golib/tui).
//
// Usage (from the grapheme package directory):
//
//	go run ./gen -unicode 15.0.0             # regenerate from the local mirror
//	go run ./gen -unicode 15.0.0 -download   # refresh the mirror first
//
// The generated header records the Unicode version and the SHA-256 hash of
// every input file, so a given tables.go is reproducible from, and auditable
// against, exactly one set of inputs.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// inputs lists every UCD file the generator consumes. GraphemeBreakTest.txt
// contributes no table data — it is mirrored for the conformance test — but
// is downloaded and hash-recorded alongside the rest.
var inputs = []struct {
	name    string // file name inside -dir
	urlPath string // path under https://www.unicode.org/Public/<version>/
}{
	{"EastAsianWidth.txt", "ucd/EastAsianWidth.txt"},
	{"GraphemeBreakProperty.txt", "ucd/auxiliary/GraphemeBreakProperty.txt"},
	{"emoji-data.txt", "ucd/emoji/emoji-data.txt"},
	{"DerivedCoreProperties.txt", "ucd/DerivedCoreProperties.txt"},
	{"DerivedGeneralCategory.txt", "ucd/extracted/DerivedGeneralCategory.txt"},
	{"GraphemeBreakTest.txt", "ucd/auxiliary/GraphemeBreakTest.txt"},
}

// gbPropConst maps UCD Grapheme_Cluster_Break property values (plus
// Extended_Pictographic from emoji-data.txt) to the gbProp constant names
// declared in segment.go.
var gbPropConst = map[string]string{
	"CR":                    "prCR",
	"LF":                    "prLF",
	"Control":               "prControl",
	"Extend":                "prExtend",
	"ZWJ":                   "prZWJ",
	"Regional_Indicator":    "prRegionalIndicator",
	"Prepend":               "prPrepend",
	"SpacingMark":           "prSpacingMark",
	"L":                     "prL",
	"V":                     "prV",
	"T":                     "prT",
	"LV":                    "prLV",
	"LVT":                   "prLVT",
	"Extended_Pictographic": "prExtendedPictographic",
}

type span struct{ lo, hi rune }

type propSpan struct {
	lo, hi rune
	prop   string // gbProp constant name
}

func main() {
	version := flag.String("unicode", "15.0.0", "Unicode version to generate from (must match the mirrored files)")
	dir := flag.String("dir", filepath.Join("gen", "testdata"), "directory holding the mirrored UCD files")
	download := flag.Bool("download", false, "download the UCD files into -dir before generating")
	out := flag.String("o", "tables.go", "output file")
	flag.Parse()

	if *download {
		for _, in := range inputs {
			url := "https://www.unicode.org/Public/" + *version + "/" + in.urlPath
			if err := fetch(url, filepath.Join(*dir, in.name)); err != nil {
				fatalf("download %s: %v", url, err)
			}
			fmt.Fprintf(os.Stderr, "downloaded %s\n", in.name)
		}
	}

	hashes := make(map[string]string, len(inputs))
	for _, in := range inputs {
		h, err := fileSHA256(filepath.Join(*dir, in.name))
		if err != nil {
			fatalf("hash %s: %v (run with -download to populate the mirror)", in.name, err)
		}
		hashes[in.name] = h
	}

	// --- Grapheme_Cluster_Break properties + Extended_Pictographic --------
	gcb, err := parseUCD(filepath.Join(*dir, "GraphemeBreakProperty.txt"), nil)
	if err != nil {
		fatalf("parse GraphemeBreakProperty.txt: %v", err)
	}
	emoji, err := parseUCD(filepath.Join(*dir, "emoji-data.txt"),
		map[string]bool{"Extended_Pictographic": true, "Emoji_Presentation": true})
	if err != nil {
		fatalf("parse emoji-data.txt: %v", err)
	}

	var gb []propSpan
	for prop, spans := range gcb {
		name, ok := gbPropConst[prop]
		if !ok {
			fatalf("GraphemeBreakProperty.txt: unknown property %q (new Unicode version? extend gbPropConst and segment.go)", prop)
		}
		for _, s := range spans {
			gb = append(gb, propSpan{s.lo, s.hi, name})
		}
	}
	// Extended_Pictographic is folded into the same table as a
	// pseudo-property. UAX #29 guarantees no code point carries both a GCB
	// property and Extended_Pictographic; verified below after sorting.
	for _, s := range emoji["Extended_Pictographic"] {
		gb = append(gb, propSpan{s.lo, s.hi, "prExtendedPictographic"})
	}
	sort.Slice(gb, func(i, j int) bool { return gb[i].lo < gb[j].lo })
	for i := 1; i < len(gb); i++ {
		if gb[i].lo <= gb[i-1].hi {
			fatalf("overlapping ranges %04X..%04X (%s) and %04X..%04X (%s): Extended_Pictographic is no longer disjoint from Grapheme_Cluster_Break — the folded table encoding is invalid for this Unicode version",
				gb[i-1].lo, gb[i-1].hi, gb[i-1].prop, gb[i].lo, gb[i].hi, gb[i].prop)
		}
	}
	gb = mergePropSpans(gb)

	// --- East Asian Width --------------------------------------------------
	eaw, err := parseUCD(filepath.Join(*dir, "EastAsianWidth.txt"),
		map[string]bool{"W": true, "F": true, "A": true})
	if err != nil {
		fatalf("parse EastAsianWidth.txt: %v", err)
	}
	wide := append(append([]span(nil), eaw["W"]...), eaw["F"]...)
	// EastAsianWidth.txt lists assigned code points only; its header
	// specifies that all undesignated code points in Planes 2 and 3 default
	// to W. Fold those defaults in so future-assigned CJK ideographs
	// measure correctly.
	wide = append(wide, span{0x20000, 0x2FFFD}, span{0x30000, 0x3FFFD})
	wide = sortMerge(wide)
	ambiguous := sortMerge(eaw["A"])

	emojiPres := sortMerge(emoji["Emoji_Presentation"])

	// --- Zero-width: Default_Ignorable ∪ Mn ∪ Me ---------------------------
	dcp, err := parseUCD(filepath.Join(*dir, "DerivedCoreProperties.txt"),
		map[string]bool{"Default_Ignorable_Code_Point": true})
	if err != nil {
		fatalf("parse DerivedCoreProperties.txt: %v", err)
	}
	gc, err := parseUCD(filepath.Join(*dir, "DerivedGeneralCategory.txt"),
		map[string]bool{"Mn": true, "Me": true})
	if err != nil {
		fatalf("parse DerivedGeneralCategory.txt: %v", err)
	}
	var zero []span
	zero = append(zero, dcp["Default_Ignorable_Code_Point"]...)
	zero = append(zero, gc["Mn"]...)
	zero = append(zero, gc["Me"]...)
	zero = sortMerge(zero)

	// --- Emit ---------------------------------------------------------------
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "// Code generated by gen/gen.go from the Unicode %s UCD. DO NOT EDIT.\n", *version)
	fmt.Fprintf(&buf, "//\n// Inputs (SHA-256):\n")
	for _, in := range inputs {
		fmt.Fprintf(&buf, "//\t%s  %s\n", hashes[in.name], in.name)
	}
	fmt.Fprintf(&buf, "//\n// Refresh: see the package documentation in doc.go.\n\n")
	fmt.Fprintf(&buf, "package grapheme\n\n")

	fmt.Fprintf(&buf, "// unicodeVersion is the UCD version these tables were generated from.\nconst unicodeVersion = %q\n\n", *version)

	fmt.Fprintf(&buf, "// gbRanges maps runes to Grapheme_Cluster_Break properties (UAX #29),\n")
	fmt.Fprintf(&buf, "// with Extended_Pictographic (emoji-data.txt) folded in as a\n")
	fmt.Fprintf(&buf, "// pseudo-property; the generator verifies the two sets are disjoint.\n")
	fmt.Fprintf(&buf, "// %d ranges, sorted by lo for binary search.\n", len(gb))
	fmt.Fprintf(&buf, "var gbRanges = []gbRange{\n")
	for _, s := range gb {
		fmt.Fprintf(&buf, "\t{0x%04X, 0x%04X, %s},\n", s.lo, s.hi, s.prop)
	}
	fmt.Fprintf(&buf, "}\n\n")

	emitSpans(&buf, "wideRanges", wide,
		"wideRanges holds East Asian Wide (W) and Fullwidth (F) code points\n// (UAX #11), including the Plane 2/3 undesignated-defaults-to-W ranges.")
	emitSpans(&buf, "ambiguousRanges", ambiguous,
		"ambiguousRanges holds East Asian Ambiguous (A) code points (UAX #11).")
	emitSpans(&buf, "emojiPresentationRanges", emojiPres,
		"emojiPresentationRanges holds Emoji_Presentation code points\n// (emoji-data.txt): emoji-by-default, rendered two columns wide.")
	emitSpans(&buf, "zeroWidthRanges", zero,
		"zeroWidthRanges holds zero-display-width code points:\n// Default_Ignorable_Code_Point (DerivedCoreProperties.txt, which includes\n// ZWJ, ZWNJ, and the variation selectors) union the Mn and Me general\n// categories (DerivedGeneralCategory.txt).")

	src, err := format.Source(buf.Bytes())
	if err != nil {
		fatalf("gofmt generated source: %v", err)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		fatalf("write %s: %v", *out, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s: %d gb ranges, %d wide, %d ambiguous, %d emoji-presentation, %d zero-width\n",
		*out, len(gb), len(wide), len(ambiguous), len(emojiPres), len(zero))
}

func emitSpans(buf *bytes.Buffer, name string, spans []span, doc string) {
	fmt.Fprintf(buf, "// %s\n// %d ranges, sorted by lo for binary search.\n", doc, len(spans))
	fmt.Fprintf(buf, "var %s = []runeRange{\n", name)
	for _, s := range spans {
		fmt.Fprintf(buf, "\t{0x%04X, 0x%04X},\n", s.lo, s.hi)
	}
	fmt.Fprintf(buf, "}\n\n")
}

// parseUCD parses the standard UCD semicolon-delimited format:
//
//	XXXX[..YYYY] ; Value [; ...] # comment
//
// returning value → spans. If want is non-nil, only listed values are kept.
func parseUCD(path string, want map[string]bool) (map[string][]span, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := make(map[string][]span)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ";")
		if len(fields) < 2 {
			return nil, fmt.Errorf("%s:%d: malformed line %q", path, lineNo, line)
		}
		value := strings.TrimSpace(fields[1])
		if want != nil && !want[value] {
			continue
		}
		rng := strings.TrimSpace(fields[0])
		var lo, hi rune
		if a, b, isRange := strings.Cut(rng, ".."); isRange {
			lo, err = parseHexRune(a)
			if err == nil {
				hi, err = parseHexRune(b)
			}
		} else {
			lo, err = parseHexRune(rng)
			hi = lo
		}
		if err != nil {
			return nil, fmt.Errorf("%s:%d: bad code point range %q: %v", path, lineNo, rng, err)
		}
		out[value] = append(out[value], span{lo, hi})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseHexRune(s string) (rune, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 16, 32)
	return rune(n), err
}

// sortMerge sorts spans by lo and merges overlapping or adjacent spans.
func sortMerge(spans []span) []span {
	if len(spans) == 0 {
		return nil
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].lo < spans[j].lo })
	out := spans[:1]
	for _, s := range spans[1:] {
		last := &out[len(out)-1]
		if s.lo <= last.hi+1 {
			if s.hi > last.hi {
				last.hi = s.hi
			}
			continue
		}
		out = append(out, s)
	}
	return out
}

// mergePropSpans merges adjacent spans carrying the same property.
// Input must already be sorted and non-overlapping.
func mergePropSpans(spans []propSpan) []propSpan {
	if len(spans) == 0 {
		return nil
	}
	out := spans[:1]
	for _, s := range spans[1:] {
		last := &out[len(out)-1]
		if s.prop == last.prop && s.lo == last.hi+1 {
			last.hi = s.hi
			continue
		}
		out = append(out, s)
	}
	return out
}

func fetch(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen: "+format+"\n", args...)
	os.Exit(1)
}
