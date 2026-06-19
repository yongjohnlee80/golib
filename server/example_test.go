package server_test

import (
	"fmt"

	"github.com/yongjohnlee80/golib/server"
)

// ExampleRouter shows static, parameter, and wildcard routing, plus the 404/405
// distinction carried by MatchResult.
func ExampleRouter() {
	// H = string keeps the example transport-agnostic; a real HTTP app would use
	// server.NewRouter[http.Handler]().
	r := server.NewRouter[string]()
	_ = r.Handle("GET", "/users/{id}", "getUser")
	_ = r.Handle("GET", "/files/{path...}", "serveFile")

	// Parameter capture.
	res := r.Match("GET", "/users/42")
	fmt.Println(res.Handler, res.Context.Param("id"))

	// Trailing wildcard captures the remainder of the path.
	res = r.Match("GET", "/files/img/logo.png")
	fmt.Println(res.Handler, res.Context.Param("path"))

	// Path matches but the method does not: a 405, with AllowedMethods for the
	// Allow header.
	res = r.Match("POST", "/users/42")
	fmt.Println(res.Found, res.MethodAllowed, res.AllowedMethods)

	// Nothing matches the path: a 404.
	res = r.Match("GET", "/missing")
	fmt.Println(res.Found)

	// Output:
	// getUser 42
	// serveFile img/logo.png
	// true false [GET]
	// false
}

// ExampleRouter_group shows prefix composition via nested Groups.
func ExampleRouter_group() {
	r := server.NewRouter[string]()
	v1 := r.Group("/api").Group("/v1")
	_ = v1.Handle("GET", "/ping", "pong")

	fmt.Println(r.Match("GET", "/api/v1/ping").Handler)
	// Output: pong
}

// ExampleChain shows the immutable middleware builder. The first middleware added
// is the outermost wrapper.
func ExampleChain() {
	wrap := func(tag string) server.Middleware[string] {
		return func(next string) string { return tag + "(" + next + ")" }
	}

	// Deriving from a shared base never mutates it (copy-on-write).
	base := server.NewChain[string](wrap("log"))
	withAuth := base.Use(wrap("auth"))

	fmt.Println(base.Then("handler"))     // only log
	fmt.Println(withAuth.Then("handler")) // log wraps auth wraps handler
	// Output:
	// log(handler)
	// log(auth(handler))
}

// ExampleRouteContext shows reading captured path parameters from a match.
func ExampleRouteContext() {
	r := server.NewRouter[string]()
	_ = r.Handle("GET", "/u/{id}/posts/{slug}", "h")

	rc := r.Match("GET", "/u/7/posts/hello-world").Context
	fmt.Println(rc.Param("id"), rc.Param("slug"))
	// Output: 7 hello-world
}
