# qr

A QR code encoder: text in, a grid of modules out. It covers QR codes as phones read them
(Model 2): versions 1 to 40, the error correction levels L, M, Q and H, and the numeric,
alphanumeric and byte modes. It draws nothing, so the same `Code` becomes cells of a terminal,
pixels or SVG in whatever the caller renders with. No dependencies.

```bash
go get github.com/yongjohnlee80/golib/qr
```

```go
import "github.com/yongjohnlee80/golib/qr"
```

## Encoding

`Encode(text, level)` picks the mode from the text: digits alone are numeric, digits, upper-case
letters and `space $ % * + - . / :` alphanumeric, and anything else is byte mode, the text's UTF-8
bytes, which scanners read as UTF-8. `EncodeBytes(data, level)` is byte mode for any bytes. Both
choose the smallest version that holds the data, and the mask that leaves the fewest patterns a
scanner could misread.

```go
c, err := qr.Encode("https://example.com", qr.M)
if errors.Is(err, qr.ErrTooLong) {
	// more than a version 40 code holds at M
}
c.Size    // modules on a side: 17 + 4 × c.Version
c.Version // 1 to 40
c.Mask    // 0 to 7
```

| Level | Recovers about | A version 40 code holds, in bytes |
| --- | --- | --- |
| `qr.L` | 7% | 2953 |
| `qr.M` | 15% | 2331 |
| `qr.Q` | 25% | 1663 |
| `qr.H` | 30% | 1273 |

An unknown level answers `ErrLevel`; data too long for version 40 answers `ErrTooLong`. Both are
`errs.ErrInvalidArgument`.

## Drawing

`c.Black(x, y)` is whether the module at column `x`, row `y` is dark; outside the code it is light.
The code has no margin of its own: draw a quiet zone of the light colour around it, four modules
wide by the standard, though scanners read two.

```go
const quiet = 4
for y := -quiet; y < c.Size+quiet; y++ {
	for x := -quiet; x < c.Size+quiet; x++ {
		if c.Black(x, y) {
			// draw a dark module at (x+quiet, y+quiet)
		}
	}
}
```

In a terminal, two rows of modules fit one row of cells with the half blocks `▀`, `▄` and `█`.
Cameras expect dark modules on light, so on a dark background draw the light modules instead.

## Not covered

One segment per code (no mixing of modes), no ECI designator (byte mode is read as UTF-8 by
convention), no Kanji mode, no Micro QR and no structured append.

## How it is tested

The tests compare codes module for module with another encoder's, written independently in another
language, for every mode, level and mask, versions 1 to 40, and read every version at every level
back through a decoder in the tests that checks the error correction by its syndromes. Before
release, codes of each mode at every version and level were rendered and read by two scanners'
decoders.
