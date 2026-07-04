package logger

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestFromSlog_FieldsBecomeAttrs(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	sl := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	l := FromSlog(sl)

	Info(l, Fields{"msg": "hello", "user": "jo", "count": 3})
	out := buf.String()
	for _, want := range []string{"msg=hello", "user=jo", "count=3", "level=INFO"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
}

func TestFromSlog_EntryKeepsErrAttr(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	sl := slog.New(slog.NewTextHandler(&buf, nil))
	l := FromSlog(sl)

	boom := errors.New("boom")
	Error(l, boom, Fields{"op": "save"})
	out := buf.String()
	for _, want := range []string{"err=boom", "op=save", "level=ERROR", "msg=boom"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
}

func TestFromSlog_PlainPayloadIsMessage(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := FromSlog(slog.New(slog.NewTextHandler(&buf, nil)))
	Info(l, "just text")
	if !strings.Contains(buf.String(), `msg="just text"`) {
		t.Errorf("output = %s", buf.String())
	}
}

func TestNewSlogHandler_ForwardsToLogger(t *testing.T) {
	t.Parallel()
	rec := &recordingLogger{}
	sl := slog.New(NewSlogHandler(rec))

	sl.Warn("careful", "k", "v")
	if len(rec.entries) != 1 {
		t.Fatalf("expected 1 record, got %d", len(rec.entries))
	}
	e := rec.entries[0]
	if e.sev != SeverityWarning {
		t.Errorf("severity = %s, want warning", e.sev)
	}
	f, ok := e.payload.(Fields)
	if !ok {
		t.Fatalf("payload %T, want Fields", e.payload)
	}
	if f["msg"] != "careful" || f["k"] != "v" {
		t.Errorf("fields = %v", f)
	}
}

func TestNewSlogHandler_GroupsAndAttrs(t *testing.T) {
	t.Parallel()
	rec := &recordingLogger{}
	sl := slog.New(NewSlogHandler(rec)).With("app", "x").WithGroup("req")

	sl.Info("hit", "path", "/y")
	f := rec.entries[0].payload.(Fields)
	if f["app"] != "x" {
		t.Errorf("WithAttrs attr lost: %v", f)
	}
	if f["req.path"] != "/y" {
		t.Errorf("group prefix missing: %v", f)
	}
}

func TestSeverityLevelRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []Severity{SeverityDebug, SeverityInfo, SeverityNotice, SeverityWarning, SeverityError, SeverityCritical} {
		if got := severityOf(slogLevel(s)); got != s {
			t.Errorf("round trip %s -> %v -> %s", s, slogLevel(s), got)
		}
	}
}

type recordingLogger struct {
	entries []struct {
		sev     Severity
		payload any
	}
}

func (r *recordingLogger) Log(s Severity, p any) {
	r.entries = append(r.entries, struct {
		sev     Severity
		payload any
	}{s, p})
}
