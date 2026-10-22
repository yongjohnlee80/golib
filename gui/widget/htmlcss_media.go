package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/internal/css"
)

// MEDIA QUERIES — which @media blocks apply to the view a sheet is read for.

// mediaEnv is what a media query is asked about: the view's size in pixels and its theme.
type mediaEnv struct {
	w, h float32
	dark bool
}

// mediaMatches evaluates a media query list: true when any query in it matches env. A query is
// [not|only] [all|screen|print|…] and (feature: value) conditions; the features read are min- and
// max-width and -height, width, height, orientation and prefers-color-scheme. Any other feature,
// and print, never matches.
func mediaMatches(list string, env mediaEnv) bool {
	for _, q := range css.SplitTop(list, ',') {
		if mediaQuery(strings.TrimSpace(q), env) {
			return true
		}
	}
	return false
}

// mediaQuery evaluates a single media query against the provided media environment.
func mediaQuery(q string, env mediaEnv) bool {
	if q == "" {
		return true
	}
	not := false
	if rest, ok := strings.CutPrefix(q, "not "); ok {
		not, q = true, rest
	} else if rest, ok := strings.CutPrefix(q, "only "); ok {
		q = rest
	}
	ok := true
	for i, part := range splitAnd(q) {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "("):
			ok = ok && mediaFeature(strings.Trim(part, "() "), env)
		case i == 0 && (part == "all" || part == "screen"):
		default:
			ok = false // print, or a type the view is not
		}
	}
	return ok != not
}

// splitAnd splits a query at its top-level "and"s.
func splitAnd(q string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(q); i++ {
		switch q[i] {
		case '(':
			depth++
		case ')':
			depth = max(depth-1, 0)
		case 'a':
			if depth == 0 && strings.HasPrefix(q[i:], "and") && (i == 0 || q[i-1] == ' ') && (i+3 == len(q) || q[i+3] == ' ' || q[i+3] == '(') {
				out = append(out, q[start:i])
				start = i + 3
				i += 2
			}
		}
	}
	return append(out, q[start:])
}

// mediaFeature evaluates one "name: value" (or a bare name) against env.
func mediaFeature(f string, env mediaEnv) bool {
	name, value, _ := strings.Cut(f, ":")
	name, value = strings.TrimSpace(name), strings.TrimSpace(value)
	px := func() (float32, bool) {
		l, ok := parseLen(value)
		if !ok || l.unit == '%' || l.unit == 'a' {
			return 0, false
		}
		return l.px(16, 16, 0, gui.Size{W: env.w, H: env.h}), true // em in a query is the initial font size
	}
	switch name {
	case "min-width", "max-width", "width", "min-height", "max-height", "height":
		v, ok := px()
		if !ok {
			return false
		}
		have := env.w
		if strings.HasSuffix(name, "height") {
			have = env.h
		}
		switch {
		case strings.HasPrefix(name, "min-"):
			return have >= v
		case strings.HasPrefix(name, "max-"):
			return have <= v
		}
		return have == v
	case "orientation":
		return value == "portrait" && env.h >= env.w || value == "landscape" && env.w > env.h
	case "prefers-color-scheme":
		return value == "dark" && env.dark || value == "light" && !env.dark
	}
	return false
}
