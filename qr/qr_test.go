package qr

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// fixture is a code made by another encoder, segno 1.6.6, a Python library written from the
// standard independently of this one, at a version it chose and a mask it was given. Its one
// departure from the standard was corrected before the fixtures were made: when the terminator
// ends on a byte boundary, segno adds eight zero bits more, a whole zero codeword, where the
// standard adds none before the pad codewords. Scanners stop at the terminator, so its codes
// still read, but the bits after it are not the standard's.
type fixture struct {
	name    string
	mode    mode
	level   Level
	version int
	mask    int
	data    []byte
	rows    [][]bool
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	f, err := os.Open("testdata/segno.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	modes := map[string]mode{"numeric": numericMode, "alphanumeric": alphanumericMode, "byte": byteMode}
	levels := map[string]Level{"L": L, "M": M, "Q": Q, "H": H}
	var out []fixture
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if rest, ok := strings.CutPrefix(line, "case "); ok {
			p := strings.Fields(rest)
			v, _ := strconv.Atoi(p[3])
			m, _ := strconv.Atoi(p[4])
			data, err := hex.DecodeString(p[5])
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, fixture{name: p[0], mode: modes[p[1]], level: levels[p[2]], version: v, mask: m, data: data})
			continue
		}
		fx := &out[len(out)-1]
		packed, err := hex.DecodeString(line)
		if err != nil {
			t.Fatal(err)
		}
		size := 17 + 4*fx.version
		row := make([]bool, size)
		for x := range size {
			row[x] = packed[x/8]>>uint(7-x%8)&1 == 1
		}
		fx.rows = append(fx.rows, row)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) < 15 {
		t.Fatalf("read %d fixtures", len(out))
	}
	return out
}

// TestCodesMatchAnotherEncoderModuleForModule: with the version and mask fixed, every module of
// every fixture is the other encoder's. That covers the segment's bits, the padding, the blocks
// and their error correction, the interleaving, the placement, the masks, and the format and
// version information, across all three modes, the four levels, the eight masks and versions 1 to
// 40, the character count's widths changing at 10 and 27 included.
func TestCodesMatchAnotherEncoderModuleForModule(t *testing.T) {
	for _, fx := range loadFixtures(t) {
		t.Run(fx.name, func(t *testing.T) {
			if got := smallestVersion(len(fx.data), fx.mode, fx.level); got != fx.version {
				t.Fatalf("the smallest version is %d; the other encoder chose %d", got, fx.version)
			}
			c, err := encode(fx.data, fx.mode, fx.level, fx.version, fx.mask)
			if err != nil {
				t.Fatal(err)
			}
			if c.Size != len(fx.rows) {
				t.Fatalf("size %d, want %d", c.Size, len(fx.rows))
			}
			wrong := 0
			for y, row := range fx.rows {
				for x, want := range row {
					if c.Black(x, y) != want {
						if wrong < 5 {
							t.Errorf("module (%d, %d) is %v", x, y, !want)
						}
						wrong++
					}
				}
			}
			if wrong > 0 {
				t.Fatalf("%d modules differ", wrong)
			}
		})
	}
}

// TestEncodeChoosesTheModeAndTheSmallestVersion: the public entry points pick the mode from the
// text, and the version the other encoder picked for the same data.
func TestEncodeChoosesTheModeAndTheSmallestVersion(t *testing.T) {
	for _, fx := range loadFixtures(t) {
		var c *Code
		var err error
		if fx.mode == byteMode && modeFor(string(fx.data)) != byteMode {
			c, err = EncodeBytes(fx.data, fx.level)
		} else {
			c, err = Encode(string(fx.data), fx.level)
		}
		if err != nil {
			t.Fatalf("%s: %v", fx.name, err)
		}
		if c.Version != fx.version || c.Level != fx.level || c.Size != 17+4*fx.version {
			t.Errorf("%s: version %d level %v size %d, want version %d level %v", fx.name, c.Version, c.Level, c.Size, fx.version, fx.level)
		}
		if got := decode(t, c); !bytes.Equal(got, fx.data) {
			t.Errorf("%s: decoded %q", fx.name, got)
		}
	}
}

