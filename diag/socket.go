package diag

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// maxSocketPath is the longest socket path served: a unix socket's path is limited to about 104
// bytes, and some systems take fewer.
const maxSocketPath = 100

// socketServer serves the runtime's profiles on a unix socket only the user can open.
type socketServer struct {
	path string
	srv  *http.Server
	ln   net.Listener
	stop chan struct{} // closed by close: a CPU profile in progress ends
}

// serveSocket listens on path, 0600. Called under the directory's lock, so nothing else probes,
// removes or listens here meanwhile. What is already at path decides:
//   - nothing: listen;
//   - a socket that answers: refused, a process outside the lock serves it;
//   - a socket that refuses the connection: stale, removed, then listen;
//   - anything else (a file, a directory, a symlink), or a probe that times out or is denied:
//     refused, and never removed.
func serveSocket(path string) (*socketServer, error) {
	if len(path) > maxSocketPath {
		return nil, fmt.Errorf("diag: the socket path %s is %d bytes, over the %d a unix socket allows: choose a shorter directory", path, len(path), maxSocketPath)
	}
	if err := clearStale(path); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("diag: %w", err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false) // close removes it, under the lock
	if err := os.Chmod(path, 0o600); err != nil {  // before Serve: only the user connects
		ln.Close()
		os.Remove(path)
		return nil, fmt.Errorf("diag: %w", err)
	}
	s := &socketServer{path: path, ln: ln, stop: make(chan struct{})}
	s.srv = &http.Server{Handler: profileMux(s.stop), ReadHeaderTimeout: 10 * time.Second}
	go s.srv.Serve(ln) //nolint:errcheck // Serve's error after close is http.ErrServerClosed
	return s, nil
}

// clearStale makes path free for a listener, or says why it is not.
func clearStale(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("diag: %w", err)
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("diag: %s exists and is not a socket: move it, or choose another directory", path)
	}
	c, err := net.DialTimeout("unix", path, time.Second)
	switch {
	case err == nil:
		c.Close()
		return fmt.Errorf("diag: %s is served by another process: choose another directory", path)
	case errors.Is(err, syscall.ECONNREFUSED):
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("diag: removing the stale socket: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("diag: probing %s: %w", path, err)
	}
}

// close ends a CPU profile in progress, stops serving and removes the socket.
func (s *socketServer) close() {
	close(s.stop)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.srv.Shutdown(ctx); err != nil {
		s.srv.Close()
	}
	s.ln.Close()
	os.Remove(s.path)
}

// cpuStarted is called once a CPU profile has begun: a hook for the package's tests.
var cpuStarted = func() {}

// maxCPUSeconds caps a CPU profile's length.
const maxCPUSeconds = 300

// served are the named profiles the socket serves. Heap and allocs hold allocation stacks only;
// goroutine also holds the program's pprof labels. Nothing else: no index, cmdline (os.Args
// verbatim), trace or symbol.
var served = map[string]bool{"heap": true, "allocs": true, "goroutine": true}

// profileMux serves /debug/pprof/<name> for the served profiles and /debug/pprof/profile?seconds=N
// for a CPU profile, which also holds labels, in pprof's form. A mux of its own: nothing is
// registered on http.DefaultServeMux.
func profileMux(stop <-chan struct{}) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /debug/pprof/profile", func(w http.ResponseWriter, r *http.Request) {
		secs, err := strconv.Atoi(cmp.Or(r.URL.Query().Get("seconds"), "30"))
		if err != nil || secs <= 0 || secs > maxCPUSeconds {
			http.Error(w, fmt.Sprintf("seconds is 1 to %d", maxCPUSeconds), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		if err := pprof.StartCPUProfile(w); err != nil {
			http.Error(w, err.Error(), http.StatusConflict) // the process allows one CPU profile at a time
			return
		}
		cpuStarted()
		t := time.NewTimer(time.Duration(secs) * time.Second)
		select {
		case <-t.C:
		case <-r.Context().Done(): // the fetcher went away
		case <-stop: // the monitor is closing
		}
		t.Stop()
		pprof.StopCPUProfile()
	})
	mux.HandleFunc("GET /debug/pprof/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		p := pprof.Lookup(name)
		if !served[name] || p == nil {
			http.Error(w, "no such profile", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		if err := p.WriteTo(w, 0); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	return mux
}

// Fetch asks the process serving socket for one profile, "heap", "allocs", "goroutine" or
// "profile?seconds=N" (CPU), and writes it to w in pprof's gzipped protobuf form. Goroutine and
// CPU profiles hold the program's pprof labels, if it sets any: treat them as sensitive.
func Fetch(ctx context.Context, socket, profile string, w io.Writer) error {
	if err := checkProfile(profile); err != nil {
		return err
	}
	// a transport of this call's own, its one connection closed when the call ends, whatever the
	// path: no connection is kept for a next Fetch
	tr := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://diag/debug/pprof/"+profile, nil)
	if err != nil {
		return fmt.Errorf("diag: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("diag: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("diag: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("diag: %w", err)
	}
	return nil
}

// checkProfile admits what the socket serves: a served name with no query, or "profile" with at
// most seconds=N.
func checkProfile(profile string) error {
	name, query, hasQuery := strings.Cut(profile, "?")
	if served[name] && !hasQuery {
		return nil
	}
	if name == "profile" {
		if !hasQuery {
			return nil
		}
		if secs, ok := strings.CutPrefix(query, "seconds="); ok {
			if n, err := strconv.Atoi(secs); err == nil && n > 0 && n <= maxCPUSeconds {
				return nil
			}
		}
	}
	return fmt.Errorf("diag: %q is not a profile the socket serves: heap, allocs, goroutine, or profile?seconds=1..%d", profile, maxCPUSeconds)
}
