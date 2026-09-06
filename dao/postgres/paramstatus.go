package postgres

import (
	"encoding/binary"
	"io"
	"runtime"
	"sync"
	"weak"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Reported ParameterStatus capture.
//
// PostgreSQL reports a set of GUC_REPORT parameters at connect (server_version,
// client_encoding, DateStyle, TimeZone, …) and again whenever one changes. The
// SERVER decides what is in that set — newer servers add to it — and a consumer
// relaying the protocol must forward every one of them verbatim rather than a
// fixed list (autodb protocol matrix). pgconn keeps the set in a private
// map and exposes only a per-key lookup, so nothing above the driver can
// enumerate what was reported.
//
// This file captures the set where it is visible: on the connection's own byte
// stream. pgconn lets a caller supply the frontend's constructor
// (Config.BuildFrontend) and calls it AFTER TLS negotiation, so the reader it
// hands us carries plaintext protocol frames. statusTee wraps that reader,
// frames the stream (one type byte + a 4-byte length that includes itself) and
// records the body of every 'S' (ParameterStatus) frame; every other frame is
// skipped by count without copying. The recorder is keyed by the *Frontend the
// constructor returns, which pgconn exposes as PgConn.Frontend(), and removed
// when the pool closes the connection.

// statusRecorder is one connection's reported set.
type statusRecorder struct {
	mu   sync.Mutex
	vals map[string]string
}

func (r *statusRecorder) set(name, value string) {
	r.mu.Lock()
	r.vals[name] = value
	r.mu.Unlock()
}

// snapshot returns a copy of everything reported so far.
func (r *statusRecorder) snapshot() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.vals))
	for k, v := range r.vals {
		out[k] = v
	}
	return out
}

// statusTee is an io.Reader that passes bytes through unchanged while framing
// the backend protocol stream and recording ParameterStatus bodies. It keeps
// no copy of any other frame. Partial reads are handled: a header or a body may
// arrive across any number of Read calls.
type statusTee struct {
	r   io.Reader
	rec *statusRecorder

	hdr    [5]byte // type + int32 length
	hdrLen int     // bytes of hdr collected so far
	remain int     // body bytes still to consume for the current frame
	isS    bool    // current frame is ParameterStatus
	body   []byte  // ParameterStatus body being collected
}

func (t *statusTee) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 {
		t.scan(p[:n])
	}
	return n, err
}

// scan advances the framing state machine over b.
func (t *statusTee) scan(b []byte) {
	for len(b) > 0 {
		if t.remain == 0 {
			// Collecting a header.
			need := 5 - t.hdrLen
			if need > len(b) {
				need = len(b)
			}
			copy(t.hdr[t.hdrLen:], b[:need])
			t.hdrLen += need
			b = b[need:]
			if t.hdrLen < 5 {
				return
			}
			t.hdrLen = 0
			length := int(binary.BigEndian.Uint32(t.hdr[1:5])) // includes itself, not the type byte
			t.remain = length - 4
			t.isS = t.hdr[0] == 'S'
			if t.isS {
				t.body = t.body[:0]
			}
			if t.remain <= 0 { // an empty body (or a malformed length): frame complete
				t.remain = 0
				if t.isS {
					t.finishS()
				}
			}
			continue
		}
		// Consuming a body.
		take := t.remain
		if take > len(b) {
			take = len(b)
		}
		if t.isS {
			t.body = append(t.body, b[:take]...)
		}
		t.remain -= take
		b = b[take:]
		if t.remain == 0 && t.isS {
			t.finishS()
		}
	}
}