// TestEncodeBytesIsByteMode: digits given as bytes stay bytes. 41 digits fill a version 1 code at
// L in numeric mode; as 41 bytes they need version 3.
func TestEncodeBytesIsByteMode(t *testing.T) {
	c, err := EncodeBytes([]byte(strings.Repeat("7", 41)), L)
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != 3 {
		t.Errorf("41 digits as bytes: version %d, want 3", c.Version)
	}
}

// TestAlignmentPositionsAreTheStandards: every version's alignment pattern rows and columns, as
// the standard's table lists them.
func TestAlignmentPositionsAreTheStandards(t *testing.T) {
	want := map[int][]int{
		1: nil, 2: {6, 18}, 3: {6, 22}, 4: {6, 26}, 5: {6, 30}, 6: {6, 34},
		7: {6, 22, 38}, 8: {6, 24, 42}, 9: {6, 26, 46}, 10: {6, 28, 50}, 11: {6, 30, 54}, 12: {6, 32, 58}, 13: {6, 34, 62},
		14: {6, 26, 46, 66}, 15: {6, 26, 48, 70}, 16: {6, 26, 50, 74}, 17: {6, 30, 54, 78}, 18: {6, 30, 56, 82}, 19: {6, 30, 58, 86}, 20: {6, 34, 62, 90},
		21: {6, 28, 50, 72, 94}, 22: {6, 26, 50, 74, 98}, 23: {6, 30, 54, 78, 102}, 24: {6, 28, 54, 80, 106}, 25: {6, 32, 58, 84, 110}, 26: {6, 30, 58, 86, 114}, 27: {6, 34, 62, 90, 118},
		28: {6, 26, 50, 74, 98, 122}, 29: {6, 30, 54, 78, 102, 126}, 30: {6, 26, 52, 78, 104, 130}, 31: {6, 30, 56, 82, 108, 134}, 32: {6, 34, 60, 86, 112, 138}, 33: {6, 30, 58, 86, 114, 142}, 34: {6, 34, 62, 90, 118, 146},
		35: {6, 30, 54, 78, 102, 126, 150}, 36: {6, 24, 50, 76, 102, 128, 154}, 37: {6, 28, 54, 80, 106, 132, 158}, 38: {6, 32, 58, 84, 110, 136, 162}, 39: {6, 26, 54, 82, 110, 138, 166}, 40: {6, 30, 58, 86, 114, 142, 170},
	}
	for v := 1; v <= 40; v++ {
		if got := alignmentPositions(v); !slices.Equal(got, want[v]) {
			t.Errorf("version %d: %v, want %v", v, got, want[v])
		}
	}
}

func TestModeFor(t *testing.T) {
	for text, want := range map[string]mode{
		"":                 numericMode,
		"0123456789":       numericMode,
		"HELLO WORLD $%*+": alphanumericMode,
		"12AB-./:":         alphanumericMode,
		"Hello":            byteMode,
		"app_k3j9":         byteMode,
		"9\x00":            byteMode,
		"é":                byteMode,
	} {
		if got := modeFor(text); got != want {
			t.Errorf("modeFor(%q) = %d, want %d", text, got, want)
		}
	}
}

