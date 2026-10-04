package query

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/yongjohnlee80/golib/errs"
)

// Term is one word of a query, to be matched as itself. Prefix marks the last word of a query
// written with a trailing '*': it matches any word starting with Text.
type Term struct {
	Text   string
	Prefix bool
}

// Terms splits a query into its words. Every word is matched as itself, so no character of a
// full-text dialect's syntax (AND, OR, NEAR, a column filter, parentheses, '-', '^') means anything
// but itself. A '*' ending the last word keeps its meaning, a prefix; '*' ending any other word is
// dropped. A word with no letter or digit is dropped, since a tokenizer would drop all of it.
//
// words are the kept words lowercased, with a leading '#' removed: what a tag is compared with.
func Terms(q string) (terms []Term, words []string) {
	fields := strings.Fields(q)
	for i, f := range fields {
		prefix := i == len(fields)-1 && strings.HasSuffix(f, "*")
		f = strings.TrimRight(f, "*")
		if !strings.ContainsFunc(f, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) {
			continue
		}
		terms = append(terms, Term{Text: f, Prefix: prefix})
		words = append(words, strings.ToLower(strings.TrimPrefix(f, "#")))
	}
	return terms, words
}

// FTS5 renders terms as an SQLite FTS5 match: each word a quoted phrase, quotes doubled inside,
// the prefix term followed by '*'. All the words must match. No terms render as "".
func FTS5(terms []Term) string {
	parts := make([]string, len(terms))
	for i, t := range terms {
		p := `"` + strings.ReplaceAll(t.Text, `"`, `""`) + `"`
		if t.Prefix {
			p += "*"
		}
		parts[i] = p
	}
	return strings.Join(parts, " ")
}

// Fields declares the fields a query may filter on, and reads a filter's value as its field's
// type. A nil Fields declares none.
type Fields interface {
	// Declared reports whether field is one to filter on.
	Declared(field string) bool
	// FacetValue is text read as field's value, in the form stored for it.
	FacetValue(field, text string) (string, error)
}

// ErrUnknownFacet is a filter on a field the Fields do not declare.
var ErrUnknownFacet = errs.Sentinel(errs.ErrInvalidArgument, "search: not a declared field")

// ErrFacetValue is a filter value that is not a value of its field's type.
var ErrFacetValue = errs.Sentinel(errs.ErrInvalidArgument, "search: not a value of the field's type")

// facetWord is a query word that may be a field filter: name:value.
var facetWord = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*):(\S+)$`)

// Facets takes the query's name:value words for declared fields out of q and joins them to the
// given filters, every value read by FacetValue. A name:value word for a field that is not declared
// stays a word, so "re:" and "http://x" search as text. A given filter on a field that is not
// declared is ErrUnknownFacet; a value FacetValue refuses is ErrFacetValue. rest is q without the
// words taken; facets is nil when there are none.
func Facets(q string, given map[string][]string, f Fields) (rest string, facets map[string][]string, err error) {
	out := map[string][]string{}
	add := func(field, text string) error {
		if !declared(f, field) {
			return fmt.Errorf("%w: %q", ErrUnknownFacet, field)
		}
		v, err := f.FacetValue(field, text)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrFacetValue, err)
		}
		if !slices.Contains(out[field], v) {
			out[field] = append(out[field], v)
		}
		return nil
	}
	for field, values := range given {
		if len(values) == 0 {
			return "", nil, fmt.Errorf("%w: %q has no value", ErrFacetValue, field)
		}
		for _, v := range values {
			if err := add(field, v); err != nil {
				return "", nil, err
			}
		}
	}
	var words []string
	for _, w := range strings.Fields(q) {
		m := facetWord.FindStringSubmatch(w)
		if m == nil || !declared(f, m[1]) {
			words = append(words, w)
			continue
		}
		if err := add(m[1], m[2]); err != nil {
			return "", nil, err
		}
	}
	if len(out) == 0 {
		out = nil
	}
	return strings.Join(words, " "), out, nil
}

// declared is f declaring field; a nil f declares nothing.
func declared(f Fields, field string) bool {
	return f != nil && f.Declared(field)
}
