package server

import (
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/errs"
)

// MatchResult reports the outcome of [Router.Match].
//
//	Found=false                       -> no path matched (HTTP: 404)
//	Found=true, MethodAllowed=false   -> path matched, method not registered (HTTP:
//	                                     405; AllowedMethods drives the Allow header)
//	Found=true, MethodAllowed=true    -> Handler and Context are valid
type MatchResult[H any] struct {
	// Handler is the resolved handler. It is only valid when MethodAllowed is true;
	// on a 404 or 405 it holds the zero value of H.
	Handler H
	// Context holds the path parameters captured for this match, or nil when the
	// matched route is fully static (no {param}/{wildcard} segments).
	Context *RouteContext
	// Found reports whether any registered route matched the path, regardless of
	// method. Found == false is the 404 signal.
	Found bool
	// MethodAllowed reports whether the matched path also has a handler for the
	// requested method. Found == true with MethodAllowed == false is the 405 signal.
	MethodAllowed bool
	// AllowedMethods is the sorted set of methods registered on the matched path. It
	// is populated whenever Found is true and is intended to drive the Allow header
	// on a 405 response.
	AllowedMethods []string
}

// Router is a transport-agnostic, tree-based router generic over the handler type
// H. Build it (Handle/Group), then Match concurrently.
type Router[H any] struct {
	root   *node[H]
	prefix string
}

// node is a single segment in the routing tree. Each node may simultaneously hold
// static children (keyed by exact segment text), one parameter child ({name}), and
// one trailing-wildcard child ({name...}). handlers maps HTTP method -> handler for
// routes that terminate at this node; a node with no handlers is purely structural.
type node[H any] struct {
	static   map[string]*node[H] // exact-match children, keyed by segment text
	param    *node[H]            // single "{name}" child, if any
	wild     *node[H]            // single trailing "{name...}" child, if any
	pname    string              // this node's own capture name (set on param/wild nodes)
	handlers map[string]H        // method -> handler for routes ending here
}

// NewRouter creates an empty router.
func NewRouter[H any]() *Router[H] { return &Router[H]{root: &node[H]{}} }

// Group returns a sub-router that registers under prefix on the same tree.
// Nestable: g.Group("/v2") composes prefixes.
func (r *Router[H]) Group(prefix string) *Router[H] {
	return &Router[H]{root: r.root, prefix: r.prefix + prefix}
}

// Handle registers h for method+pattern (pattern is relative to any Group prefix).
// Segments: static ("users"), param ("{id}"), trailing wildcard ("{rest...}", which
// must be the last segment). It returns an error on an empty method, a non-final
// wildcard, or a duplicate route.
func (r *Router[H]) Handle(method, pattern string, h H) error {
	if method == "" {
		return errs.Wrap(errs.ErrInvalidArgument, "server: empty method for pattern %q", pattern)
	}
	segs := splitSegments(r.prefix + pattern)
	cur := r.root
	for i, seg := range segs {
		switch {
		case isWildcard(seg):
			if i != len(segs)-1 {
				return errs.Wrap(errs.ErrInvalidArgument, "server: wildcard %q must be the last segment in %q", seg, pattern)
			}
			if cur.wild == nil {
				cur.wild = &node[H]{pname: captureName(seg)}
			}
			cur = cur.wild
		case isParam(seg):
			if cur.param == nil {
				cur.param = &node[H]{pname: captureName(seg)}
			}
			cur = cur.param
		default:
			if cur.static == nil {
				cur.static = map[string]*node[H]{}
			}
			ch := cur.static[seg]
			if ch == nil {
				ch = &node[H]{}
				cur.static[seg] = ch
			}
			cur = ch
		}
	}
	if cur.handlers == nil {
		cur.handlers = map[string]H{}
	}
	if _, dup := cur.handlers[method]; dup {
		return errs.Wrap(errs.ErrInvalidArgument, "server: duplicate route %s %q", method, pattern)
	}
	cur.handlers[method] = h
	return nil
}

// Match resolves method+path. See [MatchResult].
func (r *Router[H]) Match(method, path string) MatchResult[H] {
	var res MatchResult[H]
	leaf, caps := matchNode(r.root, splitSegments(path))
	if leaf == nil {
		return res // Found == false
	}
	res.Found = true
	if len(caps) > 0 {
		m := make(map[string]string, len(caps))
		for _, c := range caps {
			m[c.name] = c.value
		}
		res.Context = &RouteContext{params: m}
	}
	if h, ok := leaf.handlers[method]; ok {
		res.Handler = h
		res.MethodAllowed = true
	}
	res.AllowedMethods = sortedKeys(leaf.handlers)
	return res
}

// capture is a single path-parameter binding (name -> value) accumulated while a
// match descends the tree. They are collected only on the winning path.
type capture struct{ name, value string }

// matchNode descends preferring static > param > wildcard, building captures only
// along the successful path (so abandoned branches never leak params).
func matchNode[H any](n *node[H], segs []string) (*node[H], []capture) {
	if len(segs) == 0 {
		if len(n.handlers) > 0 {
			return n, nil
		}
		return nil, nil
	}
	seg, rest := segs[0], segs[1:]
	if n.static != nil {
		if ch := n.static[seg]; ch != nil {
			if leaf, caps := matchNode(ch, rest); leaf != nil {
				return leaf, caps
			}
		}
	}
	if n.param != nil {
		if leaf, caps := matchNode(n.param, rest); leaf != nil {
			return leaf, append([]capture{{n.param.pname, seg}}, caps...)
		}
	}
	if n.wild != nil && len(n.wild.handlers) > 0 {
		return n.wild, []capture{{n.wild.pname, strings.Join(segs, "/")}}
	}
	return nil, nil
}

// splitSegments splits a path/pattern into non-empty segments, dropping any query
// or fragment.
func splitSegments(p string) []string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// isParam reports whether seg is a capture segment, i.e. wrapped in braces such as
// "{id}" or "{rest...}".
func isParam(seg string) bool {
	return len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}'
}

// isWildcard reports whether seg is a trailing catch-all capture, i.e. "{name...}".
// A wildcard is only legal as the final segment of a pattern.
func isWildcard(seg string) bool {
	return isParam(seg) && strings.HasSuffix(seg, "...}")
}

// captureName returns the parameter name of a "{name}" or "{name...}" segment.
func captureName(seg string) string {
	inner := seg[1 : len(seg)-1]
	return strings.TrimSuffix(inner, "...")
}

// sortedKeys returns m's keys in ascending order, giving deterministic output for
// the Allow header (and stable tests).
func sortedKeys[H any](m map[string]H) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
