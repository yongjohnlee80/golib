# grapheme

`grapheme` is the foundational Unicode text segmentation and terminal cell-width measurement package for `golib/tui`. It implements Unicode Standard Annex #29 (UAX #29 Extended Grapheme Clusters), Unicode Standard Annex #11 (UAX #11 East Asian Width), and Unicode Technical Standard #51 (UTS #51 Emoji Presentation) with **zero external dependencies** and **zero heap allocations** on standard iteration paths.

---

## The Problem: Why Terminal UIs Cannot Use `len()` or `utf8.RuneCount`

In terminal user interfaces (TUIs), strings are rendered onto a discrete two-dimensional grid of monospace character cells:

```text
       Column 0    Column 1    Column 2    Column 3    Column 4    Column 5    Column 6    Column 7
    ┌───────────┬───────────┬───────────┬───────────┬───────────┬───────────┬───────────┬───────────┐
Row0│    'H'    │    'e'    │    'l'    │    'l'    │    'o'    │    ' '    │           │           │
    ├───────────┼───────────┼───────────┴───────────┼───────────┴───────────┼───────────┼───────────┤
Row1│    'e'    │    '́'     │          '世'         │          '界'         │    '!'    │           │
    ├───────────┴───────────┼───────────────────────┴───────────┬───────────┼───────────┼───────────┤
Row2│         '🇰🇷'          │                '👨‍👩‍👧'               │    'x'    │           │           │
    └───────────────────────┴───────────────────────────────────┴───────────┴───────────┴───────────┘
```

Treating text as raw bytes or code points causes severe visual and behavioural defects in real-world TUIs:

