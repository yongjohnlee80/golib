// Command fixture serves a tui/web instance for the browser matrix.
//
// It is a REAL tui/web server, not a mock: the whole point of ADR-0009 §2.9's
// matrix is that synthetic dispatch hides engine divergences, and a fake server
// would hide them just as effectively.
//
// The component tree is a bare text sink that records every tui.Event it sees and
// exposes the log over HTTP, so a browser test can assert the exact event stream
// an interaction produced — which is the thing a Go test cannot observe.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/token"
	"github.com/yongjohnlee80/golib/server"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/web"
)

const addr = "127.0.0.1:8081"

func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}
}

// recorder is the component under observation: it accepts every event and keeps
// an ordered log.
type recorder struct {
	mu     sync.Mutex
	events []logged
}

type logged struct {
	Kind    string `json:"kind"`
	Code    int    `json:"code,omitempty"`
	Text    string `json:"text,omitempty"`
	Mods    uint8  `json:"mods,omitempty"`
	KeyKind uint8  `json:"keyKind,omitempty"`
	W       int    `json:"w,omitempty"`
	H       int    `json:"h,omitempty"`
	Gained  bool   `json:"gained,omitempty"`
}

func (r *recorder) Init(*tui.Context)                 {}
func (r *recorder) Layout(c tui.Constraints) tui.Size { return tui.Size{W: c.MaxW, H: c.MaxH} }
func (r *recorder) Render(tui.Surface)                {}

func (r *recorder) HandleEvent(ev tui.Event) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch e := ev.(type) {
	case tui.KeyEvent:
		r.events = append(r.events, logged{
			Kind: "key", Code: int(e.Code), Text: e.Text,
			Mods: uint8(e.Mods), KeyKind: uint8(e.Kind),
		})
	case tui.PasteEvent:
		r.events = append(r.events, logged{Kind: "paste", Text: e.Text})
	case tui.ResizeEvent:
		r.events = append(r.events, logged{Kind: "resize", W: e.W, H: e.H})
	case tui.FocusEvent:
		r.events = append(r.events, logged{Kind: "focus", Gained: e.Gained})
	case tui.MouseEvent:
		r.events = append(r.events, logged{Kind: "mouse", W: e.X, H: e.Y, Mods: uint8(e.Mods)})
	default:
		return false
	}
	return true
}

func (r *recorder) snapshot() []logged {
	r.mu.Lock()
	defer r.mu.Unlock()
	// A non-nil empty slice, so an empty log encodes as [] rather than null. A
	// test asserting "no events were emitted" must be able to call .filter on
	// the result — and "emits nothing" is precisely what several §2.9 cases
	// check, so null there turns a passing property into a TypeError.
	out := make([]logged, 0, len(r.events))
	return append(out, r.events...)
}

func (r *recorder) reset() {
	r.mu.Lock()
	r.events = nil
	r.mu.Unlock()
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := token.NewMemStore(256)
	policy, err := web.RecommendedPolicy([]auth.Factor{token.NewFactor(store)})
	if err != nil {
		return err
	}
	issuer := token.NewIssuer(store)

	rec := &recorder{}
	mgr, err := web.NewManager(func(b *web.Backend) web.Runner {
		return tui.NewApp(rec, tui.WithBackend(b))
	}, web.MaxSessions(16))
	if err != nil {
		return err
	}

	h, err := web.NewHandler(web.Config{
		Addr:           addr,
		Policy:         policy,
		AllowedOrigins: []string{"http://" + addr},
		ExpectedHost:   addr,
	}, mgr, web.Title("browsertest"))
	if err != nil {
		return err
	}

	// The control plane the tests drive. Deliberately separate from the WebTUI
	// server, on its own mux, so it cannot be mistaken for part of the product
	// surface or accidentally shipped.
	control := http.NewServeMux()
	control.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	control.HandleFunc("/_ticket", func(w http.ResponseWriter, _ *http.Request) {
		secret, err := issuer.Issue("browsertest", time.Minute, true)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"ticket": secret.Reveal()})
	})
	control.HandleFunc("/_events", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(rec.snapshot())
	})
	control.HandleFunc("/_reset", func(w http.ResponseWriter, _ *http.Request) {
		rec.reset()
		w.WriteHeader(http.StatusNoContent)
	})

	// One listener serves both: the control routes first, everything else to the
	// WebTUI handler.
	srv := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/healthz", "/_ticket", "/_events", "/_reset":
				control.ServeHTTP(w, r)
			default:
				webRouter(h).ServeHTTP(w, r)
			}
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	fmt.Println("fixture listening on http://" + addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// webRouter mounts the WebTUI page and WebSocket on a plain mux, using the
// handler's own Guard so the tests exercise the real handshake controls.
func webRouter(h *web.Handler) http.Handler {
	var reg server.Registry
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", h.ServePage)
	mux.Handle("GET /ws", h.Guard(h.WebSocketHandler(&reg)))
	return mux
}
