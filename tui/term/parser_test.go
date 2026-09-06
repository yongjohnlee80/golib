package term

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// renderAction snapshots an action as a readable string. Copies everything —
// action.data aliases parser storage and is only valid during emit.
func renderAction(a *action) string {
	switch a.kind {
	case actPrint:
		if a.alt {
			return fmt.Sprintf("print+alt:%q", string(a.r))
		}
		return fmt.Sprintf("print:%q", string(a.r))
	case actExecute:
		return fmt.Sprintf("exec:%02X", a.b)
	case actEsc:
		return fmt.Sprintf("esc:inter=%q final=%q", a.inter, string(rune(a.final)))
	case actCSI:
		return fmt.Sprintf("csi:priv=%q params=%s inter=%q final=%q",
			privString(a.priv), renderParams(a.params), a.inter, string(rune(a.final)))
	case actOSC:
		return fmt.Sprintf("osc:%q", string(a.data))
	case actDCS:
		return fmt.Sprintf("dcs:priv=%q params=%s inter=%q final=%q data=%q",
			privString(a.priv), renderParams(a.params), a.inter, string(rune(a.final)), string(a.data))
	}
	return "?"
}

func privString(p byte) string {
	if p == 0 {
		return ""
	}
	return string(rune(p))
}

func renderParams(ps []csiParam) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, p := range ps {
		if i > 0 {
			sb.WriteByte(' ')
		}
		for j, v := range p.parts {
			if j > 0 {
				sb.WriteByte(':')
			}
			fmt.Fprintf(&sb, "%d", v)
		}
	}
	sb.WriteByte(']')
	return sb.String()
}

// parse feeds input as one contiguous chunk and returns rendered actions.
func parse(input string) []string {
	var p parser
	var out []string
	for i := 0; i < len(input); i++ {
		p.feed(input[i], func(a *action) { out = append(out, renderAction(a)) })
	}
	return out
}

// parserCorpus is the shared table for both the direct decode assertions and
// the split-boundary property test: every ctlseqs shape the parser claims,
// kitty sub-parameters, OSC/DCS with ST and BEL terminators, CAN/SUB
// aborts, and ESC-from-anywhere restarts.
var parserCorpus = []struct {
	name  string
	input string
	want  []string
}{
	{"plain ascii", "hi", []string{`print:"h"`, `print:"i"`}},
	{"c0 execute", "\x0d\x09", []string{`exec:0D`, `exec:09`}},
	{"del executes", "\x7f", []string{`exec:7F`}},
	{"utf8 2-byte", "é", []string{`print:"é"`}},
	{"utf8 4-byte", "🎉", []string{`print:"🎉"`}},
	{"esc dispatch alt-a", "\x1ba", []string{`esc:inter="" final="a"`}},
	{"esc intermediate", "\x1b(B", []string{`esc:inter="(" final="B"`}},
	{"esc utf8 is alt print", "\x1bé", []string{`print+alt:"é"`}},
	{"csi no params", "\x1b[A", []string{`csi:priv="" params=[] inter="" final="A"`}},
	{"csi params", "\x1b[1;5A", []string{`csi:priv="" params=[1 5] inter="" final="A"`}},
	{"csi empty params default", "\x1b[;5H", []string{`csi:priv="" params=[-1 5] inter="" final="H"`}},
	{"csi private marker", "\x1b[?2004h", []string{`csi:priv="?" params=[2004] inter="" final="h"`}},
	{"csi gt marker", "\x1b[>3u", []string{`csi:priv=">" params=[3] inter="" final="u"`}},
	{"csi subparams kitty", "\x1b[97:65:97;2u",
		[]string{`csi:priv="" params=[97:65:97 2] inter="" final="u"`}},
	{"csi subparam event", "\x1b[1;1:3A", []string{`csi:priv="" params=[1 1:3] inter="" final="A"`}},
	{"csi intermediate decrpm", "\x1b[?2026;1$y",
		[]string{`csi:priv="?" params=[2026 1] inter="$" final="y"`}},
	{"csi sgr mouse", "\x1b[<35;10;5M",
		[]string{`csi:priv="<" params=[35 10 5] inter="" final="M"`}},
	{"csi param saturates", "\x1b[999999d",
		[]string{`csi:priv="" params=[65535] inter="" final="d"`}},
	{"osc bel", "\x1b]11;rgb:1e1e/1e1e/1e1e\x07", []string{`osc:"11;rgb:1e1e/1e1e/1e1e"`}},
	{"osc st", "\x1b]10;?\x1b\\", []string{`osc:"10;?"`, `esc:inter="" final="\\"`}},
	{"dcs st", "\x1bP1+r524742=1\x1b\\",
		[]string{`dcs:priv="" params=[1] inter="+" final="r" data="524742=1"`, `esc:inter="" final="\\"`}},
	{"dcs no params", "\x1bP+q524742\x1b\\",
		[]string{`dcs:priv="" params=[] inter="+" final="q" data="524742"`, `esc:inter="" final="\\"`}},
	{"can aborts csi and is ctrl-x", "\x1b[12\x18A",
		[]string{`exec:18`, `print:"A"`}},
	{"sub aborts osc", "\x1b]11;partial\x1aZ",
		[]string{`exec:1A`, `print:"Z"`}},
	{"esc restarts csi", "\x1b[12\x1b[3~",
		[]string{`csi:priv="" params=[3] inter="" final="~"`}},
	{"esc restarts osc without dispatch loss", "\x1b]11;abc\x1b[A",
		[]string{`osc:"11;abc"`, `csi:priv="" params=[] inter="" final="A"`}},
	{"esc esc delivers first", "\x1b\x1ba",
		[]string{`exec:1B`, `esc:inter="" final="a"`}},
	{"sos pm apc swallowed", "\x1b_hidden\x1b\\x",
		[]string{`esc:inter="" final="\\"`, `print:"x"`}},
	{"csi ignore on bad private marker", "\x1b[1;?5m x",
		[]string{`print:" "`, `print:"x"`}},
	{"c0 inside csi executes", "\x1b[1\x0d;2H",
		[]string{`exec:0D`, `csi:priv="" params=[1 2] inter="" final="H"`}},
	{"stray continuation dropped", "\x80a", []string{`print:"a"`}},
	{"interrupted utf8 reprocesses", "\xc3A", []string{`print:"A"`}},
	{"mixed stream", "a\x1b[2;3H\x1b]0;t\x07é",
		[]string{`print:"a"`, `csi:priv="" params=[2 3] inter="" final="H"`, `osc:"0;t"`, `print:"é"`}},
}