// TestCapacityAtItsEdge: the most a version 40 code holds, in each mode at L, is accepted, and
// one character more is refused; the same at version 1 moves to version 2.
func TestCapacityAtItsEdge(t *testing.T) {
	for _, c := range []struct {
		name  string
		char  string
		level Level
		v1    int // the most version 1 holds
		v40   int // the most version 40 holds
	}{
		{"numeric", "7", L, 41, 7089},
		{"alphanumeric", "A", L, 25, 4296},
		{"byte", "a", L, 17, 2953},
		{"byte", "a", M, 14, 2331},
		{"byte", "a", Q, 11, 1663},
		{"byte", "a", H, 7, 1273},
	} {
		if code, err := Encode(strings.Repeat(c.char, c.v1), c.level); err != nil || code.Version != 1 {
			t.Errorf("%s %v: %d characters: %v, version %v", c.name, c.level, c.v1, err, code)
		}
		if code, err := Encode(strings.Repeat(c.char, c.v1+1), c.level); err != nil || code.Version != 2 {
			t.Errorf("%s %v: %d characters do not move to version 2: %v", c.name, c.level, c.v1+1, err)
		}
		if code, err := Encode(strings.Repeat(c.char, c.v40), c.level); err != nil || code.Version != 40 {
			t.Errorf("%s %v: %d characters: %v", c.name, c.level, c.v40, err)
		}
		if _, err := Encode(strings.Repeat(c.char, c.v40+1), c.level); !errors.Is(err, ErrTooLong) {
			t.Errorf("%s %v: %d characters: %v, want ErrTooLong", c.name, c.level, c.v40+1, err)
		}
	}
	if _, err := EncodeBytes(make([]byte, 2954), L); !errors.Is(err, ErrTooLong) || !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("EncodeBytes past version 40: %v", err)
	}
	if _, err := encode([]byte("0123456789"), numericMode, H, 1, 0); err != nil {
		t.Errorf("10 digits at 1-H: %v", err)
	}
	if _, err := encode([]byte("01234567890123456789"), numericMode, H, 1, 0); !errors.Is(err, ErrTooLong) {
		t.Errorf("20 digits forced into 1-H: %v, want ErrTooLong", err)
	}
}

func TestLevels(t *testing.T) {
	if !H.Valid() || (H + 1).Valid() {
		t.Error("Level.Valid does not end at H")
	}
	for l, want := range map[Level]string{L: "L", M: "M", Q: "Q", H: "H", H + 1: "Level(?)"} {
		if l.String() != want {
			t.Errorf("%d.String() = %q", l, l.String())
		}
	}
	if _, err := Encode("x", H+1); !errors.Is(err, ErrLevel) || !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("Encode at level H+1: %v", err)
	}
}

func TestBlackOutsideIsLight(t *testing.T) {
	c, err := Encode("x", M)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Black(0, 0) || !c.Black(c.Size-1, 0) || !c.Black(0, c.Size-1) {
		t.Fatal("a finder pattern's corner is not dark")
	}
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {c.Size, 0}, {0, c.Size}, {c.Size, c.Size}} {
		if c.Black(p[0], p[1]) {
			t.Errorf("(%d, %d), outside the code, is dark", p[0], p[1])
		}
	}
}

// TestEveryVersionAndLevelRoundTrips: for each version and level, a code of random bytes that
// fill it reads back through an independent decoder: its format information names the level and
// mask, every block's error correction holds (its syndromes are zero), and the segment is the
// data.
func TestEveryVersionAndLevelRoundTrips(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for v := 1; v <= 40; v++ {
		for l := L; l <= H; l++ {
			n := (8*dataCodewords(v, l) - 4 - byteMode.countBits(v)) / 8
			data := make([]byte, n)
			for i := range data {
				data[i] = byte(rng.Uint32())
			}
			c, err := EncodeBytes(data, l)
			if err != nil {
				t.Fatalf("%d-%v: %v", v, l, err)
			}
			if c.Version != v {
				t.Fatalf("%d-%v: %d bytes chose version %d", v, l, n, c.Version)
			}
			if got := decode(t, c); !bytes.Equal(got, data) {
				t.Fatalf("%d-%v: the data does not read back", v, l)
			}
		}
	}
}

// TestTheMaskChosenHasTheLowestPenalty: of the eight masks, the one chosen scores no more than
// any other, and the first on a tie.
func TestTheMaskChosenHasTheLowestPenalty(t *testing.T) {
	// "tie-25" and "tie-105" each have two masks sharing the lowest score.
	for _, text := range []string{"app_k3j9x0q2m8w7v5t1r4z6y0u2i8o3p7a9s", "HELLO WORLD", "01234567", strings.Repeat("ab", 200), "tie-25", "tie-105"} {
		c, err := Encode(text, M)
		if err != nil {
			t.Fatal(err)
		}
		scores := make([]int, 8)
		for m := range 8 {
			o, err := encode([]byte(text), modeFor(text), M, c.Version, m)
			if err != nil {
				t.Fatal(err)
			}
			scores[m] = penalty(o.dark, o.Size)
		}
		for m, s := range scores {
			if s < scores[c.Mask] || s == scores[c.Mask] && m < c.Mask {
				t.Errorf("%q: mask %d scores %d, the chosen %d scores %d", text, m, s, c.Mask, scores[c.Mask])
			}
		}
	}
}

