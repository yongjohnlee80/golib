package vtparse

import (
	"math/rand"
	"testing"
)

// parseOutput is parse for an output-stream parser.
func parseOutput(input string) []string {
	p := Parser{Output: true}
	var out []string
	for i := 0; i < len(input); i++ {
		p.Feed(input[i], func(a *Action) { out = append(out, renderAction(a)) })
	}
	return out
}

// outputCorpus covers every place the output mode departs from the input
// mode, plus ordinary sequences to show they are unchanged.
var outputCorpus = []struct {
	name  string
	input string
	want  []string
}{
	{"plain ascii", "hi", []string{`print:"h"`, `print:"i"`}},
	{"del ignored", "a\x7fb", []string{`print:"a"`, `print:"b"`}},
	{"esc esc restarts", "\x1b\x1b[A", []string{`csi:priv="" params=[] inter="" final="A"`}},
	{"esc utf8 dropped", "\x1bé", nil},
	{"esc utf8 then text", "\x1béx", []string{`print:"x"`}},
	{"c1 csi", "\u009b1;2H", []string{`csi:priv="" params=[1 2] inter="" final="H"`}},
	{"c1 csi private", "\u009b?1049h", []string{`csi:priv="?" params=[1049] inter="" final="h"`}},
	{"c1 osc bel", "\u009d0;title\x07", []string{`osc:"0;title"`}},
	{"c1 dcs st", "\u0090+q544e\x1b\\",
		[]string{`dcs:priv="" params=[] inter="+" final="q" data="544e"`, `esc:inter="" final="\\"`}},
	{"c1 ind", "\u0084", []string{`esc:inter="" final="D"`}},
	{"c1 ri", "\u008d", []string{`esc:inter="" final="M"`}},
	{"c1 st alone", "\u009c", []string{`esc:inter="" final="\\"`}},
	{"nbsp prints", "\u00a0", []string{`print:"\u00a0"`}},
	{"can aborts csi", "\x1b[1\x18A", []string{`exec:18`, `print:"A"`}},
	{"sgr truecolor colon", "\x1b[38:2::1:2:3m",
		[]string{`csi:priv="" params=[38:2:-1:1:2:3] inter="" final="m"`}},
}

func TestOutputCorpus(t *testing.T) {
	for _, tc := range outputCorpus {
		t.Run(tc.name, func(t *testing.T) {
			got := parseOutput(tc.input)
			if !equalStrings(got, tc.want) {
				t.Errorf("parseOutput(%q)\n got: %v\nwant: %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestOutputSplitBoundaries(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, tc := range outputCorpus {
		t.Run(tc.name, func(t *testing.T) {
			whole := parseOutput(tc.input)
			for trial := 0; trial < 20; trial++ {
				p := Parser{Output: true}
				var got []string
				emit := func(a *Action) { got = append(got, renderAction(a)) }
				rest := tc.input
				for len(rest) > 0 {
					n := 1 + rng.Intn(len(rest))
					for i := 0; i < n; i++ {
						p.Feed(rest[i], emit)
					}
					rest = rest[n:]
				}
				if !equalStrings(got, whole) {
					t.Fatalf("split trial %d diverged\n got: %v\nwant: %v", trial, got, whole)
				}
			}
		})
	}
}

func TestInputModeKeepsItsDeviations(t *testing.T) {
	// The same bytes through an input parser: the output rules must not
	// leak into the key decoder's stream.
	for _, tc := range []struct {
		input string
		want  []string
	}{
		{"\x7f", []string{`exec:7F`}},
		{"\x1b\x1b", []string{`exec:1B`}},
		{"\x1bé", []string{`print+alt:"é"`}},
		{"\u009b", []string{`print:"\u009b"`}},
	} {
		if got := parse(tc.input); !equalStrings(got, tc.want) {
			t.Errorf("parse(%q)\n got: %v\nwant: %v", tc.input, got, tc.want)
		}
	}
}

func TestResetKeepsOutput(t *testing.T) {
	p := Parser{Output: true}
	p.Feed(0x1b, func(*Action) {})
	if !p.InEscape() {
		t.Fatal("InEscape after ESC = false")
	}
	p.Reset()
	if p.InEscape() {
		t.Fatal("InEscape after Reset = true")
	}
	if !p.Output {
		t.Fatal("Reset cleared Output")
	}
	var got []string
	p.Feed(0x7f, func(a *Action) { got = append(got, renderAction(a)) })
	if len(got) != 0 {
		t.Fatalf("DEL after Reset emitted %v", got)
	}
}

func TestOutputC1STEndsStrings(t *testing.T) {
	for _, c := range []struct {
		name, input string
		want        []string
	}{
		{"osc", "\x1b]0;title\u009cafter", []string{`osc:"0;title"`, `print:"a"`, `print:"f"`, `print:"t"`, `print:"e"`, `print:"r"`}},
		{"dcs", "\x1bP+q54\u009cx", []string{`dcs:priv="" params=[] inter="+" final="q" data="54"`, `print:"x"`}},
		{"apc", "\x1b_Gi=1\u009cx", []string{`apc:"Gi=1"`, `print:"x"`}},
		{"pm ignored", "\x1b^secret\u009cx", []string{`print:"x"`}},
		{"other C2 runes stay data", "\x1b]0;caf\u00e9\u00a0\x07", []string{"osc:\"0;caf\u00e9\\u00a0\""}},
		{"C2 before ESC stays data", "\x1b]0;a\xc2\x1b\\", []string{`osc:"0;a\xc2"`, `esc:inter="" final="\\"`}},
		{"C2 then CAN drops", "\x1b]0;a\xc2\x18x", []string{`exec:18`, `print:"x"`}},
	} {
		if got := parseOutput(c.input); !equalStrings(got, c.want) {
			t.Errorf("%s: %q\n got: %v\nwant: %v", c.name, c.input, got, c.want)
		}
	}
	// Input keeps U+009C as string data: the key decoder's strings are replies.
	if got := parse("\x1b]0;a\u009cb\x07"); !equalStrings(got, []string{`osc:"0;a\u009cb"`}) {
		t.Errorf("input: %v", got)
	}
}

func TestOutputHoldsEightSubparams(t *testing.T) {
	got := parseOutput("\x1b[1:2:3:4:5:6:7:8:9:10m")
	want := []string{`csi:priv="" params=[1:2:3:4:5:6:7:8] inter="" final="m"`}
	if !equalStrings(got, want) {
		t.Errorf("output overflow: got %v want %v", got, want)
	}
}
