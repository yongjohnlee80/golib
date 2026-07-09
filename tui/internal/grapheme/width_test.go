package grapheme

import "testing"

func TestClusterWidth(t *testing.T) {
	tests := []struct {
		name    string
		cluster string
		want    int
	}{
		{"empty", "", 0},
		{"ascii letter", "a", 1},
		{"space", " ", 1},
		{"control", "\t", 0},
		{"crlf", "\r\n", 0},
		{"cjk wide", "世", 2},
		{"fullwidth latin", "Ａ", 2},
		{"halfwidth katakana", "ｱ", 1},
		{"hangul syllable", "각", 2},
		{"combining acute", "é", 1},     // e + U+0301
		{"stacked combining", "á̈", 1},  // a + U+0301 + U+0308
		{"lone combining mark", "́", 0}, // degenerate cluster
		{"lone zwj", "‍", 0},
		{"lone zwnj", "‌", 0},
		{"zwsp", "​", 0}, // default-ignorable
		{"zwj farmer", "\U0001F9D1‍\U0001F33E", 2},
		{"zwj family", "\U0001F469‍\U0001F469‍\U0001F466", 2},
		{"flag KR", "\U0001F1F0\U0001F1F7", 2},
		{"flag AU", "\U0001F1E6\U0001F1FA", 2},
		{"emoji presentation default", "\U0001F600", 2}, // 😀
		{"vs16 heart", "❤️", 2},                         // text-default heart forced emoji
		{"vs16 keycap", "1️⃣", 2},
		{"vs15 umbrella", "☂︎", 1}, // emoji-capable forced text
		{"vs15 watch", "⌚︎", 1},    // Emoji_Presentation forced narrow
		{"watch no vs", "⌚", 2},    // Emoji_Presentation
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClusterWidth(tt.cluster, false); got != tt.want {
				t.Errorf("ClusterWidth(%q, false) = %d, want %d", tt.cluster, got, tt.want)
			}
		})
	}
}

func TestClusterWidthAmbiguous(t *testing.T) {
	tests := []struct {
		cluster              string
		narrow, ambiguousTwo int
	}{
		{"±", 1, 2},  // U+00B1 PLUS-MINUS SIGN, EAW A
		{"§", 1, 2},  // U+00A7 SECTION SIGN, EAW A
		{"○", 1, 2},  // U+25CB WHITE CIRCLE, EAW A
		{"Ω", 1, 2},  // U+03A9 GREEK OMEGA, EAW A
		{"a", 1, 1},  // narrow either way
		{"世", 2, 2},  // wide either way
		{"é", 1, 1}, // combining mark is EAW A but zero-width wins
		{"é", 1, 2},  // precomposed U+00E9 is itself EAW A
	}
	for _, tt := range tests {
		if got := ClusterWidth(tt.cluster, false); got != tt.narrow {
			t.Errorf("ClusterWidth(%q, false) = %d, want %d", tt.cluster, got, tt.narrow)
		}
		if got := ClusterWidth(tt.cluster, true); got != tt.ambiguousTwo {
			t.Errorf("ClusterWidth(%q, true) = %d, want %d", tt.cluster, got, tt.ambiguousTwo)
		}
	}
}

func TestStringWidth(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"ascii", "hello", 5},
		{"ascii sentence", "The quick brown fox", 19},
		{"cjk", "世界", 4},
		{"mixed cjk ascii", "Go言語", 6},
		{"combining", "café", 4}, // café with combining acute
		{"zwj emoji in text", "a\U0001F9D1‍\U0001F33Eb", 4},
		{"flags", "\U0001F1F0\U0001F1F7\U0001F1E6\U0001F1FA", 4}, // two flags
		{"vs16 in text", "x❤️y", 4},
		{"vs15 in text", "x☂︎y", 3},
		{"tabs and newlines zero", "a\tb\nc", 3},
		{"ascii then combining", "ée", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StringWidth(tt.in, false); got != tt.want {
				t.Errorf("StringWidth(%q, false) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestStringWidthAmbiguous(t *testing.T) {
	const s = "a±b○" // 2 narrow + 2 ambiguous
	if got := StringWidth(s, false); got != 4 {
		t.Errorf("StringWidth(%q, false) = %d, want 4", s, got)
	}
	if got := StringWidth(s, true); got != 6 {
		t.Errorf("StringWidth(%q, true) = %d, want 6", s, got)
	}
}

// TestStringWidthEqualsClusterSum pins the documented contract:
// StringWidth is exactly the sum of ClusterWidth over Clusters.
func TestStringWidthEqualsClusterSum(t *testing.T) {
	for _, c := range benchCorpora {
		for _, wide := range []bool{false, true} {
			sum := 0
			for cl := range Clusters(c.text) {
				sum += ClusterWidth(cl, wide)
			}
			if got := StringWidth(c.text, wide); got != sum {
				t.Errorf("%s (ambiguousWide=%v): StringWidth = %d, cluster sum = %d", c.name, wide, got, sum)
			}
		}
	}
}

func TestStringWidthAllocs(t *testing.T) {
	for _, c := range benchCorpora {
		c := c
		if n := testing.AllocsPerRun(100, func() { StringWidth(c.text, false) }); n != 0 {
			t.Errorf("StringWidth(%s) allocates %.1f times per run, want 0", c.name, n)
		}
	}
}

func BenchmarkStringWidth(b *testing.B) {
	for _, c := range benchCorpora {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(c.text)))
			for b.Loop() {
				StringWidth(c.text, false)
			}
		})
	}
}