1. **Bytes (`len(s))`**: Slicing strings by byte offsets cuts through multi-byte UTF-8 encodings, corrupting data and printing Unicode replacement glyphs (`�`).
2. **Runes (`utf8.RuneCountInString(s))`**: Slicing by rune boundaries prevents encoding errors, but splits multi-rune characters. For example, the accented character `é` can be represented as `e` (U+0065) plus a combining acute accent `́` (U+0301). Slicing between them leaves an orphaned accent mark that attaches to whatever character follows on screen.
3. **Grapheme Clusters (User-Perceived Characters)**: Slicing must happen on **extended grapheme cluster boundaries** so that base characters, combining marks, variation selectors, and zero-width joiners (ZWJ) remain intact as a single user-perceived glyph.
4. **Terminal Cells (Display Columns)**: A single grapheme cluster can occupy **0, 1, or 2 columns** on a monospace terminal screen:
   - **0 columns**: Combining marks, non-spacing accents, C0/C1 control codes, and default-ignorable characters (e.g. ZWJ, variation selectors).
   - **1 column**: Standard Latin letters, digits, punctuation, halfwidth Katakana.
   - **2 columns**: East Asian Wide/Fullwidth ideographs (CJK Kanji, Hanzi, Hanja), emoji symbols, and Regional Indicator country flags.

### Representation Comparison

| Input Text                        | Bytes | Runes | Grapheme Clusters | Terminal Cells (Width) | Terminal Screen Representation |
| :-------------------------------- | :---- | :---- | :---------------- | :--------------------- | :----------------------------- |
| `"A"`                             | 1     | 1     | 1                 | 1                      | `[A]`                          |
| `"é"` (precomposed U+00E9)        | 2     | 1     | 1                 | 1                      | `[é]`                          |
| `"e\u0301"` (decomposed `e` + `́`) | 3     | 2     | 1                 | 1                      | `[é]`                          |
| `"世"` (CJK Ideograph)            | 3     | 1     | 1                 | 2                      | `[ 世 ]`                       |
| `"🇰🇷"` (Flag: `🇰` + `🇷`)      | 8     | 2     | 1                   | 2                      | `[ 🇰🇷 ]`                       |
| `"❤️"` (Heart + VS16)             | 6     | 2     | 1                 | 2                      | `[ ❤️ ]`                       |
| `"👨‍👩‍👧"` (Family emoji ZWJ chain)   | 18    | 5     | 1                 | 2                      | `[ 👨‍👩‍👧 ]`                       |

If a TUI computes cursor positioning, line truncation, or box borders using rune counts or byte lengths:

- **Box borders shear** because wide characters push vertical lines to the right.
- **The cursor drifts** away from the actual text insertion point.
- **Lines truncate prematurely or overflow** into adjacent widgets.

---

## Architecture & Zero-Allocation Invariants

`grapheme` sits at the lowest layer of `golib/tui`, directly beneath `Surface`, `CellBuffer`, `widget.Editor`, and `widget.Box`. Because text measurement happens on every frame and every keystroke, this package enforces strict performance guarantees:

- **Zero Heap Allocations**: `Clusters` returns Go 1.23+ `iter.Seq[string]` (range-over-func). Each yielded cluster is a contiguous subslice of the input string without allocating memory or copying bytes.
- **ASCII Fast Paths**: Over 95% of text in source code and configuration is standard ASCII. `clusterLen` and `StringWidth` evaluate ASCII bytes and ASCII-ASCII pairs with inline CPU checks, bypassing the table lookups entirely.
- **Cache-Friendly Tables**: Generated Unicode tables in `tables.go` are dense, flat arrays of non-overlapping rune ranges. Binary searches (`gbLookup`, `inRanges`) run in $O(\log N)$ without pointer dereferences or interface overhead.
- **Offline Reproducibility**: Tables are generated from pinned Unicode Character Database (UCD) files mirrored in `gen/testdata/`. The table generator (`gen/gen.go`) verifies SHA-256 hashes of all inputs so builds are deterministic and require no network access.

---

## API Reference

### `Clusters(s string) iter.Seq[string]`

Yields the extended grapheme clusters of `s` in order, strictly following UAX #29 rules GB1–GB13 and GB999.

```go
import "github.com/yongjohnlee80/golib/tui/internal/grapheme"

for cluster := range grapheme.Clusters("Hello 世界 👨‍👩‍👧!") {
    // cluster: "H", "e", "l", "l", "o", " ", "世", "界", " ", "👨‍👩‍👧", "!"
}
```

- **Round-trip Guarantee**: Concatenating all yielded clusters reproduces the input string byte-for-byte.
- **Safety**: Malformed UTF-8 sequences are yielded as individual single-byte clusters without panicking or stalling.

### `ClusterWidth(cluster string, ambiguousWide bool) int`

Returns the terminal cell width (`0`, `1`, or `2`) of a single grapheme cluster.

```go
w1 := grapheme.ClusterWidth("A", false)          // 1
w2 := grapheme.ClusterWidth("界", false)         // 2
w3 := grapheme.ClusterWidth("👨‍👩‍👧", false)       // 2
w4 := grapheme.ClusterWidth("\u0301", false)     // 0 (combining acute)
```

- **Base Width**: The width of the cluster's first non-zero-width rune.
- **Overrides**:
  - `VS16` (U+FE0F) forces width `2` (emoji presentation).
  - `VS15` (U+FE0E) forces width `1` (text presentation).
  - Regional Indicator pairs (flags) force width `2`.
- **`ambiguousWide`**: Controls East Asian Ambiguous characters (UAX #11). When `false` (default), they measure `1` column (Western terminals). When `true`, they measure `2` columns (legacy CJK environments).

### `StringWidth(s string, ambiguousWide bool) int`

Returns the total visual cell width of string `s` across all clusters.

```go
total := grapheme.StringWidth("Status: [OK] 🚀", false)
```

Uses an inline ASCII fast path to sum widths at memory-bandwidth speeds.

---

## Real-World TUI Usage Patterns

### 1. Drawing Box Borders Without Misalignment

In widgets like `widget.Box` or modal dialogs, vertical borders must align perfectly regardless of whether the contained text contains CJK or emoji:

```go
func DrawBox(title, body string, width int) {
    // Calculate display width accurately
    titleWidth := grapheme.StringWidth(title, false)
    padding := width - 2 - titleWidth
    if padding < 0 {
        padding = 0
    }

    fmt.Printf("┌─ %s %s┐\n", title, strings.Repeat("─", padding))
    // Render content...
}
```

### 2. Cluster-Safe String Truncation

Truncating text to fit a fixed terminal column budget must never split a multi-rune cluster or leave a double-width character cut in half:

```go
// TruncateToWidth truncates text to fit within maxWidth terminal cells,
// appending ellipsis ("...") if truncated, without cutting grapheme clusters.
func TruncateToWidth(text string, maxWidth int) string {
    if grapheme.StringWidth(text, false) <= maxWidth {
        return text
    }

    ellipsisWidth := grapheme.StringWidth("...", false)
    targetWidth := maxWidth - ellipsisWidth
    if targetWidth <= 0 {
        return "..."
    }

    var b strings.Builder
    curWidth := 0

    for cluster := range grapheme.Clusters(text) {
        cw := grapheme.ClusterWidth(cluster, false)
        if curWidth+cw > targetWidth {
            break
        }
        b.WriteString(cluster)
        curWidth += cw
    }

    b.WriteString("...")
    return b.String()
}
```

### 3. Cursor Coordinate Calculation

In modal editors (e.g., `editor/main`), the cursor's physical screen column `(x)` is determined by the cumulative cell widths of preceding grapheme clusters, NOT the byte or rune offset:

```go
func BufferColToScreenX(line string, byteOffset int) int {
    screenX := 0
    traversedBytes := 0

    for cluster := range grapheme.Clusters(line) {
        if traversedBytes >= byteOffset {
            break
        }
        screenX += grapheme.ClusterWidth(cluster, false)
        traversedBytes += len(cluster)
    }

    return screenX
}
```

---

## Conformance and Standards

| Standard    | Coverage                                                       | Test Suite                                      |
| :---------- | :------------------------------------------------------------- | :---------------------------------------------- |
| **UAX #29** | Extended Grapheme Clusters (GB1–GB13, GB999)                   | `GraphemeBreakTest.txt` (602 test cases passed) |
| **UAX #11** | East Asian Width (Wide, Fullwidth, Ambiguous, Narrow, Neutral) | `width_test.go`                                 |
| **UTS #51** | Emoji Presentation, ZWJ sequences, VS15/VS16 selectors, Flags  | `segment_test.go`, `width_test.go`              |

> **Unicode Version Pin**: Pinned to **Unicode 15.0.0** to match the Go 1.25 standard library's `unicode.Version`. Rule GB9c (Indic conjunct clusters via InCB) was introduced in Unicode 15.1 and will be added when the pin is updated.

---

## Maintenance and Table Generation

`tables.go` is generated by `gen/gen.go`.

```bash
# Regenerate tables from the committed local mirror
go generate ./tui/internal/grapheme

# Refresh the mirror from unicode.org and regenerate (annual update)
go run ./gen -unicode 15.0.0 -download
```

The generated file records the SHA-256 hashes of all input files at the top of `tables.go` for auditing.
