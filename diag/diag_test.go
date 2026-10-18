package diag

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/logger"
)

// TestMain runs a child's part when the test binary is started as one: a cell that needs a fresh
// process (nothing else imported, MemProfileRate set before any allocation) runs it there.
func TestMain(m *testing.M) {
	switch os.Getenv("DIAG_CHILD") {
	case "inert":
		childInert()
	case "sites":
		childSites()
	case "labels":
		childLabels()
	default:
		os.Exit(m.Run())
	}
}

// child runs this binary as the named child, with args after it, and returns what it printed.
func child(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "DIAG_CHILD="+name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the %s child failed: %v\n%s", name, err, out)
	}
	return string(out)
}

// shortDir is a directory whose socket path stays under a unix socket's limit, removed after t.
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "diag")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// lines are dir's samples.jsonl, decoded.
func lines(t *testing.T, dir string) []map[string]any {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, samplesName))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var v map[string]any
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			t.Fatalf("a line is not JSON: %v: %s", err, sc.Bytes())
		}
		out = append(out, v)
	}
	return out
}

func kinds(ls []map[string]any, kind string) []map[string]any {
	var out []map[string]any
	for _, l := range ls {
		if l["kind"] == kind {
			out = append(out, l)
		}
	}
	return out
}

func fastSampling(t *testing.T) {
	was := minEvery
	minEvery = time.Millisecond
	t.Cleanup(func() { minEvery = was })
}

// childInert imports diag and never starts it: the default mux must hold no pprof route.
func childInert() {
	for _, p := range []string{"/debug/pprof/", "/debug/pprof/heap", "/debug/pprof/cmdline"} {
		if _, pattern := http.DefaultServeMux.Handler(httptest.NewRequest(http.MethodGet, p, nil)); pattern != "" {
			fmt.Printf("registered %s as %q\n", p, pattern)
		}
	}
	fmt.Println("done")
}

// Importing diag registers nothing on http.DefaultServeMux, started or not; a started monitor's
// socket still serves the heap profile.
func TestImportingDiagRegistersNothing(t *testing.T) {
	if out := child(t, "inert"); out != "done\n" {
		t.Fatalf("the default mux holds pprof routes:\n%s", out)
	}
	m, err := Start(context.Background(), shortDir(t), WithSocket())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var b bytes.Buffer
	if err := Fetch(context.Background(), m.Socket(), "heap", &b); err != nil || !gzipped(b.Bytes()) {
		t.Fatalf("Fetch(heap): %v, %d bytes", err, b.Len())
	}
}

func gzipped(b []byte) bool { return len(b) > 2 && b[0] == 0x1f && b[1] == 0x8b }

// get is a raw request on the socket, past Fetch's own checks.
func get(t *testing.T, socket, path string) int {
	t.Helper()
	c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}}
	resp, err := c.Get("http://diag" + path)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

// The socket serves heap, allocs, goroutine and a CPU profile, nothing else; Fetch refuses
// anything else before it asks.
func TestTheSocketServesFourProfiles(t *testing.T) {
	m, err := Start(context.Background(), shortDir(t), WithSocket())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	fi, err := os.Stat(m.Socket())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the socket's mode: %v, %v; want 0600", fi.Mode().Perm(), err)
	}
	for _, p := range []string{"heap", "allocs", "goroutine", "profile?seconds=1"} {
		var b bytes.Buffer
		if err := Fetch(context.Background(), m.Socket(), p, &b); err != nil || !gzipped(b.Bytes()) {
			t.Errorf("Fetch(%s): %v, %d bytes", p, err, b.Len())
		}
	}
	for _, p := range []string{"/debug/pprof/", "/debug/pprof/cmdline", "/debug/pprof/trace", "/debug/pprof/symbol", "/debug/pprof/threadcreate", "/debug/pprof/block"} {
		if code := get(t, m.Socket(), p); code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", p, code)
		}
	}
	for _, p := range []string{"cmdline", "trace", "heap?debug=1", "profile?seconds=999", "profile?seconds=1&debug=1", "../heap", ""} {
		if err := Fetch(context.Background(), m.Socket(), p, io.Discard); err == nil {
			t.Errorf("Fetch(%q) was asked for", p)
		}
	}
}

