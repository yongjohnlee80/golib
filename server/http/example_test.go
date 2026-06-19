package httpserver_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	httpserver "github.com/yongjohnlee80/golib/server/http"
)

// ExampleServer shows the full lifecycle: build, register a parameterised route,
// bind an ephemeral port, serve, make a request, and shut down via context.
func ExampleServer() {
	srv := httpserver.New(httpserver.Addr("127.0.0.1:0"))
	srv.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = httpserver.JSON(w, http.StatusOK, map[string]string{"id": httpserver.URLParam(r, "id")})
	})

	// Listen binds synchronously, so Addr() is known before we serve.
	if err := srv.Listen(context.Background()); err != nil {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // cancelling Run triggers graceful shutdown
	go func() { _ = srv.Run(ctx) }()

	resp, err := http.Get("http://" + srv.Addr() + "/users/42")
	if err != nil {
		panic(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Print(string(body))
	// Output: {"id":"42"}
}

// ExampleServer_middleware shows global, group, and route middleware composing
// outside-in: global wraps group wraps route wraps the handler.
func ExampleServer_middleware() {
	trace := func(tag string) httpserver.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("X-Trace", tag)
				next.ServeHTTP(w, r)
			})
		}
	}

	srv := httpserver.New(httpserver.Addr("127.0.0.1:0"), httpserver.Middlewares(trace("global")))
	srv.Group("/api", trace("group")).With(trace("route")).Get("/x", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	if err := srv.Listen(context.Background()); err != nil {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Run(ctx) }()

	resp, err := http.Get("http://" + srv.Addr() + "/api/x")
	if err != nil {
		panic(err)
	}
	resp.Body.Close()
	fmt.Println(resp.Header.Values("X-Trace"))
	// Output: [global group route]
}

// ExampleJSON writes a JSON body with a status code.
func ExampleJSON() {
	rec := httptest.NewRecorder()
	_ = httpserver.JSON(rec, http.StatusOK, map[string]string{"hello": "world"})

	fmt.Println(rec.Code)
	fmt.Print(rec.Body.String())
	// Output:
	// 200
	// {"hello":"world"}
}

// ExampleError writes the error envelope, whose JSON schema matches request.Error.
func ExampleError() {
	rec := httptest.NewRecorder()
	httpserver.Error(rec, http.StatusNotFound, "user not found")

	fmt.Print(rec.Body.String())
	// Output: {"status":404,"message":"user not found"}
}

// ExampleDecode reads and validates a JSON request body into a typed value.
func ExampleDecode() {
	type CreateUser struct {
		Name string `json:"name"`
	}
	req := httptest.NewRequest("POST", "/users", strings.NewReader(`{"name":"Ada"}`))

	u, err := httpserver.Decode[CreateUser](req, 0) // 0 -> DefaultMaxBodyBytes
	fmt.Println(u.Name, err)
	// Output: Ada <nil>
}

// ExampleHandler shows the error-returning handler adapter: a *StatusError becomes
// its status + message; any other error becomes a 500.
func ExampleHandler() {
	h := httpserver.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if r.URL.Query().Get("id") == "" {
			return httpserver.Status(http.StatusBadRequest, "missing id")
		}
		return httpserver.JSON(w, http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/users", nil))

	fmt.Println(rec.Code)
	fmt.Print(rec.Body.String())
	// Output:
	// 400
	// {"status":400,"message":"missing id"}
}

// ExampleMockServer shows the loopback test double: stub a route, call it over a
// real client, and inspect what was recorded.
func ExampleMockServer() {
	mock := httpserver.NewMock()
	defer mock.Close()
	mock.Stub("GET", "/health", http.StatusOK, map[string]string{"status": "up"})

	resp, err := mock.Client().Get(mock.URL() + "/health")
	if err != nil {
		panic(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	fmt.Print(string(body))
	fmt.Println("recorded:", len(mock.Recorded()))
	// Output:
	// {"status":"up"}
	// recorded: 1
}
