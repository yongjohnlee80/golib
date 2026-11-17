package qr

// grid is a code being drawn: its modules, and which of them belong to a function pattern, which
// neither codewords nor the mask may touch.
type grid struct {
	size     int
	version  int
	dark     []bool
	function []bool
}

func newGrid(version int) *grid {
	size := 17 + 4*version
	return &grid{size: size, version: version, dark: make([]bool, size*size), function: make([]bool, size*size)}
}

// set draws a function module.
func (g *grid) set(x, y int, dark bool) {
	g.dark[y*g.size+x] = dark
	g.function[y*g.size+x] = true
}

// drawFunctionPatterns draws everything but the codewords: the three finder patterns with their
// light separators, the timing patterns, the alignment patterns, and the version information. The
// format information's modules are reserved here, and written once the mask is chosen.
func (g *grid) drawFunctionPatterns() {
	for i := range g.size {
		g.set(6, i, i%2 == 0)
		g.set(i, 6, i%2 == 0)
	}
	g.drawFinder(3, 3)
	g.drawFinder(g.size-4, 3)
	g.drawFinder(3, g.size-4)
	pos := alignmentPositions(g.version)
	last := len(pos) - 1
	for i, x := range pos {
		for j, y := range pos {
			if i == 0 && j == 0 || i == 0 && j == last || i == last && j == 0 {
				continue // a finder pattern's corner
			}
			g.drawAlignment(x, y)
		}
	}
	g.drawFormat(L, 0)
	g.drawVersion()
}

// drawFinder draws a finder pattern centred on (cx, cy), with the light ring around it that
// separates it from the rest; the ring's part outside the code is left out.
func (g *grid) drawFinder(cx, cy int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			x, y := cx+dx, cy+dy
			if x < 0 || y < 0 || x >= g.size || y >= g.size {
				continue
			}
			d := max(abs(dx), abs(dy))
			g.set(x, y, d != 2 && d != 4)
		}
	}
}

// drawAlignment draws an alignment pattern centred on (cx, cy): a dark centre, a light ring and a
// dark ring.
func (g *grid) drawAlignment(cx, cy int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			g.set(cx+dx, cy+dy, max(abs(dx), abs(dy)) != 1)
		}
	}
}

// alignmentPositions are the rows and columns of a version's alignment patterns, which sit at
// every crossing of them but the three finder patterns' corners: 6, then evenly spaced up to
// size − 7, the spacing even and the gap left at the first.
func alignmentPositions(version int) []int {
	if version == 1 {
		return nil
	}
	n := version/7 + 2
	step := (version*8 + n*3 + 5) / (n*4 - 4) * 2
	pos := make([]int, n)
	pos[0] = 6
	for i, p := n-1, 17+4*version-7; i >= 1; i, p = i-1, p-step {
		pos[i] = p
	}
	return pos
}

// drawFormat writes the format information, the level and the mask with their BCH code, in its
// two copies: around the top left finder, and split between the other two. The one module that is
// always dark sits beside the bottom left copy.
func (g *grid) drawFormat(level Level, mask int) {
	data := level.formatBits()<<3 | mask
	rem := data
	for range 10 {
		rem = rem<<1 ^ (rem>>9)*0x537
	}
	bits := (data<<10 | rem) ^ 0x5412
	bit := func(i int) bool { return bits>>uint(i)&1 == 1 }
	for i := 0; i <= 5; i++ {
		g.set(8, i, bit(i))
	}
	g.set(8, 7, bit(6))
	g.set(8, 8, bit(7))
	g.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		g.set(14-i, 8, bit(i))
	}
	for i := range 8 {
		g.set(g.size-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		g.set(8, g.size-15+i, bit(i))
	}
	g.set(8, g.size-8, true)
}

// drawVersion writes the version information of versions 7 and up, the version with its Golay
// code, in its two copies beside the top right and bottom left finders.
func (g *grid) drawVersion() {
	if g.version < 7 {
		return
	}
	rem := g.version
	for range 12 {
		rem = rem<<1 ^ (rem>>11)*0x1F25
	}
	bits := g.version<<12 | rem
	for i := range 18 {
		dark := bits>>uint(i)&1 == 1
		a, b := g.size-11+i%3, i/3
		g.set(a, b, dark)
		g.set(b, a, dark)
	}
}

