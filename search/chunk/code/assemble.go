package code

import (
	"strings"

	"github.com/yongjohnlee80/golib/search"
	"github.com/yongjohnlee80/golib/search/chunk"
)

// unit is one declaration a language's extractor found: its span in the source, the breadcrumb
// and embedding text of a whole unit, and what an oversized unit needs to split.
type unit struct {
	start, end int
	crumb      string
	// embed is the unit's embedding text when it differs from breadcrumb + body ("" otherwise).
	embed string
	// sig is the declaration's signature on one line: every fragment of a split unit keeps it in
	// its breadcrumb, so a fragment found alone still says which function it belongs to.
	sig string
	// tail is the end of embed a bound keeps whole, cutting what comes before it: a function's
	// signature, after a doc that may run long.
	tail string
	// cuts are offsets inside the span where a fragment may begin (a statement's first byte),
	// ascending. A unit with none is never split.
	cuts []int
	// fragEmbed is the embedding text of a fragment after the first, from its span.
	fragEmbed func(start, end int) string
}

// assemble turns ascending, disjoint units into chunks that partition src: every non-whitespace
// byte outside the units becomes a header chunk (before the first unit) or a glue chunk (after it),
// and every chunk's Body is exactly src[ByteStart:ByteEnd]. A unit that overlaps the one before it
// is dropped, so a heuristic extractor's mistake costs structure, never the partition.
func assemble(src []byte, units []unit, headerCrumb, glueCrumb string, limit int) []search.Chunk {
	if limit <= 0 {
		limit = chunk.DefaultTokens
	}
	var out []search.Chunk
	emit := func(start, end int, crumb, embed string) {
		out = append(out, search.Chunk{
			Ord: len(out), Breadcrumb: crumb, Body: string(src[start:end]), Embed: embed,
			ByteStart: start, ByteEnd: end,
		})
	}
	gap := func(start, end int, crumb string) {
		start, end = trimSpan(src, start, end)
		if start >= end {
			return
		}
		for _, s := range splitLines(src, start, end, budgetFor(crumb, limit)) {
			emit(s[0], s[1], crumb, "")
		}
	}
	pos := 0
	for _, u := range units {
		if u.start < pos || u.end <= u.start || u.end > len(src) {
			continue
		}
		crumb := glueCrumb
		if pos == 0 {
			crumb = headerCrumb
		}
		gap(pos, u.start, crumb)
		emitUnit(src, u, limit, emit)
		pos = u.end
	}
	crumb := glueCrumb
	if pos == 0 {
		crumb = headerCrumb
	}
	gap(pos, len(src), crumb)
	if len(out) == 0 {
		emit(0, 0, headerCrumb, "")
	}
	return out
}

// emitUnit emits a unit whole, or, when it is over the budget and can be cut, as fragments that
// partition its span: the first keeps the unit's breadcrumb and embedding (doc and signature), and
// every fragment's breadcrumb carries the signature. A later fragment of a function embeds what its
// fragEmbed gives, or the function's doc and signature again; a fragment of a declaration (a long
// type, a table), which has no signature, embeds its breadcrumb and its own first lines, never the
// whole declaration again. No chunk's embedding is larger than the budget its body is held to.
func emitUnit(src []byte, u unit, limit int, emit func(start, end int, crumb, embed string)) {
	start, end := trimSpan(src, u.start, u.end)
	budget := budgetFor(u.crumb, limit)
	if chunk.Tokens(src[start:end]) <= budget {
		emit(start, end, u.crumb, boundTail(u.embed, u.tail, budget))
		return
	}
	cuts := u.cuts
	if len(cuts) == 0 {
		// no statements to cut at (a long type, a fields-only class): cut at lines, so no chunk
		// runs past the budget
		for i := start; i < end-1; i++ {
			if src[i] == '\n' {
				cuts = append(cuts, nextNonSpace(src, i+1))
			}
		}
	}
	crumb := u.crumb
	if u.sig != "" {
		crumb = u.crumb + " > " + u.sig
	}
	budget = budgetFor(crumb, limit)
	frags := pack(src, start, end, cuts, budget)
	for i, f := range frags {
		embed := boundTail(u.embed, u.tail, budget)
		switch {
		case i == 0:
		case u.fragEmbed != nil:
			embed = boundEmbed(u.fragEmbed(f[0], f[1]), budget)
		case u.sig == "" && u.embed != "":
			// a declaration's fragment: what it holds, not the declaration again
			embed = boundEmbed(joinNonEmpty(crumb, preview(src, f[0], f[1])), budget)
		}
		// a function cut at lines, having no statements to cut at, keeps its doc and signature; a
		// unit embedding its body (a header) embeds each fragment's body, which the budget bounds
		emit(f[0], f[1], crumb, embed)
	}
}

// previewLines is how many of a declaration fragment's lines its embedding keeps: enough to say
// what the fragment holds (a table's first rows, a type's first fields) without the rest.
const previewLines = 3

