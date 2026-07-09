package grapheme

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

func collect(s string) []string {
	return slices.Collect(Clusters(s))
}

func TestClusters(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"ascii", "hello", []string{"h", "e", "l", "l", "o"}},
		{"ascii with space", "a b", []string{"a", " ", "b"}},
		{"crlf joins", "a\r\nb", []string{"a", "\r\n", "b"}},
		{"lfcr breaks", "\n\r", []string{"\n", "\r"}},
		{"controls break", "a\tb", []string{"a", "\t", "b"}},
		// e + COMBINING ACUTE ACCENT, then x (GB9).
		{"combining acute", "éx", []string{"é", "x"}},
		// a + COMBINING ACUTE + COMBINING DIAERESIS: one cluster.
		{"stacked combining", "á̈", []string{"á̈"}},
		// Precomposed é + COMBINING DIAERESIS: still one cluster.
		{"precomposed plus combining", "é̈", []string{"é̈"}},
		{"cjk", "世界", []string{"世", "界"}}, // 世界
		// Conjoining jamo L+V+T (각): one syllable cluster (GB6/GB7/GB8).
		{"hangul jamo LVT", "각", []string{"각"}},
		// Precomposed LV syllable 가 + trailing jamo T (GB7... GB8 path).
		{"hangul LV plus T", "각", []string{"각"}},
		// Two precomposed LVT syllables: two clusters.
		{"hangul syllables split", "각각", []string{"각", "각"}},
		// 🧑 ZWJ 🌾 (farmer): GB11 joins.
		{"zwj emoji farmer", "\U0001f9d1‍\U0001f33e", []string{"\U0001f9d1‍\U0001f33e"}},
		// 👩 ZWJ 👩 ZWJ 👦 (family): chained GB11.
		{"zwj family chain", "\U0001f469‍\U0001f469‍\U0001f466",
			[]string{"\U0001f469‍\U0001f469‍\U0001f466"}},
		// ZWJ attaches to 'a' (GB9) but GB11 needs a pictographic base, so 🌾 breaks off.
		{"zwj without pictographic base breaks", "a‍\U0001f33e", []string{"a‍", "\U0001f33e"}},
		// Regional Indicator pair 🇰🇷 (GB12).
		{"flag pair", "\U0001f1f0\U0001f1f7", []string{"\U0001f1f0\U0001f1f7"}},
		// Four RIs pair off from the left: 🇰🇷 🇦🇺 (GB12/GB13).
		{"two flags split pairwise", "\U0001f1f0\U0001f1f7\U0001f1e6\U0001f1fa",
			[]string{"\U0001f1f0\U0001f1f7", "\U0001f1e6\U0001f1fa"}},
		// ❤ + VS16: the variation selector is Extend (GB9).
		{"vs16 heart", "❤️!", []string{"❤️", "!"}},
		// 1 + VS16 + COMBINING ENCLOSING KEYCAP: one cluster.
		{"keycap", "1️⃣", []string{"1️⃣"}},
		// ARABIC NUMBER SIGN (Prepend) + ARABIC-INDIC DIGIT ONE (GB9b).
		{"prepend", "؀١", []string{"؀١"}},
		// Tamil NA + vowel sign I (SpacingMark, GB9a).
		{"spacing mark", "நி", []string{"நி"}},
		{"invalid utf8 single bytes", "a\xffb", []string{"a", "\xff", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := collect(tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("Clusters(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestClustersRoundTrip checks the substring contract: concatenating the
// yielded clusters reproduces the input exactly.
func TestClustersRoundTrip(t *testing.T) {
	inputs := []string{
		"", "hello", "é", "\r\n\r\n", "世界",
		"\U0001f9d1‍\U0001f33e flags \U0001f1f0\U0001f1f7\U0001f1e6\U0001f1fa",
		"bad\xff\xfebytes",
	}
	for _, in := range inputs {
		if got := strings.Join(collect(in), ""); got != in {
			t.Errorf("clusters of %q rejoin to %q", in, got)
		}
	}
}

func TestClustersEarlyStop(t *testing.T) {
	n := 0
	for range Clusters("abcdef") {
		n++
		if n == 2 {
			break
		}
	}
	if n != 2 {
		t.Fatalf("iterated %d clusters after break, want 2", n)
	}
}

// TestGraphemeBreakConformance runs every case of the pinned Unicode
// version's GraphemeBreakTest.txt (UAX #29 conformance data, mirrored by
// the generator under gen/testdata).
func TestGraphemeBreakConformance(t *testing.T) {
	t.Logf("tables: Unicode %s; stdlib unicode.Version: %s", unicodeVersion, unicode.Version)
	data, err := os.ReadFile(filepath.Join("gen", "testdata", "GraphemeBreakTest.txt"))
	if err != nil {
		t.Fatalf("read conformance data: %v (regenerate the mirror: go run ./gen -download)", err)
	}
	const (
		tokBreak   = "÷" // ÷
		tokNoBreak = "×" // ×
	)
	cases := 0
	for lineNo, line := range strings.Split(string(data), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var (
			input strings.Builder
			want  []string
			cur   strings.Builder
		)
		for _, tok := range strings.Fields(line) {
			switch tok {
			case tokBreak:
				if cur.Len() > 0 {
					want = append(want, cur.String())
					cur.Reset()
				}
			case tokNoBreak:
				// no boundary; keep accumulating the current cluster
			default:
				n, err := strconv.ParseUint(tok, 16, 32)
				if err != nil {
					t.Fatalf("line %d: bad token %q: %v", lineNo+1, tok, err)
				}
				cur.WriteRune(rune(n))
				input.WriteRune(rune(n))
			}
		}
		if cur.Len() > 0 {
			t.Fatalf("line %d: test case does not end with a break token", lineNo+1)
		}
		if got := collect(input.String()); !slices.Equal(got, want) {
			t.Errorf("line %d: Clusters(%q) = %q, want %q", lineNo+1, input.String(), got, want)
		}
		cases++
	}
	if cases < 500 {
		t.Fatalf("only %d conformance cases parsed — data file truncated?", cases)
	}
	t.Logf("%d conformance cases", cases)
}

var benchCorpora = []struct {
	name, text string
}{
	{"ascii", strings.Repeat("The quick brown fox jumps over the lazy dog. ", 20)},
	{"cjk", strings.Repeat("終端の格子は書記素の幅で揃う。", 30)},
	{"emoji", strings.Repeat("\U0001f9d1‍\U0001f33e\U0001f1f0\U0001f1f7❤️é ", 40)},
	{"mixed", strings.Repeat("cell 世界 \U0001f9d1‍\U0001f33e ok é! ", 30)},
}

func BenchmarkClusters(b *testing.B) {
	for _, c := range benchCorpora {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(c.text)))
			for b.Loop() {
				for range Clusters(c.text) {
				}
			}
		})
	}
}
