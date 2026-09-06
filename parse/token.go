package parse

import "strconv"

// Token is one lexical unit: a Kind and a half-open byte span. It is INERT — it
// cannot answer for its own bytes and does not pretend to. The bytes come from
// Scan.Acquire, which hands back a lifetime along with them; the line and column
// come from Source, which resolves them on demand from a line index rather than
// charging every token for a diagnostic most of them never need.
//
// The span is two int64 offsets, not two Positions, for two reasons:
//
//   - size. Token{Kind, int64, int64} is 24 bytes against 56 for a pair of
//     Positions — 24 MiB per million tokens against 56 MiB — and a streaming
//     lexer whose whole point is O(1) per token should not carry a per-token
//     newline scan it has not been asked for.
//   - collision. parse.Position already exists in this package with an int
//     Offset. Storing it here would either truncate that offset on a 32-bit
//     build or force a breaking change to a type kept for now, so the span is
//     int64 throughout and line/column is a separate, non-truncating Location.
type Token struct {
	Kind  Kind
	Start int64 // offset of the first byte of the token
	End   int64 // one past the last byte — half-open, so End-Start is the length
}

// Len is the token's length in bytes.
func (t Token) Len() int64 { return t.End - t.Start }

// String renders the token as kind and half-open span, for diagnostics and test
// failure messages. It is deliberately not the token's text: a Token does not
// hold its bytes.
func (t Token) String() string {
	return t.Kind.String() + "[" +
		strconv.FormatInt(t.Start, 10) + ":" +
		strconv.FormatInt(t.End, 10) + ")"
}
