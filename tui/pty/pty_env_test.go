package pty

import (
	"slices"
	"testing"
)

// TestEachNameKeepsItsLastSetting: a name set more than once keeps its last setting alone, the
// others in their order; case-blind, Path and PATH are one name, else two; Windows' per-drive
// entries ("=C:=...") are names of their own; an entry with no "=" is a name too.
func TestEachNameKeepsItsLastSetting(t *testing.T) {
	env := []string{"HOME=/inherited", "Path=a", "=C:=C:\\x", "KEEP=1", "HOME=/own", "PATH=b", "=D:=D:\\y", "BARE"}
	if got, want := lastSettingWins(env, false), []string{"Path=a", "=C:=C:\\x", "KEEP=1", "HOME=/own", "PATH=b", "=D:=D:\\y", "BARE"}; !slices.Equal(got, want) {
		t.Errorf("case-sensitive: %q, want %q", got, want)
	}
	if got, want := lastSettingWins(env, true), []string{"=C:=C:\\x", "KEEP=1", "HOME=/own", "PATH=b", "=D:=D:\\y", "BARE"}; !slices.Equal(got, want) {
		t.Errorf("case-blind: %q, want %q", got, want)
	}
	if got := lastSettingWins(nil, true); len(got) != 0 {
		t.Errorf("none: %q", got)
	}
}
