package qr

// eccPerBlock is the number of error correction codewords in each block, by level and version
// (index 0 unused).
var eccPerBlock = [4][41]int{
	L: {0, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18, 20, 24, 26, 30, 22, 24, 28, 30, 28, 28, 28, 28, 30, 30, 26, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
	M: {0, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26, 26, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28},
	Q: {0, 13, 22, 18, 26, 18, 24, 18, 22, 20, 24, 28, 26, 24, 20, 30, 24, 28, 28, 26, 30, 28, 30, 30, 30, 30, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
	H: {0, 17, 28, 22, 16, 22, 28, 26, 26, 24, 28, 24, 28, 22, 24, 24, 30, 28, 28, 26, 28, 30, 24, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
}

// blocks is the number of blocks the codewords are split into, by level and version (index 0
// unused).
var blocks = [4][41]int{
	L: {0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4, 4, 4, 4, 4, 6, 6, 6, 6, 7, 8, 8, 9, 9, 10, 12, 12, 12, 13, 14, 15, 16, 17, 18, 19, 19, 20, 21, 22, 24, 25},
	M: {0, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16, 17, 17, 18, 20, 21, 23, 25, 26, 28, 29, 31, 33, 35, 37, 38, 40, 43, 45, 47, 49},
	Q: {0, 1, 1, 2, 2, 4, 4, 6, 6, 8, 8, 8, 10, 12, 16, 12, 17, 16, 18, 21, 20, 23, 23, 25, 27, 29, 34, 34, 35, 38, 40, 43, 45, 48, 51, 53, 56, 59, 62, 65, 68},
	H: {0, 1, 1, 2, 4, 4, 4, 5, 6, 8, 8, 11, 11, 16, 16, 18, 16, 19, 21, 25, 25, 25, 34, 30, 32, 35, 37, 40, 42, 45, 48, 51, 54, 57, 60, 63, 66, 70, 74, 77, 81},
}

// rawModules is the number of modules of a version that hold codewords: all of them but the
// finder patterns with their separators, the timing patterns, the alignment patterns, the format
// and version information and the one dark module. Some versions leave up to seven of them over,
// as remainder bits.
func rawModules(version int) int {
	n := (16*version+128)*version + 64
	if version >= 2 {
		align := version/7 + 2
		n -= (25*align-10)*align - 55
		if version >= 7 {
			n -= 36
		}
	}
	return n
}

// totalCodewords is the number of codewords a version holds, data and error correction.
func totalCodewords(version int) int { return rawModules(version) / 8 }

// dataCodewords is the number of data codewords a version holds at a level.
func dataCodewords(version int, level Level) int {
	return totalCodewords(version) - eccPerBlock[level][version]*blocks[level][version]
}

// addErrorCorrection splits the data codewords into the version's blocks at a level, computes
// each block's error correction codewords, and interleaves them: the first codeword of every
// block, then the second, and so on, the data of all blocks before their error correction. The
// shorter blocks come first and are one data codeword shorter than the others.
func addErrorCorrection(data []byte, version int, level Level) []byte {
	n, ecLen := blocks[level][version], eccPerBlock[level][version]
	shortLen := len(data) / n
	longs := len(data) % n
	divisor := rsDivisor(ecLen)
	dataBlocks := make([][]byte, n)
	ecBlocks := make([][]byte, n)
	at := 0
	for i := range n {
		size := shortLen
		if i >= n-longs {
			size++
		}
		dataBlocks[i] = data[at : at+size]
		ecBlocks[i] = rsRemainder(dataBlocks[i], divisor)
		at += size
	}
	out := make([]byte, 0, totalCodewords(version))
	for i := 0; i <= shortLen; i++ {
		for _, b := range dataBlocks {
			if i < len(b) {
				out = append(out, b[i])
			}
		}
	}
	for i := range ecLen {
		for _, b := range ecBlocks {
			out = append(out, b[i])
		}
	}
	return out
}

// gfMul multiplies two elements of GF(256), whose reducing polynomial is x⁸ + x⁴ + x³ + x² + 1.
func gfMul(x, y byte) byte {
	var z byte
	for i := 7; i >= 0; i-- {
		carry := z >> 7
		z = z<<1 ^ carry*0x1D
		z ^= (y >> uint(i) & 1) * x
	}
	return z
}

// rsDivisor is the generator polynomial of degree n, (x − α⁰)(x − α¹)…(x − αⁿ⁻¹) with α = 2,
// its coefficients from the highest power down, the leading 1 left out.
func rsDivisor(n int) []byte {
	g := make([]byte, n)
	g[n-1] = 1
	root := byte(1)
	for range n {
		for j := range g {
			g[j] = gfMul(g[j], root)
			if j+1 < n {
				g[j] ^= g[j+1]
			}
		}
		root = gfMul(root, 2)
	}
	return g
}

// rsRemainder is the error correction codewords of data: the remainder of data, shifted up by
// the divisor's degree, divided by the divisor.
func rsRemainder(data, divisor []byte) []byte {
	r := make([]byte, len(divisor))
	for _, b := range data {
		factor := b ^ r[0]
		copy(r, r[1:])
		r[len(r)-1] = 0
		for i, d := range divisor {
			r[i] ^= gfMul(d, factor)
		}
	}
	return r
}
