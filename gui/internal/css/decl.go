// Package css holds the CSS reading gui's HTML view and SVG reader share: splitting outside
// parentheses, and a declaration block's declarations.
package css

import "strings"

// Decl is one declaration: its property, lower-cased, its value, and whether it is !important.
type Decl struct {
	Prop, Value string
	Important   bool
}

// Decls reads "prop: value; …" (a rule's body, or a style attribute). A ';' inside parentheses,
// as in url(data:image/png;base64,…), separates nothing. A declaration with no colon, an empty
// property or an empty value is skipped.
func Decls(s string) []Decl {
	var out []Decl
	for _, d := range SplitTop(s, ';') {
		prop, value, ok := strings.Cut(d, ":")
		if !ok {
			continue
		}
		prop = strings.ToLower(strings.TrimSpace(prop))
		value = strings.TrimSpace(value)
		imp := false
		if i := strings.LastIndex(strings.ToLower(value), "!important"); i >= 0 {
			value, imp = strings.TrimSpace(value[:i]), true
		}
		if prop != "" && value != "" {
			out = append(out, Decl{Prop: prop, Value: value, Important: imp})
		}
	}
	return out
}

// SplitTop splits s at sep outside parentheses.
func SplitTop(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth = max(depth-1, 0)
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}
