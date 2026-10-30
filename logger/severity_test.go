package logger

import "testing"

func TestInList(t *testing.T) {
	t.Parallel()

	haystack := []Severity{SeverityDebug, SeverityError}
	if !InList(haystack, SeverityError) {
		t.Error("InList(SeverityError) = false, want true")
	}
	if InList(haystack, SeverityInfo) {
		t.Error("InList(SeverityInfo) = true, want false")
	}
	if InList(nil, SeverityInfo) {
		t.Error("InList(nil, ...) = true, want false")
	}
}

func TestRank(t *testing.T) {
	t.Parallel()

	ordered := []Severity{
		SeverityDebug, SeverityInfo, SeverityNotice,
		SeverityWarning, SeverityError, SeverityCritical,
	}
	for i := 1; i < len(ordered); i++ {
		if rank(ordered[i-1]) >= rank(ordered[i]) {
			t.Errorf("rank(%q)=%d not < rank(%q)=%d", ordered[i-1], rank(ordered[i-1]), ordered[i], rank(ordered[i]))
		}
	}
	if rank(Severity("Bogus")) != -1 {
		t.Errorf("rank(unknown) = %d, want -1", rank(Severity("Bogus")))
	}
}

func TestParseSeverityNamesTheSixInAnyCase(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]Severity{
		"debug": SeverityDebug, "Info": SeverityInfo, "NOTICE": SeverityNotice,
		" warning ": SeverityWarning, "error": SeverityError, "Critical": SeverityCritical,
	} {
		got, err := ParseSeverity(in)
		if err != nil || got != want {
			t.Errorf("ParseSeverity(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "warn", "fatal", "Debugx"} {
		if got, err := ParseSeverity(bad); err == nil || got != "" {
			t.Errorf("ParseSeverity(%q) = %q, %v; want an error and no severity", bad, got, err)
		}
	}
}
