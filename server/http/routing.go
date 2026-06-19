package httpserver

import (
	"net/http"
	"strings"

	"github.com/yongjohnlee80/golib/server"
)

// rcKey is the private context key under which dispatch stashes the matched
// *server.RouteContext, retrieved by URLParam.
type rcKey struct{}

// URLParam returns the named path parameter captured for the current route, or
// "" if absent (e.g. r came from outside this server).
func URLParam(r *http.Request, name string) string {
	if rc, ok := r.Context().Value(rcKey{}).(*server.RouteContext); ok {
		return rc.Param(name)
	}
	return ""
}

// stdMethods is the set Mount forwards to a sub-handler.
var stdMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodHead, http.MethodOptions,
}

// Group is a routing scope: a path prefix plus an (immutable) middleware chain
// baked into every handler registered through it. Server's own route methods
// delegate to an implicit root group (empty prefix, empty chain).
type Group struct {
	s      *Server
	prefix string
	chain  *server.Chain[http.Handler]
}

func (s *Server) rootGroup() *Group {
	return &Group{s: s, prefix: "", chain: server.NewChain[http.Handler]()}
}

// Group opens a sub-scope under prefix, layering mw onto the current chain.
// The chain is copy-on-write, so sibling groups never alias each other.
func (s *Server) Group(prefix string, mw ...Middleware) *Group {
	return s.rootGroup().Group(prefix, mw...)
}

// With returns a route scope with extra middleware but the same prefix.
func (s *Server) With(mw ...Middleware) *Group { return s.rootGroup().With(mw...) }

// Group nests a further prefix + middleware under g.
func (g *Group) Group(prefix string, mw ...Middleware) *Group {
	return &Group{s: g.s, prefix: g.prefix + prefix, chain: g.chain.Use(mw...)}
}

// With layers extra middleware onto g without changing the prefix.
func (g *Group) With(mw ...Middleware) *Group {
	return &Group{s: g.s, prefix: g.prefix, chain: g.chain.Use(mw...)}
}

// register bakes the group chain into h and registers it. A bad pattern or a
// duplicate route is a programmer error at startup, so it panics (per ADR-0002).
func (g *Group) register(method, pattern string, h http.Handler) {
	wrapped := g.chain.Then(h)
	if err := g.s.router.Handle(method, g.prefix+pattern, wrapped); err != nil {
		panic("httpserver: route registration failed for " + method + " " + g.prefix + pattern + ": " + err.Error())
	}
}

// Handle registers h for a "METHOD /path" pattern (e.g. "GET /users/{id}").
func (g *Group) Handle(pattern string, h http.Handler) {
	method, path := splitMethodPattern(pattern)
	g.register(method, path, h)
}

// HandleFunc is Handle for an http.HandlerFunc value (a convenience overload).
func (g *Group) HandleFunc(pattern string, h http.HandlerFunc) { g.Handle(pattern, h) }

// Get registers h for GET pattern within this group's prefix and middleware.
func (g *Group) Get(pattern string, h http.HandlerFunc) { g.register(http.MethodGet, pattern, h) }

// Post registers h for POST pattern within this group's prefix and middleware.
func (g *Group) Post(pattern string, h http.HandlerFunc) { g.register(http.MethodPost, pattern, h) }

// Put registers h for PUT pattern within this group's prefix and middleware.
func (g *Group) Put(pattern string, h http.HandlerFunc) { g.register(http.MethodPut, pattern, h) }

// Patch registers h for PATCH pattern within this group's prefix and middleware.
func (g *Group) Patch(pattern string, h http.HandlerFunc) { g.register(http.MethodPatch, pattern, h) }

// Delete registers h for DELETE pattern within this group's prefix and middleware.
func (g *Group) Delete(pattern string, h http.HandlerFunc) { g.register(http.MethodDelete, pattern, h) }

// Head registers h for HEAD pattern. HEAD is explicit-only — registering GET does
// not implicitly answer HEAD.
func (g *Group) Head(pattern string, h http.HandlerFunc) { g.register(http.MethodHead, pattern, h) }

// Options registers h for OPTIONS pattern within this group's prefix and middleware.
func (g *Group) Options(pattern string, h http.HandlerFunc) {
	g.register(http.MethodOptions, pattern, h)
}

// Mount attaches a sub-handler (e.g. another mux) under prefix for all standard
// methods. The mounted handler sees the path with the mount prefix stripped.
func (g *Group) Mount(prefix string, h http.Handler) {
	full := g.prefix + prefix
	stripped := http.StripPrefix(strings.TrimRight(full, "/"), h)
	for _, m := range stdMethods {
		g.register(m, prefix+"/{mount...}", stripped)
	}
}

// --- Server convenience delegates to the root group ------------------------
//
// These let callers register top-level routes without first opening a Group. Each
// delegates to an implicit root group (empty prefix, empty chain), so only the
// global middleware applies. They share the registration semantics of the Group
// methods of the same name, including the panic-on-bad/duplicate-pattern policy.

// Handle registers h at a top-level "METHOD /path" pattern.
func (s *Server) Handle(pattern string, h http.Handler) { s.rootGroup().Handle(pattern, h) }

// HandleFunc registers an http.HandlerFunc at a top-level "METHOD /path" pattern.
func (s *Server) HandleFunc(pattern string, h http.HandlerFunc) { s.rootGroup().HandleFunc(pattern, h) }

// Get registers a top-level GET route.
func (s *Server) Get(pattern string, h http.HandlerFunc) { s.rootGroup().Get(pattern, h) }

// Post registers a top-level POST route.
func (s *Server) Post(pattern string, h http.HandlerFunc) { s.rootGroup().Post(pattern, h) }

// Put registers a top-level PUT route.
func (s *Server) Put(pattern string, h http.HandlerFunc) { s.rootGroup().Put(pattern, h) }

// Patch registers a top-level PATCH route.
func (s *Server) Patch(pattern string, h http.HandlerFunc) { s.rootGroup().Patch(pattern, h) }

// Delete registers a top-level DELETE route.
func (s *Server) Delete(pattern string, h http.HandlerFunc) { s.rootGroup().Delete(pattern, h) }

// Head registers a top-level HEAD route (explicit-only).
func (s *Server) Head(pattern string, h http.HandlerFunc) { s.rootGroup().Head(pattern, h) }

// Options registers a top-level OPTIONS route.
func (s *Server) Options(pattern string, h http.HandlerFunc) { s.rootGroup().Options(pattern, h) }

// Mount attaches a sub-handler under a top-level prefix (see Group.Mount).
func (s *Server) Mount(prefix string, h http.Handler) { s.rootGroup().Mount(prefix, h) }

// splitMethodPattern parses "METHOD /path" (chi-style). A pattern with no leading
// method token is treated as a GET (defensive; explicit methods are preferred).
func splitMethodPattern(pattern string) (method, path string) {
	pattern = strings.TrimSpace(pattern)
	if i := strings.IndexByte(pattern, ' '); i > 0 {
		return pattern[:i], strings.TrimSpace(pattern[i+1:])
	}
	return http.MethodGet, pattern
}
