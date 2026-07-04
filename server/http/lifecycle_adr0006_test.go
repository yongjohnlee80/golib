package httpserver

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/server"
)

// startServer runs srv on a goroutine and waits for the bind.
func startServer(t *testing.T, srv *Server) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- srv.Run(ctx) }()
	deadline := time.After(2 * time.Second)
	for srv.Addr() == "" {
		select {
		case <-deadline:
			t.Fatal("server never bound")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	return func() error {
		cancel()
		return <-errc
	}
}

func TestWithListener_ServesInjectedListener(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(WithListener(ln))
	srv.Get("/ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("pong")) })
	stop := startServer(t, srv)
	defer stop()

	if srv.Addr() != ln.Addr().String() {
		t.Fatalf("Addr = %q, want injected %q", srv.Addr(), ln.Addr())
	}
	res, err := http.Get("http://" + srv.Addr() + "/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if string(body) != "pong" {
		t.Fatalf("body = %q", body)
	}
}

// selfSignedPair returns a CA-style self-signed cert usable as both server
// and client identity for an mTLS handshake test.
func selfSignedPair(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "golib-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"golib-test"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	pool := x509.NewCertPool()
	parsed, _ := x509.ParseCertificate(der)
	pool.AddCert(parsed)
	return cert, pool
}

func TestWithTLSConfig_MutualTLS(t *testing.T) {
	t.Parallel()
	cert, pool := selfSignedPair(t)
	srv := New(Addr("127.0.0.1:0"), WithTLSConfig(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	}))
	srv.Get("/secure", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("mtls")) })
	stop := startServer(t, srv)
	defer stop()

	// Client WITH a certificate succeeds.
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:      pool,
		Certificates: []tls.Certificate{cert},
		ServerName:   "golib-test",
	}}}
	res, err := client.Get("https://" + srv.Addr() + "/secure")
	if err != nil {
		t.Fatalf("mTLS request: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "mtls" {
		t.Fatalf("body = %q", body)
	}

	// Client WITHOUT a certificate is refused at the handshake.
	bare := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: pool, ServerName: "golib-test",
	}}}
	if _, err := bare.Get("https://" + srv.Addr() + "/secure"); err == nil {
		t.Fatal("handshake without a client cert must fail under RequireAndVerifyClientCert")
	}
}

// recordingLogger captures structured records.
type recordingLogger struct {
	mu   sync.Mutex
	msgs []string
}

func (r *recordingLogger) Log(_ logger.Severity, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, fmt.Sprintf("%+v", payload))
}

func (r *recordingLogger) contains(sub string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

func TestErrorLog_FlowsThroughLogger(t *testing.T) {
	t.Parallel()
	lg := &recordingLogger{}
	cert, _ := selfSignedPair(t)
	srv := New(Addr("127.0.0.1:0"), WithLogger(lg),
		WithTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}}))
	stop := startServer(t, srv)
	defer stop()

	// Speak plaintext HTTP to a TLS port: the handshake failure is a
	// server-level error that must reach the structured logger.
	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	_ = conn.Close()

	deadline := time.After(2 * time.Second)
	for !lg.contains("server error") {
		select {
		case <-deadline:
			t.Fatal("TLS handshake failure never reached the logger")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestWithConnMetrics_ActiveGauge(t *testing.T) {
	t.Parallel()
	var peak atomic.Int64
	srv := New(Addr("127.0.0.1:0"), WithConnMetrics(func(_ http.ConnState, active int) {
		if int64(active) > peak.Load() {
			peak.Store(int64(active))
		}
	}))
	srv.Get("/m", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	stop := startServer(t, srv)
	defer stop()

	res, err := http.Get("http://" + srv.Addr() + "/m")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if peak.Load() < 1 {
		t.Errorf("active gauge peak = %d, want >= 1", peak.Load())
	}
}

func TestReadyz_DrainGate(t *testing.T) {
	t.Parallel()
	srv := New(Addr("127.0.0.1:0"))
	srv.Get("/readyz", srv.Readyz())
	srv.Get("/healthz", Healthz())
	stop := startServer(t, srv)

	get := func(path string) int {
		res, err := http.Get("http://" + srv.Addr() + path)
		if err != nil {
			return -1
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := get("/readyz"); code != 200 {
		t.Fatalf("readyz while serving = %d", code)
	}
	if code := get("/healthz"); code != 200 {
		t.Fatalf("healthz = %d", code)
	}

	// Hold a session so Shutdown stays in its drain phase while we probe.
	release := make(chan struct{})
	unreg := srv.Sessions().Register(blockingSession{release})
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done <- srv.Shutdown(ctx)
	}()
	deadline := time.After(2 * time.Second)
	for !srv.draining.Load() {
		select {
		case <-deadline:
			t.Fatal("shutdown never flipped the drain gate")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	// The listener is still accepting during drain (http.Server.Shutdown
	// stops accepting; probe the flag directly through the handler instead).
	rec := newLocalRecorder()
	srv.Readyz()(rec, nil)
	if rec.status != 503 {
		t.Fatalf("readyz during drain = %d, want 503", rec.status)
	}

	close(release)
	unreg()
	if err := <-done; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	_ = stop()
}

// blockingSession stays alive until released.
type blockingSession struct{ release chan struct{} }

func (b blockingSession) Close() error { return nil }
func (b blockingSession) Drain(ctx context.Context) error {
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return nil
}

// localRecorder is a minimal ResponseWriter for direct handler probes.
type localRecorder struct {
	h      http.Header
	status int
}

func newLocalRecorder() *localRecorder               { return &localRecorder{h: http.Header{}, status: 200} }
func (r *localRecorder) Header() http.Header         { return r.h }
func (r *localRecorder) WriteHeader(c int)           { r.status = c }
func (r *localRecorder) Write(b []byte) (int, error) { return len(b), nil }

func TestShutdown_DrainsRegisteredHijackedSession(t *testing.T) {
	t.Parallel()
	srv := New(Addr("127.0.0.1:0"))
	stop := startServer(t, srv)

	drained := &drainRecordSession{}
	unreg := srv.Sessions().Register(drained)

	go func() {
		time.Sleep(50 * time.Millisecond)
		drained.mu.Lock()
		asked := drained.asked
		drained.mu.Unlock()
		if asked {
			unreg() // polite end observed: session winds down
		}
	}()

	if err := stop(); err != nil {
		t.Fatalf("Run/Shutdown: %v", err)
	}
	drained.mu.Lock()
	defer drained.mu.Unlock()
	if !drained.asked {
		t.Error("Shutdown never drained the registered session")
	}
	if srv.Sessions().Len() != 0 {
		t.Error("sessions remain after shutdown")
	}
}

type drainRecordSession struct {
	mu    sync.Mutex
	asked bool
}

func (d *drainRecordSession) Close() error { return nil }
func (d *drainRecordSession) Drain(ctx context.Context) error {
	d.mu.Lock()
	d.asked = true
	d.mu.Unlock()
	return nil
}

// Silence unused-import guards on platforms where some paths differ.
var _ = server.Registry{}
