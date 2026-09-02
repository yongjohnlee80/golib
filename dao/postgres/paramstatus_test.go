package postgres

import (
	"bytes"
	"encoding/binary"
	"io"
	"math/rand"
	"testing"

	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// frame builds one backend frame: type byte, int32 length (including itself), body.
func frame(typ byte, body []byte) []byte {
	out := make([]byte, 5+len(body))
	out[0] = typ
	binary.BigEndian.PutUint32(out[1:5], uint32(4+len(body)))
	copy(out[5:], body)
	return out
}

func statusFrame(name, value string) []byte {
	return frame('S', append(append(append([]byte(name), 0), []byte(value)...), 0))
}

// startupStream is a realistic connect sequence: AuthenticationOk, several
// ParameterStatus frames, BackendKeyData, ReadyForQuery — followed by a large
// CopyData frame and one more ParameterStatus (a later SET).
func startupStream() ([]byte, map[string]string) {
	want := map[string]string{
		"server_version": "17.2", "client_encoding": "UTF8", "DateStyle": "ISO, MDY", "TimeZone": "UTC",
		"integer_datetimes": "on", "standard_conforming_strings": "on", "server_encoding": "UTF8",
		"is_superuser": "off", "session_authorization": "autodb", "application_name": "", "in_hot_standby": "off",
	}
	var s []byte
	s = append(s, frame('R', []byte{0, 0, 0, 0})...) // AuthenticationOk
	for _, k := range []string{"server_version", "client_encoding", "DateStyle", "TimeZone", "integer_datetimes", "standard_conforming_strings", "server_encoding", "is_superuser", "session_authorization", "application_name", "in_hot_standby"} {
		s = append(s, statusFrame(k, want[k])...)
	}
	s = append(s, frame('K', []byte{0, 0, 0, 7, 0, 0, 0, 9})...) // BackendKeyData
	s = append(s, frame('Z', []byte{'I'})...)                    // ReadyForQuery
	big := bytes.Repeat([]byte{'x'}, 70000)                      // a large frame: CopyData, whose body is raw bytes a real Frontend decodes as-is
	s = append(s, frame('d', big)...)
	s = append(s, statusFrame("application_name", "psql")...) // a later SET
	want["application_name"] = "psql"
	return s, want
}

// chunkReader hands the stream out in the given chunk sizes.
type chunkReader struct {
	data   []byte
	sizes  []int
	i, off int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if c.off >= len(c.data) {
		return 0, io.EOF
	}
	n := c.sizes[c.i%len(c.sizes)]
	c.i++
	if n > len(p) {
		n = len(p)
	}
	if c.off+n > len(c.data) {
		n = len(c.data) - c.off
	}
	copy(p, c.data[c.off:c.off+n])
	c.off += n
	return n, nil
}

func drain(t *testing.T, r io.Reader) {
	t.Helper()
	buf := make([]byte, 4096)
	for {
		if _, err := r.Read(buf); err == io.EOF {
			return
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

// The tee records every ParameterStatus in a realistic startup + later-SET
// stream, whatever the read boundaries: whole, one byte at a time, and random.
func TestStatusTee_RecordsEveryParameterStatusAcrossAnySplit(t *testing.T) {
	t.Parallel()
	stream, want := startupStream()
	rng := rand.New(rand.NewSource(42))
	random := make([]int, 64)
	for i := range random {
		random[i] = 1 + rng.Intn(9000)
	}
	for name, sizes := range map[string][]int{"whole": {len(stream)}, "byte-at-a-time": {1}, "sevens": {7}, "random": random} {
		rec := &statusRecorder{vals: map[string]string{}}
		tee := &statusTee{r: &chunkReader{data: stream, sizes: sizes}, rec: rec}
		drain(t, tee)
		got := rec.snapshot()
		if len(got) != len(want) {
			t.Fatalf("[%s] recorded %d statuses, want %d: %v", name, len(got), len(want), got)
		}
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("[%s] %s = %q, want %q", name, k, got[k], v)
			}
		}
	}
}

// The tee passes bytes through unchanged — it is a reader, not a filter.
func TestStatusTee_PassesBytesThroughUnchanged(t *testing.T) {
	t.Parallel()
	stream, _ := startupStream()
	rec := &statusRecorder{vals: map[string]string{}}
	tee := &statusTee{r: &chunkReader{data: stream, sizes: []int{13}}, rec: rec}
	out, err := io.ReadAll(tee)
	if err != nil || !bytes.Equal(out, stream) {
		t.Fatalf("bytes altered by the tee (len %d vs %d, err %v)", len(out), len(stream), err)
	}
}

// Frames that are not ParameterStatus are never copied: the body buffer only
// grows for 'S' frames, even across a 70 000-byte CopyData frame.
func TestStatusTee_CopiesOnlyParameterStatusBodies(t *testing.T) {
	t.Parallel()
	stream, _ := startupStream()
	rec := &statusRecorder{vals: map[string]string{}}
	tee := &statusTee{r: &chunkReader{data: stream, sizes: []int{4096}}, rec: rec}
	drain(t, tee)
	if cap(tee.body) > 256 {
		t.Fatalf("body buffer grew to cap %d; only ParameterStatus bodies may be copied", cap(tee.body))
	}
}

// installStatusCapture registers a recorder per built frontend and composes
// with a caller's own BuildFrontend/BeforeClose.
func TestInstallStatusCapture_RegistersPerFrontendAndComposes(t *testing.T) {
	t.Parallel()
	cfg, err := pgxpool.ParseConfig("postgres://u:p@127.0.0.1:1/db")
	if err != nil {
		t.Fatal(err)
	}
	prevCalled := 0
	prev := cfg.ConnConfig.BuildFrontend
	cfg.ConnConfig.BuildFrontend = func(r io.Reader, w io.Writer) *pgproto3.Frontend {
		prevCalled++
		return prev(r, w)
	}
	installStatusCapture(cfg)
	stream, want := startupStream()
	f := cfg.ConnConfig.BuildFrontend(&chunkReader{data: stream, sizes: []int{11}}, io.Discard)
	if prevCalled != 1 {
		t.Fatalf("the caller's BuildFrontend was called %d times, want 1 (composition)", prevCalled)
	}
	v, ok := recorders.Load(f)
	if !ok {
		t.Fatal("no recorder registered for the built frontend")
	}
	// Drive the frontend's reader to the end through the frontend itself.
	for {
		if _, err := f.Receive(); err != nil {
			break
		}
	}
	got := v.(*statusRecorder).snapshot()
	if got["server_version"] != want["server_version"] || got["application_name"] != "psql" {
		t.Fatalf("recorder through the real frontend: %v", got)
	}
	recorders.Delete(f)
}

// A handle built without a pool (no capture) reports an empty set, not nil.
func TestReportedParameterStatuses_EmptyWithoutCapture(t *testing.T) {
	t.Parallel()
	p := &pinnedConn{}
	if got := p.ReportedParameterStatuses(); got == nil || len(got) != 0 {
		t.Fatalf("got %v, want an empty non-nil map", got)
	}
}
