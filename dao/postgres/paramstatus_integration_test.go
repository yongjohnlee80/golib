//go:build integration

package postgres

import (
	"testing"
)

// Live: the captured set is the server's own. Every captured key agrees with
// pgconn's private map (through its per-key lookup), the set carries the
// parameters every PostgreSQL reports, and a later SET of a reported parameter
// shows up in the snapshot. Requires TEST_PGURL (autodb-r3-pg scratch DB on
// VM43 — never lm-omni-db).
func TestParamStatus_Live_CapturedSetIsTheServersReportedSet(t *testing.T) {
	p := mustPin(t, openPG(t))
	got := p.ReportedParameterStatuses()
	for _, k := range []string{"server_version", "server_encoding", "client_encoding", "DateStyle", "TimeZone", "integer_datetimes", "standard_conforming_strings", "is_superuser", "session_authorization"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("captured set lacks %q; got %v", k, got)
		}
	}
	for k, v := range got {
		if pv := p.pgConn.ParameterStatus(k); pv != v {
			t.Fatalf("%s: captured %q, pgconn reports %q — the tee misparsed a frame", k, v, pv)
		}
	}
	if len(got) < 9 {
		t.Fatalf("only %d statuses captured: %v", len(got), got)
	}
}

// A runtime SET of a GUC_REPORT parameter reaches the snapshot through the same
// stream — the set is live, not a startup-only copy.
func TestParamStatus_Live_LaterSetIsCaptured(t *testing.T) {
	p := mustPin(t, openPG(t))
	before := p.ReportedParameterStatuses()["application_name"]
	if _, err := p.SimpleQuery(bg(t), "SET application_name = 'paramstatus_probe'", func(ExtendedMessage) error { return nil }); err != nil {
		t.Fatalf("SET: %v", err)
	}
	after := p.ReportedParameterStatuses()["application_name"]
	if after != "paramstatus_probe" || after == before {
		t.Fatalf("application_name before %q after %q, want the SET value", before, after)
	}
}

// Recorders do not leak: after the pool closes its connections the registry
// holds no entry for them.
func TestParamStatus_Live_RecorderRemovedOnClose(t *testing.T) {
	conn := openPG(t)
	p := mustPin(t, conn)
	f := p.pgConn.Frontend()
	if _, ok := recorders.Load(f); !ok {
		t.Fatal("no recorder for the pinned connection while it is live")
	}
	p.Discard() // destroys the member → pool closes the connection → BeforeClose
	if err := conn.Close(); err != nil {
		t.Fatalf("pool close: %v", err)
	}
	if _, ok := recorders.Load(f); ok {
		t.Fatal("recorder still registered after the connection was closed — leak")
	}
}