func line(s string) []bool {
	out := make([]bool, len(s))
	for i, c := range s {
		out[i] = c == '1'
	}
	return out
}

func TestRunPenalty(t *testing.T) {
	for s, want := range map[string]int{
		"1010":         0,
		"11110":        0,
		"11111":        3,
		"111111":       4,
		"0000000":      5,
		"111110000011": 6,
	} {
		if got := runPenalty(line(s)); got != want {
			t.Errorf("runPenalty(%s) = %d, want %d", s, got, want)
		}
	}
}

func TestFinderPenalty(t *testing.T) {
	for s, want := range map[string]int{
		"00001011101":     40, // light before
		"000010111011":    40, // light before only: dark within four after it
		"10111010000":     40, // light after
		"1011101":         40, // past the ends counts as light
		"11011101":        40, // dark before it, but past the end is light
		"101110110":       40, // dark after it, but before the start is light
		"1000101110110":   0,  // neither side has four light modules
		"000010111010000": 40, // once, though both sides are light
		"1011100":         0,
	} {
		if got := finderPenalty(line(s)); got != want {
			t.Errorf("finderPenalty(%s) = %d, want %d", s, got, want)
		}
	}
}

func TestPenaltyCountsBlocksAndBalance(t *testing.T) {
	// A 2 × 2 all dark: one block, and all of it dark is 10 steps of 5% from half.
	if got := penalty([]bool{true, true, true, true}, 2); got != penaltyBlock+10*penaltyBalance {
		t.Errorf("all dark: %d", got)
	}
	// A checkerboard: no run, no block, no finder, balanced.
	board := make([]bool, 36)
	for i := range board {
		board[i] = (i/6+i%6)%2 == 0
	}
	if got := penalty(board, 6); got != 0 {
		t.Errorf("checkerboard: %d", got)
	}
	// 3 dark of 4 is 5 steps of 5% from half, and 2 × 2 has no block of one colour.
	if got := penalty([]bool{true, true, true, false}, 2); got != 5*penaltyBalance {
		t.Errorf("three of four dark: %d", got)
	}
}

// decode reads a code back the way a scanner does once it has the grid, written from the
// standard apart from the encoder: it shares only the function-pattern layout and the block
// table. The error correction is checked by its syndromes, which the encoder never computes.
func decode(t *testing.T, c *Code) []byte {
	t.Helper()
	level, mask := readFormat(t, c)
	if level != c.Level || mask != c.Mask {
		t.Fatalf("the format information says %v mask %d; the code is %v mask %d", level, mask, c.Level, c.Mask)
	}
	g := newGrid(c.Version)
	g.drawFunctionPatterns()
	var bits []bool
	for right := c.Size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := range c.Size {
			y := vert
			if (right+1)&2 == 0 {
				y = c.Size - 1 - vert
			}
			for _, x := range []int{right, right - 1} {
				if !g.function[y*c.Size+x] {
					bits = append(bits, c.Black(x, y) != masks[mask](x, y))
				}
			}
		}
	}
	total := totalCodewords(c.Version)
	if len(bits)/8 != total {
		t.Fatalf("%d codeword bits in a version %d code", len(bits), c.Version)
	}
	raw := make([]byte, total)
	for i := range total * 8 {
		if bits[i] {
			raw[i/8] |= 0x80 >> uint(i%8)
		}
	}
	n, ecLen := blocks[level][c.Version], eccPerBlock[level][c.Version]
	dataLen := total - n*ecLen
	short, longs := dataLen/n, dataLen%n
	blockData := make([][]byte, n)
	at := 0
	for i := 0; i <= short; i++ {
		for b := range n {
			if i < short || b >= n-longs {
				blockData[b] = append(blockData[b], raw[at])
				at++
			}
		}
	}
	var data []byte
	for b := range n {
		block := append([]byte(nil), blockData[b]...)
		for i := range ecLen {
			block = append(block, raw[dataLen+i*n+b])
		}
		for i := range ecLen {
			if s := syndrome(block, i); s != 0 {
				t.Fatalf("block %d: syndrome %d is %d", b, i, s)
			}
		}
		data = append(data, blockData[b]...)
	}
	return readSegment(t, data, c.Version)
}