// preview is the first lines of src[start:end] that are not blank, each on one line, and an
// elision mark when more follow.
func preview(src []byte, start, end int) string {
	var keep []string
	for _, l := range strings.Split(string(src[start:end]), "\n") {
		if l = oneLine(l); l == "" {
			continue
		}
		if len(keep) == previewLines {
			keep = append(keep, elided)
			break
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}

// elided marks text an embedding leaves out.
const elided = "…"

// boundTail is boundEmbed keeping tail, the end of text: what comes before it (the breadcrumb, a
// long doc) is cut instead, so a doc never costs a function its signature. With no room before
// the tail, the tail is the whole embedding. A tail larger than the budget is cut at a word, the
// last resort, and never gives way to the text before it. A whole unit's tail always fits, being
// part of a body the budget holds; a fragment's budget is smaller by the signature its breadcrumb
// carries, so at a small limit a fragment's can not.
func boundTail(text, tail string, budget int) string {
	if tail == "" || !strings.HasSuffix(text, tail) || chunk.Tokens([]byte(text)) <= budget {
		return boundEmbed(text, budget)
	}
	head := strings.TrimRight(strings.TrimSuffix(text, tail), "\n")
	for hb := budget - chunk.Tokens([]byte(tail)) - 1; hb > 1; hb-- {
		if out := boundEmbed(head, hb) + "\n" + tail; chunk.Tokens([]byte(out)) <= budget {
			return out
		}
	}
	return boundEmbed(tail, budget)
}

// boundEmbed holds an embedding text to budget estimated tokens: whole when it fits, else its
// leading lines (the breadcrumb, the doc, the start of a signature) and an elision mark, a line
// too long by itself cut at a word. "" stays "" (the chunk embeds its breadcrumb and body).
func boundEmbed(text string, budget int) string {
	if text == "" || chunk.Tokens([]byte(text)) <= budget {
		return text
	}
	fits := func(s string) bool { return chunk.Tokens([]byte(s)) <= budget }
	var out string
	for _, l := range strings.Split(text, "\n") {
		next := l
		if out != "" {
			next = out + "\n" + l
		}
		if fits(next + "\n" + elided) {
			out = next
			continue
		}
		// the line does not fit whole: keep as many of its words as do
		words := strings.Fields(l)
		lo, hi := 0, len(words)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			cand := strings.Join(words[:mid], " ")
			if out != "" {
				cand = out + "\n" + cand
			}
			if fits(cand + " " + elided) {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		if lo > 0 {
			part := strings.Join(words[:lo], " ")
			if out != "" {
				part = out + "\n" + part
			}
			return part + " " + elided
		}
		break
	}
	if out == "" {
		return elided
	}
	return out + "\n" + elided
}

// pack groups the segments the cuts make into fragments of at most budget estimated tokens (a
// segment alone over it is a fragment of its own). Fragments are trimmed of the whitespace at their
// ends, so only whitespace lies between them.
func pack(src []byte, start, end int, cuts []int, budget int) [][2]int {
	bounds := []int{start}
	for _, c := range cuts {
		if c > bounds[len(bounds)-1] && c < end {
			bounds = append(bounds, c)
		}
	}
	bounds = append(bounds, end)
	var out [][2]int
	fs := bounds[0]
	for i := 1; i < len(bounds); i++ {
		// close the fragment before segment i when adding it would pass the budget
		if i+1 < len(bounds) {
			s, e := trimSpan(src, fs, bounds[i+1])
			if chunk.Tokens(src[s:e]) > budget && bounds[i] > fs {
				fe := bounds[i]
				a, b := trimSpan(src, fs, fe)
				if a < b {
					out = append(out, [2]int{a, b})
				}
				fs = bounds[i]
			}
		}
	}
	a, b := trimSpan(src, fs, end)
	if a < b {
		out = append(out, [2]int{a, b})
	}
	return out
}

// splitLines cuts a span over the budget at line ends, so header and glue chunks stay bounded too.
func splitLines(src []byte, start, end, budget int) [][2]int {
	if chunk.Tokens(src[start:end]) <= budget {
		return [][2]int{{start, end}}
	}
	var cuts []int
	for i := start; i < end-1; i++ {
		if src[i] == '\n' {
			cuts = append(cuts, i+1)
		}
	}
	return pack(src, start, end, cuts, budget)
}

// budgetFor is the tokens left for a body under crumb.
func budgetFor(crumb string, limit int) int {
	return max(1, limit-chunk.Tokens([]byte(crumb+"\n")))
}

// trimSpan narrows [start, end) past the whitespace at both ends.
func trimSpan(src []byte, start, end int) (int, int) {
	for start < end && isSpace(src[start]) {
		start++
	}
	for end > start && isSpace(src[end-1]) {
		end--
	}
	return start, end
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

// oneLine collapses s's whitespace runs to single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// joinNonEmpty joins the parts that are not blank with newlines.
func joinNonEmpty(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			keep = append(keep, strings.TrimSpace(p))
		}
	}
	return strings.Join(keep, "\n")
}