// placeCodewords writes the codewords' bits, most significant first, into the modules no function
// pattern holds: in columns two wide from the right, upward then downward in turn, skipping the
// vertical timing pattern's column. The modules left over are the remainder bits, light.
func (g *grid) placeCodewords(codewords []byte) {
	i, n := 0, len(codewords)*8
	for right := g.size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		upward := (right+1)&2 == 0
		for vert := range g.size {
			y := vert
			if upward {
				y = g.size - 1 - vert
			}
			for j := range 2 {
				x := right - j
				if g.function[y*g.size+x] || i >= n {
					continue
				}
				g.dark[y*g.size+x] = codewords[i/8]>>uint(7-i%8)&1 == 1
				i++
			}
		}
	}
}

// masks are the eight data masks: a module the mask's condition holds for is inverted.
var masks = [8]func(x, y int) bool{
	func(x, y int) bool { return (x+y)%2 == 0 },
	func(x, y int) bool { return y%2 == 0 },
	func(x, y int) bool { return x%3 == 0 },
	func(x, y int) bool { return (x+y)%3 == 0 },
	func(x, y int) bool { return (x/3+y/2)%2 == 0 },
	func(x, y int) bool { return x*y%2+x*y%3 == 0 },
	func(x, y int) bool { return (x*y%2+x*y%3)%2 == 0 },
	func(x, y int) bool { return ((x+y)%2+x*y%3)%2 == 0 },
}

// applyMask inverts the codeword modules the mask selects. Applied twice, it undoes itself.
func (g *grid) applyMask(mask int) {
	for y := range g.size {
		for x := range g.size {
			if !g.function[y*g.size+x] && masks[mask](x, y) {
				g.dark[y*g.size+x] = !g.dark[y*g.size+x]
			}
		}
	}
}

// bestMask is the mask whose code, format information included, has the lowest penalty; the
// first of them on a tie.
func (g *grid) bestMask(level Level) int {
	best, bestPenalty := 0, -1
	for m := range masks {
		g.applyMask(m)
		g.drawFormat(level, m)
		if p := penalty(g.dark, g.size); bestPenalty < 0 || p < bestPenalty {
			best, bestPenalty = m, p
		}
		g.applyMask(m)
	}
	return best
}

// The penalty weights, for runs of one colour, blocks of one colour, patterns that look like a
// finder, and an imbalance of dark and light.
const (
	penaltyRun     = 3
	penaltyBlock   = 3
	penaltyFinder  = 40
	penaltyBalance = 10
)

// penalty scores how hard a scanner may find the code: each run of five or more modules of one
// colour in a row or column scores 3, plus 1 for each module past five; each 2 × 2 block of one
// colour scores 3; each finder-like run dark, light, dark × 3, light, dark with four light modules
// before or after it scores 40, outside the code counting as light; and the share of dark modules
// scores 10 for each whole 5% it is away from half.
func penalty(dark []bool, size int) int {
	at := func(x, y int) bool { return dark[y*size+x] }
	p := 0
	for _, byRow := range []bool{true, false} {
		for a := range size {
			line := make([]bool, size)
			for b := range size {
				if byRow {
					line[b] = at(b, a)
				} else {
					line[b] = at(a, b)
				}
			}
			p += runPenalty(line) + finderPenalty(line)
		}
	}
	for y := range size - 1 {
		for x := range size - 1 {
			c := at(x, y)
			if c == at(x+1, y) && c == at(x, y+1) && c == at(x+1, y+1) {
				p += penaltyBlock
			}
		}
	}
	darkCount := 0
	for _, d := range dark {
		if d {
			darkCount++
		}
	}
	total := size * size
	p += abs(darkCount*2-total) * 10 / total * penaltyBalance
	return p
}

// runPenalty scores a line's runs of five or more modules of one colour.
func runPenalty(line []bool) int {
	p, run := 0, 0
	for i := range line {
		if i > 0 && line[i] == line[i-1] {
			run++
		} else {
			run = 1
		}
		if run == 5 {
			p += penaltyRun
		} else if run > 5 {
			p++
		}
	}
	return p
}

// finderPenalty scores a line's finder-like runs: 1011101 with 0000 before or after it, where
// past either end counts as 0.
func finderPenalty(line []bool) int {
	light := func(from, to int) bool {
		for i := max(from, 0); i < min(to, len(line)); i++ {
			if line[i] {
				return false
			}
		}
		return true
	}
	pattern := [7]bool{true, false, true, true, true, false, true}
	p := 0
	for i := 0; i+7 <= len(line); i++ {
		if [7]bool(line[i:i+7]) == pattern && (light(i-4, i) || light(i+7, i+11)) {
			p += penaltyFinder
		}
	}
	return p
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
