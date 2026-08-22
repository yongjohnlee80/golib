// Command webdemo runs the golib TUI demo in a browser.
//
// It is ADR-0009 acceptance criterion 1: the SAME component tree the terminal
// demo uses, driven by tui/web instead of tui/term. The only difference between
// this file and tui/examples/demo/main.go is which backend is handed to
// tui.NewApp — the component code is shared and unmodified.
//
// # Running it
//
// Authentication is mandatory, so the demo mints a single-use ticket and prints a
// URL carrying it in the FRAGMENT, which is never sent to a server:
//
//	go run ./tui/examples/webdemo
//	# open the printed http://127.0.0.1:8080/#t=... in a browser
//
// The bind is loopback and plaintext, which is permitted only because it is
// loopback; a non-loopback bind without TLS refuses to start. For a remote host,
// forward it over SSH and leave the bind alone:
//
//	ssh -L 8080:127.0.0.1:8080 host
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/token"
	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/examples/demoapp"
	"github.com/yongjohnlee80/golib/tui/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "loopback bind address")
	flag.Parse()

	if err := run(*addr); err != nil {
		fmt.Fprintln(os.Stderr, "webdemo:", err)
		os.Exit(1)
	}
}

func run(addr string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := logger.New(logger.WithWriter(os.Stderr))

	// The attach policy. A ticket only, for a demo — a real deployment composes
	// mTLS and an SSH challenge alongside it with web.RecommendedPolicy.
	store := token.NewMemStore(64)
	policy, err := web.RecommendedPolicy([]auth.Factor{token.NewFactor(store)})
	if err != nil {
		return err
	}

	// One session, one App. THIS is the criterion: the same demoapp tree the
	// terminal demo runs, with a different backend.
	mgr, err := web.NewManager(func(b *web.Backend, _ *web.SessionInfo) web.Runner {
		return tui.NewApp(demoapp.New(stop, true), tui.WithBackend(b))
	}, web.MaxSessions(4), web.ManagerLogger(log))
	if err != nil {
		return err
	}

	origin := "http://" + addr
	h, err := web.NewHandler(web.Config{
		Addr:   addr,
		Policy: policy,
		// Configured, never inferred from the request.
		AllowedOrigins: []string{origin},
		ExpectedHost:   addr,
	}, mgr, web.Title("golib TUI demo"), web.HandlerLogger(log))
	if err != nil {
		// A misconfigured WebTUI must not start. A non-loopback addr without TLS
		// lands here.
		return err
	}

	// A single-use ticket, handed over out of band — in a real deployment this is
	// minted over the user's existing SSH session, which is what makes the SSH
	// hop the authentication.
	secret, err := token.NewIssuer(store).Issue("demo", 5*time.Minute, true)
	if err != nil {
		return err
	}
	fmt.Printf("\n  open  %s/#t=%s\n\n", origin, secret.Reveal())
	fmt.Println("  the ticket is in the URL FRAGMENT, which browsers never send to a")
	fmt.Println("  server; the client scrubs it from the address bar before connecting.")
	fmt.Println("  it is single-use — reload after connecting and you will need a new one.")
	fmt.Println()

	if err := h.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