const (
	sentinelArg   = "diag-sentinel-argument-7f3a"
	sentinelLabel = "diag-sentinel-label-9c1e"
)

// childLabels runs with a sentinel argument and a goroutine under a sentinel label, and says
// which of its profiles hold which.
func childLabels() {
	ready, hold := make(chan struct{}), make(chan struct{})
	pprof.Do(context.Background(), pprof.Labels("sentinel", sentinelLabel), func(context.Context) {
		go func() { close(ready); <-hold }()
	})
	<-ready
	for _, p := range []string{"heap", "allocs", "goroutine"} {
		var b bytes.Buffer
		if err := pprof.Lookup(p).WriteTo(&b, 0); err != nil {
			fmt.Println("error", err)
			return
		}
		r, _ := gzip.NewReader(&b)
		raw, _ := io.ReadAll(r)
		fmt.Printf("%s arg=%v label=%v\n", p, bytes.Contains(raw, []byte(sentinelArg)), bytes.Contains(raw, []byte(sentinelLabel)))
	}
	close(hold)
}

// Heap and allocs profiles hold neither the program's arguments nor its labels; the goroutine
// profile holds its labels, as the content table says.
func TestWhatTheProfilesHold(t *testing.T) {
	out := child(t, "labels", sentinelArg)
	for _, want := range []string{"heap arg=false label=false", "allocs arg=false label=false", "goroutine arg=false label=true"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

// One monitor owns a directory, socket or not, however many race to start.
func TestOneMonitorOwnsADirectory(t *testing.T) {
	for _, socket := range []bool{false, true} {
		dir := shortDir(t)
		var opts []Option
		if socket {
			opts = append(opts, WithSocket())
		}
		var wg sync.WaitGroup
		ms := make([]*Monitor, 8)
		errs := make([]error, 8)
		for i := range ms {
			wg.Add(1)
			go func() { defer wg.Done(); ms[i], errs[i] = Start(context.Background(), dir, opts...) }()
		}
		wg.Wait()
		won := 0
		for i, m := range ms {
			if errs[i] == nil {
				won++
				defer m.Close()
			} else if !strings.Contains(errs[i].Error(), "another monitor owns") {
				t.Errorf("socket %v: a loser's error: %v", socket, errs[i])
			}
		}
		if won != 1 {
			t.Fatalf("socket %v: %d monitors started on one directory, want 1", socket, won)
		}
	}
}

// What is already at pprof.sock decides: a stale socket is replaced, and a live one, a file, a
// directory and a symlink are each refused and left as they were.
func TestWhatIsAtTheSocketPath(t *testing.T) {
	t.Run("stale", func(t *testing.T) {
		dir := shortDir(t)
		ln, err := net.Listen("unix", filepath.Join(dir, socketName))
		if err != nil {
			t.Fatal(err)
		}
		ln.(*net.UnixListener).SetUnlinkOnClose(false)
		ln.Close() // a socket nothing serves: a monitor that died
		m, err := Start(context.Background(), dir, WithSocket())
		if err != nil {
			t.Fatalf("a stale socket was not replaced: %v", err)
		}
		m.Close()
	})
	t.Run("live", func(t *testing.T) {
		dir := shortDir(t)
		ln, err := net.Listen("unix", filepath.Join(dir, socketName))
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		if _, err := Start(context.Background(), dir, WithSocket()); err == nil || !strings.Contains(err.Error(), "served by another process") {
			t.Fatalf("a live socket: %v", err)
		}
		if c, err := net.Dial("unix", filepath.Join(dir, socketName)); err != nil {
			t.Fatalf("the live socket was removed: %v", err)
		} else {
			c.Close()
		}
	})
	for name, make := range map[string]func(path string) error{
		"file":      func(p string) error { return os.WriteFile(p, []byte("keep"), 0o600) },
		"directory": func(p string) error { return os.Mkdir(p, 0o700) },
		"symlink":   func(p string) error { return os.Symlink("/dev/null", p) },
	} {
		t.Run(name, func(t *testing.T) {
			dir := shortDir(t)
			p := filepath.Join(dir, socketName)
			if err := make(p); err != nil {
				t.Fatal(err)
			}
			if _, err := Start(context.Background(), dir, WithSocket()); err == nil || !strings.Contains(err.Error(), "is not a socket") {
				t.Fatalf("a %s at the socket path: %v", name, err)
			}
			if _, err := os.Lstat(p); err != nil {
				t.Fatalf("the %s was removed: %v", name, err)
			}
			if m, err := Start(context.Background(), dir); err != nil {
				t.Fatalf("the refused Start kept the directory: %v", err)
			} else {
				m.Close()
			}
		})
	}
	t.Run("too long", func(t *testing.T) {
		dir := filepath.Join(shortDir(t), strings.Repeat("d", 100))
		if _, err := Start(context.Background(), dir, WithSocket()); err == nil || !strings.Contains(err.Error(), "over the 100") {
			t.Fatalf("a long path: %v", err)
		}
	})
	t.Run("denied", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads and writes every directory")
		}
		dir := shortDir(t)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(dir, 0o700)
		if _, err := Start(context.Background(), dir); err == nil {
			t.Fatal("a directory it cannot write started a monitor")
		}
	})
}