// readFormat reads the format information's first copy, and checks the second is the same.
func readFormat(t *testing.T, c *Code) (Level, int) {
	t.Helper()
	var a, b int
	at := func(x, y int) int {
		if c.Black(x, y) {
			return 1
		}
		return 0
	}
	for i := 0; i <= 5; i++ {
		a |= at(8, i) << i
	}
	a |= at(8, 7)<<6 | at(8, 8)<<7 | at(7, 8)<<8
	for i := 9; i < 15; i++ {
		a |= at(14-i, 8) << i
	}
	for i := range 8 {
		b |= at(c.Size-1-i, 8) << i
	}
	for i := 8; i < 15; i++ {
		b |= at(8, c.Size-15+i) << i
	}
	if a != b {
		t.Fatalf("the format information's copies differ: %015b, %015b", a, b)
	}
	if !c.Black(8, c.Size-8) {
		t.Fatal("the dark module is light")
	}
	v := (a ^ 0x5412) >> 10
	return [...]Level{1: L, 0: M, 3: Q, 2: H}[v>>3], v & 7
}

// syndrome evaluates the block, read as a polynomial with its first codeword the highest power,
// at α^i, by Horner's rule; a block whose error correction holds evaluates to 0 at α⁰ … α^(n−1).
func syndrome(block []byte, i int) byte {
	x := byte(1)
	for range i {
		x = gfMul(x, 2)
	}
	var s byte
	for _, c := range block {
		s = gfMul(s, x) ^ c
	}
	return s
}

// readSegment reads the one segment the encoder writes, and checks the padding after it.
func readSegment(t *testing.T, data []byte, version int) []byte {
	t.Helper()
	pos := 0
	read := func(n int) int {
		v := 0
		for range n {
			v = v<<1 | int(data[pos/8]>>uint(7-pos%8)&1)
			pos++
		}
		return v
	}
	var m mode
	switch read(4) {
	case 0b0001:
		m = numericMode
	case 0b0010:
		m = alphanumericMode
	case 0b0100:
		m = byteMode
	default:
		t.Fatal("unknown mode indicator")
	}
	count := read(m.countBits(version))
	var out []byte
	switch m {
	case numericMode:
		for left := count; left > 0; left -= 3 {
			k := min(left, 3)
			s := strconv.Itoa(read([...]int{0, 4, 7, 10}[k]))
			out = append(out, strings.Repeat("0", k-len(s))+s...)
		}
	case alphanumericMode:
		for left := count; left > 0; left -= 2 {
			if left == 1 {
				out = append(out, alphanumeric[read(6)])
			} else {
				v := read(11)
				out = append(out, alphanumeric[v/45], alphanumeric[v%45])
			}
		}
	default:
		for range count {
			out = append(out, byte(read(8)))
		}
	}
	if rest := len(data)*8 - pos; rest > 0 && read(min(4, rest)) != 0 {
		t.Fatal("the terminator is not zero")
	}
	for pos%8 != 0 {
		if read(1) != 0 {
			t.Fatal("the bits to the byte's end are not zero")
		}
	}
	for i, pad := pos/8, byte(0xEC); i < len(data); i, pad = i+1, pad^0xEC^0x11 {
		if data[i] != pad {
			t.Fatalf("pad byte %d is %#x, want %#x", i, data[i], pad)
		}
	}
	return out
}
