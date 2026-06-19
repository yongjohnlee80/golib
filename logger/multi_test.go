package logger

import "testing"

func TestMulti_FansOut(t *testing.T) {
	t.Parallel()

	a, b := &recorder{}, &recorder{}
	m := NewMulti(a, b)
	m.Log(SeverityInfo, "x")

	if len(a.records) != 1 || len(b.records) != 1 {
		t.Fatalf("records a=%d b=%d, want 1 each", len(a.records), len(b.records))
	}
	if a.records[0].severity != SeverityInfo || b.records[0].severity != SeverityInfo {
		t.Errorf("severity not forwarded to both loggers")
	}
}

func TestMulti_SkipsNil(t *testing.T) {
	t.Parallel()

	a := &recorder{}
	m := NewMulti(nil, a, nil)
	m.Log(SeverityInfo, "x") // must not panic on the nil entries
	if len(a.records) != 1 {
		t.Errorf("records = %d, want 1", len(a.records))
	}
}
