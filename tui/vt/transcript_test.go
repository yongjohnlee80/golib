package vt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestTranscripts replays each recorded program step by step and compares
// the screen and cursor with what tmux showed for the same bytes (see
// record_test.go).
func TestTranscripts(t *testing.T) {
	bins, _ := filepath.Glob(filepath.Join("testdata", "transcripts", "*.bin"))
	if len(bins) == 0 {
		t.Fatal("no transcripts recorded")
	}
	byName := map[string][]string{}
	for _, b := range bins {
		base := strings.TrimSuffix(filepath.Base(b), ".bin")
		name := base[:strings.LastIndex(base, ".")]
		byName[name] = append(byName[name], b)
	}
	for name, steps := range byName {
		sort.Slice(steps, func(i, j int) bool { return stepNum(steps[i]) < stepNum(steps[j]) })
		t.Run(name, func(t *testing.T) {
			s := New(recRows, recCols)
			for _, b := range steps {
				in, err := os.ReadFile(b)
				if err != nil {
					t.Fatal(err)
				}
				s.Write(in)
				want, err := os.ReadFile(strings.TrimSuffix(b, ".bin") + ".screen")
				if err != nil {
					t.Fatal(err)
				}
				compareScreen(t, filepath.Base(b), s, string(want))
			}
		})
	}
}

func stepNum(path string) int {
	base := strings.TrimSuffix(filepath.Base(path), ".bin")
	n, _ := strconv.Atoi(base[strings.LastIndex(base, ".")+1:])
	return n
}

// compareScreen checks s against a recorded reference: "row col" on the
// first line, then the rows' text.
func compareScreen(t *testing.T, step string, s *Screen, ref string) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(ref, "\n"), "\n")
	var row, col int
	if _, err := fmt.Sscan(lines[0], &row, &col); err != nil {
		t.Fatalf("%s: bad reference header %q", step, lines[0])
	}
	want := lines[1:]
	for len(want) < recRows {
		want = append(want, "")
	}
	got := screenRows(s)
	for i := range got {
		if got[i] != strings.TrimRight(want[i], " ") {
			t.Errorf("%s row %d\n got: %q\nwant: %q", step, i, got[i], want[i])
		}
	}
	if r, c, _, _ := s.Cursor(); r != row || c != col {
		t.Errorf("%s: cursor (%d, %d), tmux (%d, %d)", step, r, c, row, col)
	}
}