func TestParserCorpus(t *testing.T) {
	// The table-driven corpus.
	for _, tc := range parserCorpus {
		t.Run(tc.name, func(t *testing.T) {
			got := parse(tc.input)
			if !equalStrings(got, tc.want) {
				t.Errorf("parse(%q)\n got: %v\nwant: %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParserSplitBoundaries(t *testing.T) {
	// The split property: byte-stream splits at arbitrary boundaries
	// never change the decoded action sequence. Byte-at-a-time plus random
	// multi-way splits per corpus entry.
	rng := rand.New(rand.NewSource(42))
	for _, tc := range parserCorpus {
		t.Run(tc.name, func(t *testing.T) {
			whole := parse(tc.input)

			// Byte-at-a-time via a single persistent parser.
			var p parser
			var oneByOne []string
			for i := 0; i < len(tc.input); i++ {
				p.feed(tc.input[i], func(a *action) { oneByOne = append(oneByOne, renderAction(a)) })
			}
			if !equalStrings(oneByOne, whole) {
				t.Fatalf("byte-at-a-time diverged\n got: %v\nwant: %v", oneByOne, whole)
			}

			// Random split points, many trials.
			for trial := 0; trial < 20; trial++ {
				var p2 parser
				var got []string
				emit := func(a *action) { got = append(got, renderAction(a)) }
				rest := tc.input
				for len(rest) > 0 {
					n := 1 + rng.Intn(len(rest))
					for i := 0; i < n; i++ {
						p2.feed(rest[i], emit)
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

func TestParserParamOverflowStillConsumes(t *testing.T) {
	// 32 params x 4 subparams, saturating — excess ignored,
	// sequence still consumed.
	var sb strings.Builder
	sb.WriteString("\x1b[")
	for i := 0; i < 40; i++ {
		if i > 0 {
			sb.WriteByte(';')
		}
		fmt.Fprintf(&sb, "%d", i+1)
	}
	sb.WriteByte('m')
	got := parse(sb.String())
	if len(got) != 1 || !strings.HasPrefix(got[0], "csi:") {
		t.Fatalf("overflowed CSI not dispatched: %v", got)
	}
	if !strings.Contains(got[0], "params=[1 ") || strings.Contains(got[0], " 33") {
		t.Fatalf("expected 32 params max, got %v", got)
	}

	// Sub-param overflow: 6 subparams collapse to 4, rest ignored.
	got = parse("\x1b[1:2:3:4:5:6m")
	want := []string{`csi:priv="" params=[1:2:3:4] inter="" final="m"`}
	if !equalStrings(got, want) {
		t.Fatalf("subparam overflow: got %v want %v", got, want)
	}
}

func TestParserOSCDataCap(t *testing.T) {
	long := strings.Repeat("x", maxStringData+100)
	got := parse("\x1b]52;" + long + "\x07")
	if len(got) != 1 {
		t.Fatalf("expected one OSC dispatch, got %d", len(got))
	}
	if len(got[0]) > maxStringData+20 {
		t.Fatalf("OSC data not capped: %d bytes", len(got[0]))
	}
}

func FuzzParserSplit(f *testing.F) {
	// Fuzz: any byte stream, split anywhere, decodes to the
	// same action sequence as the contiguous feed.
	for _, tc := range parserCorpus {
		f.Add([]byte(tc.input), 1)
	}
	f.Fuzz(func(t *testing.T, data []byte, split int) {
		if len(data) == 0 {
			return
		}
		split = ((split % len(data)) + len(data)) % len(data)

		var pw parser
		var whole []string
		emitW := func(a *action) { whole = append(whole, renderAction(a)) }
		for i := 0; i < len(data); i++ {
			pw.feed(data[i], emitW)
		}

		var ps parser
		var parts []string
		emitP := func(a *action) { parts = append(parts, renderAction(a)) }
		for i := 0; i < split; i++ {
			ps.feed(data[i], emitP)
		}
		for i := split; i < len(data); i++ {
			ps.feed(data[i], emitP)
		}
		if !equalStrings(whole, parts) {
			t.Fatalf("split at %d diverged\nwhole: %v\nsplit: %v", split, whole, parts)
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
