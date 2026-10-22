package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/gui/internal/css"
)

// THE STYLESHEET READER — a sheet's text as rules and declarations, for the cascade (htmlcss.go).

// parseCSS reads a stylesheet's rules: an @media block's when its query matches env; any other
// @-rule is skipped with its block.
func parseCSS(src string, order *int, env mediaEnv) []cssRule {
	src = stripComments(src)
	var rules []cssRule
	for i := 0; i < len(src); {
		open := strings.IndexByte(src[i:], '{')
		if open < 0 {
			break
		}
		head := strings.TrimSpace(src[i : i+open])
		body, next := block(src, i+open)
		i = next
		if strings.HasPrefix(head, "@") {
			if q, ok := strings.CutPrefix(strings.ToLower(head), "@media"); ok && mediaMatches(q, env) {
				rules = append(rules, parseCSS(body, order, env)...)
			}
			continue // @font-face, @supports and the like: a page that needs them still shows its text
		}
		var sels []cssSelector
		for _, s := range strings.Split(head, ",") {
			if sel, ok := parseSelector(strings.TrimSpace(s)); ok {
				sels = append(sels, sel)
			}
		}
		if len(sels) == 0 {
			continue
		}
		*order++
		rules = append(rules, cssRule{sels: sels, decls: parseDecls(body), order: *order})
	}
	return rules
}

// block is the text inside the braces opening at src[open], and the index past them.
func block(src string, open int) (string, int) {
	depth := 0
	for j := open; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open+1 : j], j + 1
			}
		}
	}
	return src[open+1:], len(src)
}

// stripComments removes CSS /* ... */ block comments from raw CSS text.
func stripComments(s string) string {
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i+2:], "*/")
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + s[i+2+j+2:]
	}
}

// parseDecls reads "prop: value; …" (a rule's body, or a style attribute): gui's one CSS
// declaration reader (internal/css), shared with the SVG reader.
func parseDecls(s string) []cssDecl {
	var out []cssDecl
	for _, d := range css.Decls(s) {
		out = append(out, cssDecl{prop: d.Prop, value: d.Value, important: d.Important})
	}
	return out
}