// Close removes its own socket and releases the directory; the lock file stays, unlocked.
func TestCloseReleasesTheDirectory(t *testing.T) {
	dir := shortDir(t)
	m, err := Start(context.Background(), dir, WithSocket())
	if err != nil {
		t.Fatal(err)
	}
	sock := m.Socket()
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("a second Close: %v", err)
	}
	if _, err := os.Lstat(sock); err == nil {
		t.Error("Close left its socket")
	}
	if _, err := os.Stat(filepath.Join(dir, lockName)); err != nil {
		t.Errorf("Close removed the lock file: %v", err)
	}
	m2, err := Start(context.Background(), dir, WithSocket())
	if err != nil {
		t.Fatalf("the directory is not free after Close: %v", err)
	}
	m2.Close()
}

// A reused directory continues the snapshot numbering, and keeps the snapshots it holds.
func TestAReusedDirectoryKeepsItsSnapshots(t *testing.T) {
	dir := shortDir(t)
	for _, n := range []string{"heap-1.pb.gz", "heap-2.pb.gz"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m, err := Start(context.Background(), dir, WithStep(1)) // a snapshot at the first sample
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	for _, n := range []string{"heap-1.pb.gz", "heap-2.pb.gz"} {
		if b, _ := os.ReadFile(filepath.Join(dir, n)); string(b) != n {
			t.Errorf("%s was overwritten", n)
		}
	}
	snaps := kinds(lines(t, dir), "snapshot")
	if len(snaps) == 0 || snaps[0]["file"] != "heap-3.pb.gz" || snaps[0]["sites_age"] != sitesAge {
		t.Fatalf("the first snapshot: %v", snaps)
	}
	b, err := os.ReadFile(filepath.Join(dir, "heap-3.pb.gz"))
	if err != nil || !gzipped(b) {
		t.Fatalf("heap-3.pb.gz: %v", err)
	}
}

// holdSites keeps what it allocates: one call site, two object sizes.
var held [][]byte

//go:noinline
func holdSites(n int) {
	for i := range n {
		size := 4096
		if i%2 == 1 {
			size = 8192
		}
		held = append(held, make([]byte, size))
	}
}

// keeper holds what its method allocates: a group named by its receiver type.
type keeper struct{ b [][]byte }

//go:noinline
func (k *keeper) fill(n int) {
	for range n {
		k.b = append(k.b, make([]byte, 16384))
	}
}

var kept = &keeper{}

// childSites samples every allocation, holds a known amount from one call site and from one
// method, completes the profile's cycles, and prints the top site and the groups.
func childSites() {
	runtime.MemProfileRate = 1
	held = make([][]byte, 0, 512) // here, so holdSites allocates only what it holds
	kept.b = make([][]byte, 0, 64)
	// no collection may start inside the holders: the runtime allocates on the goroutine that
	// starts one, and those allocations would be charged to the holder's group
	gcPercent := debug.SetGCPercent(-1)
	holdSites(512) // 256 of 4096 bytes and 256 of 8192: 3 MiB
	kept.fill(64)  // 64 of 16384: 1 MiB
	debug.SetGCPercent(gcPercent)
	for range 3 {
		runtime.GC()
	}
	top, groups := sites(3, []string{"github.com/yongjohnlee80/golib/diag."})
	for _, g := range groups {
		fmt.Printf("group %s in_use=%d objects=%d\n", g.Group, g.InUse, g.Objects)
	}
	n := 0
	all, _ := sites(1<<20, nil)
	for _, s := range all {
		if len(s.Stack) > 0 && strings.Contains(s.Stack[0], "diag.holdSites") {
			n++
		}
	}
	if len(top) == 0 {
		fmt.Println("no sites")
		return
	}
	fmt.Printf("top=%s in_use=%d objects=%d holdSites_sites=%d\n", top[0].Stack[0], top[0].InUse, top[0].Objects, n)
	runtime.KeepAlive(held)
	runtime.KeepAlive(kept)
}

// The site holding the most is the allocation stack that holds it, with its exact bytes when every
// allocation is sampled, its two object sizes summed into one site; grouped, a function's sites
// are its own group and a method's are its receiver type's.
func TestTheTopSiteIsTheOneHolding(t *testing.T) {
	out := child(t, "sites")
	for _, want := range []string{
		"top=github.com/yongjohnlee80/golib/diag.holdSites ",
		fmt.Sprintf("in_use=%d objects=512 holdSites_sites=1", 256*4096+256*8192),
		fmt.Sprintf("group github.com/yongjohnlee80/golib/diag.holdSites in_use=%d objects=512\n", 256*4096+256*8192),
		fmt.Sprintf("group github.com/yongjohnlee80/golib/diag.keeper in_use=%d objects=64\n", 64*16384),
		"group other ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

// receiverOf names a method's group by its type, and a function's by itself.
func TestReceiverOf(t *testing.T) {
	for in, want := range map[string]string{
		"(*pixelLayout).lay":  "pixelLayout",
		"Editor.SetValue":     "Editor",
		"holdSites":           "holdSites",
		"holdSites.func1":     "holdSites",
		"(*Monitor).snapshot": "Monitor",
	} {
		if got := receiverOf(in); got != want {
			t.Errorf("receiverOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// lines is what a logger was given, in order.
type linesLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *linesLogger) Log(_ logger.Severity, payload any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprint(payload))
}

// With a logger, each sample and snapshot is also one debug line, readable as it stands.
func TestTheLoggerGetsSamplesAndSnapshots(t *testing.T) {
	l := &linesLogger{}
	m, err := Start(context.Background(), shortDir(t), WithLogger(l), WithStep(1), WithGroups("github.com/yongjohnlee80/golib/diag."))
	if err != nil {
		t.Fatal(err)
	}
	// the sampler's own lines, before Close's: Close logs a last sample too
	sampled := func() bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		for _, line := range l.lines {
			if strings.HasPrefix(line, "diag: heap live ") {
				return true
			}
		}
		return false
	}
	if !sampled() {
		t.Fatal("Start's first sample was not logged")
	}
	m.Close()
	l.mu.Lock()
	defer l.mu.Unlock()
	var sample, snap bool
	for _, line := range l.lines {
		sample = sample || strings.HasPrefix(line, "diag: heap live ") && strings.Contains(line, "goroutines")
		snap = snap || strings.HasPrefix(line, "diag: snapshot ") && strings.Contains(line, "heap-") && strings.Contains(line, "by group: ")
	}
	if !sample || !snap {
		t.Fatalf("the logger's lines lack a sample (%v) or a grouped snapshot (%v):\n%s", sample, snap, strings.Join(l.lines, "\n"))
	}
}

// scaleHeapSample is runtime/pprof's estimate, count and bytes together.
func TestScaleHeapSample(t *testing.T) {
	for _, c := range []struct{ count, size, rate, wantCount, wantSize int64 }{
		{2, 1024, 512 * 1024, 2049, 1049088},
		{10, 40960, 512 * 1024, 1285, 5263386},
		{1, 1 << 20, 512 * 1024, 1, 1212696},
		{0, 100, 512 * 1024, 0, 0},
		{3, 0, 512 * 1024, 0, 0},
		{3, 300, 1, 3, 300},
	} {
		if n, b := scaleHeapSample(c.count, c.size, c.rate); n != c.wantCount || b != c.wantSize {
			t.Errorf("scaleHeapSample(%d, %d, %d) = %d, %d; want %d, %d", c.count, c.size, c.rate, n, b, c.wantCount, c.wantSize)
		}
	}
}

// The sampler writes samples of the runtime's memory every period, stops on ctx's end, and Close
// writes a last sample and a final snapshot.
func TestTheSamplerSamples(t *testing.T) {
	fastSampling(t)
	dir := shortDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	m, err := Start(ctx, dir, WithEvery(5*time.Millisecond), WithStep(1<<40))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(kinds(lines(t, dir), "sample")) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-m.done: // the sampler returned on ctx's end: nothing writes until Close
	case <-time.After(5 * time.Second):
		t.Fatal("the sampler did not stop on ctx's end")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	ls := lines(t, dir)
	samples, snaps := kinds(ls, "sample"), kinds(ls, "snapshot")
	if len(samples) < 4 || len(snaps) != 1 {
		t.Fatalf("%d samples and %d snapshots; want at least 4 (3, and Close's), and Close's snapshot", len(samples), len(snaps))
	}
	s := samples[len(samples)-1]
	for _, k := range []string{"heap_live", "heap_objects", "go_total", "goroutines", "gc_cycles", "heap_free", "heap_unused", "heap_released", "rss"} {
		if _, ok := s[k].(float64); !ok {
			t.Errorf("a sample lacks %s: %v", k, s)
		}
	}
	if s["go_total"].(float64) < s["heap_objects"].(float64) || s["goroutines"].(float64) < 1 {
		t.Errorf("an implausible sample: %v", s)
	}
	if runtime.GOOS == "linux" && s["rss"].(float64) <= 0 {
		t.Error("no resident size on Linux")
	}
}

// FromEnv is a wrapper's switch: a directory turns it on, unset or blank does not.
func TestFromEnv(t *testing.T) {
	t.Setenv("DIAG_TEST_PROFILE", "  /tmp/x  ")
	if dir, ok := FromEnv("DIAG_TEST_PROFILE"); !ok || dir != "/tmp/x" {
		t.Errorf("FromEnv: %q, %v", dir, ok)
	}
	t.Setenv("DIAG_TEST_PROFILE", " ")
	if _, ok := FromEnv("DIAG_TEST_PROFILE"); ok {
		t.Error("a blank variable turned it on")
	}
	if _, ok := FromEnv("DIAG_TEST_UNSET_" + fmt.Sprint(time.Now().UnixNano())); ok {
		t.Error("an unset variable turned it on")
	}
}

// Close ends a CPU profile in progress: the fetch returns at once, not after its seconds.
func TestCloseEndsACPUProfile(t *testing.T) {
	m, err := Start(context.Background(), shortDir(t), WithSocket())
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	was := cpuStarted
	cpuStarted = func() { close(started) }
	defer func() { cpuStarted = was }()
	done := make(chan error, 1)
	go func() { done <- Fetch(context.Background(), m.Socket(), "profile?seconds=60", io.Discard) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the CPU profile did not start")
	}
	start := time.Now()
	m.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the CPU profile outlived Close")
	}
	// without the stop, only Shutdown's deadline would end it, two seconds on
	if d := time.Since(start); d > time.Second {
		t.Errorf("Close took %v with a CPU profile running: it waited for Shutdown's deadline", d)
	}
	// the process may profile again: the profile was stopped, not abandoned
	if err := pprof.StartCPUProfile(io.Discard); err != nil {
		t.Fatalf("a CPU profile is still running: %v", err)
	}
	pprof.StopCPUProfile()
}

// A heap profile that could not be written is Close's failure, the same on a second Close.
func TestASnapshotThatFailsFailsClose(t *testing.T) {
	dir := shortDir(t)
	m, err := Start(context.Background(), dir, WithStep(1<<40)) // no snapshot until Close's
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "heap-1.pb.gz"), []byte("taken"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = m.Close()
	if err == nil || !strings.Contains(err.Error(), "heap-1.pb.gz") {
		t.Fatalf("Close after a failed snapshot: %v", err)
	}
	if again := m.Close(); again == nil || again.Error() != err.Error() {
		t.Errorf("a second Close: %v, want %v", again, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "heap-1.pb.gz")); string(b) != "taken" {
		t.Error("the taken name was overwritten")
	}
}

// Fetch keeps no connection: each one it opens is closed when it returns.
func TestFetchKeepsNoConnection(t *testing.T) {
	sock := filepath.Join(shortDir(t), "f.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	opened, closed := 0, 0
	srv := &http.Server{Handler: profileMux(make(chan struct{})), ConnState: func(_ net.Conn, s http.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		switch s {
		case http.StateNew:
			opened++
		case http.StateClosed, http.StateHijacked:
			closed++
		}
	}}
	go srv.Serve(ln)
	defer srv.Close()
	counts := func() (int, int) { mu.Lock(); defer mu.Unlock(); return opened, closed }
	for i := 1; i <= 4; i++ {
		if err := Fetch(context.Background(), sock, "heap", io.Discard); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for o, c := counts(); c < o && time.Now().Before(deadline); o, c = counts() {
			time.Sleep(5 * time.Millisecond)
		}
		if o, c := counts(); o != i || c != i {
			t.Fatalf("after Fetch %d: %d connections opened, %d closed; want %d and %d", i, o, c, i, i)
		}
	}
}

//go:noinline
func funcCallers() []uintptr {
	pcs := make([]uintptr, 32)
	return pcs[:runtime.Callers(1, pcs)]
}

//go:noinline
func (k *keeper) callers() []uintptr {
	pcs := make([]uintptr, 32)
	return pcs[:runtime.Callers(1, pcs)]
}

// A group is the matched frame's own identity, whichever prefix admitted it.
func TestAGroupIsItsFramesOwnIdentity(t *testing.T) {
	const pkg = "github.com/yongjohnlee80/golib/diag"
	fn, method := funcCallers(), (&keeper{}).callers()
	for _, prefix := range []string{pkg + ".", pkg, "github.com/yongjohnlee80/golib/", "github.com/"} {
		if got := groupOf(fn, []string{prefix}); got != pkg+".funcCallers" {
			t.Errorf("prefix %q, a function: %q", prefix, got)
		}
		if got := groupOf(method, []string{prefix}); got != pkg+".keeper" {
			t.Errorf("prefix %q, a method: %q", prefix, got)
		}
	}
	if got := groupOf(fn, []string{pkg + ".funcCall"}); got != pkg+".funcCallers" {
		t.Errorf("a function's own prefix: %q", got)
	}
	if got := groupOf(method, []string{pkg + ".(*keep"}); got != pkg+".keeper" {
		t.Errorf("a method's own prefix: %q", got)
	}
	if got := groupOf(fn, []string{"example.com/nothing."}); got != "other" {
		t.Errorf("no frame matched: %q", got)
	}
}