// finishS parses a complete ParameterStatus body: name\0value\0.
func (t *statusTee) finishS() {
	body := t.body
	i := indexByte(body, 0)
	if i < 0 {
		return
	}
	name := string(body[:i])
	rest := body[i+1:]
	j := indexByte(rest, 0)
	if j < 0 {
		j = len(rest)
	}
	t.rec.set(name, string(rest[:j]))
	t.body = t.body[:0]
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// recorders maps each connection's frontend to its recorder for the life of
// the connection. The key is a WEAK pointer to the *Frontend, so the registry
// never keeps a frontend alive, and every entry is tied to the frontend's own
// lifetime through runtime.AddCleanup: a connect that fails after the frontend
// is built (wrong password, a failed ValidateConnect or AfterConnect) never
// becomes a pool resource and fires no pool hook, but its frontend becomes
// unreachable and the cleanup removes the entry (PR #23 MF1). The pool's
// BeforeClose removes entries eagerly for connections that did become
// resources.
var recorders sync.Map // weak.Pointer[pgproto3.Frontend] → *statusRecorder

// installStatusCapture wires the capture into a pool config: every connection
// the pool opens gets a recording frontend. Composes with a caller's own
// BuildFrontend and BeforeClose.
func installStatusCapture(cfg *pgxpool.Config) {
	prevBuild := cfg.ConnConfig.BuildFrontend
	if prevBuild == nil {
		prevBuild = pgproto3.NewFrontend
	}
	cfg.ConnConfig.BuildFrontend = func(r io.Reader, w io.Writer) *pgproto3.Frontend {
		rec := &statusRecorder{vals: make(map[string]string)}
		f := prevBuild(&statusTee{r: r, rec: rec}, w)
		key := weak.Make(f)
		recorders.Store(key, rec)
		// The cleanup's argument is the weak key, never f: an argument that
		// referenced f would keep it reachable and the cleanup would never run.
		runtime.AddCleanup(f, func(k weak.Pointer[pgproto3.Frontend]) { recorders.Delete(k) }, key)
		return f
	}
	prevClose := cfg.BeforeClose
	cfg.BeforeClose = func(c *pgx.Conn) {
		if pc := c.PgConn(); pc != nil && pc.Frontend() != nil {
			recorders.Delete(weak.Make(pc.Frontend()))
		}
		if prevClose != nil {
			prevClose(c)
		}
	}
}

// reportedStatuses returns the recorder for a live connection, or nil when the
// pool was opened without capture (a handle built by hand, or a pre-capture pool).
func reportedStatuses(pgConn *pgconn.PgConn) *statusRecorder {
	if pgConn == nil || pgConn.Frontend() == nil {
		return nil
	}
	v, ok := recorders.Load(weak.Make(pgConn.Frontend()))
	if !ok {
		return nil
	}
	return v.(*statusRecorder)
}

// ParameterStatusReporter is the OPTIONAL capability a pinned connection offers
// when its pool captures the server's reported ParameterStatus set. It is a
// separate leaf interface — PinnedConn itself is unchanged, so external
// implementations of PinnedConn keep compiling (PR #23 MF2; the
// interface-evolution convention) — reached by type assertion:
//
//	if r, ok := pc.(postgres.ParameterStatusReporter); ok { set := r.ReportedParameterStatuses() }
type ParameterStatusReporter interface {
	// ReportedParameterStatuses is every ParameterStatus the server has sent on
	// this connection — the connect-time GUC_REPORT set and later changes — as a
	// snapshot copy. Empty when the pool was opened without capture.
	ReportedParameterStatuses() map[string]string
}

var _ ParameterStatusReporter = (*pinnedConn)(nil)

// ReportedParameterStatuses returns every ParameterStatus the server has sent
// on this pinned connection — the connect-time GUC_REPORT set and any later
// change (a SET of a reported parameter) — as a snapshot copy. It is what a
// protocol relay forwards at session open in place of a fixed list. The map is
// empty when the connection was opened by a pool without capture.
func (p *pinnedConn) ReportedParameterStatuses() map[string]string {
	rec := reportedStatuses(p.pgConn)
	if rec == nil {
		return map[string]string{}
	}
	return rec.snapshot()
}
