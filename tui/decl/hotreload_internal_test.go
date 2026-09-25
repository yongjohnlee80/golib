package decl

import "testing"

// TestTheDebounceReloadsOnceTheFilesHoldStill drives the follower one poll at
// a time, with no clock: a change is acted on at the poll that sees it
// unchanged, never at the poll that first sees it.
func TestTheDebounceReloadsOnceTheFilesHoldStill(t *testing.T) {
	a, b, c := snap("a"), snap("b"), snap("c")
	for name, tc := range map[string]struct {
		polls []snapshot
		want  []bool
	}{
		"unchanged":          {[]snapshot{a, a, a}, []bool{false, false, false}},
		"one change":         {[]snapshot{b, b, b}, []bool{false, true, false}},
		"still being saved":  {[]snapshot{b, c, c}, []bool{false, false, true}},
		"changed back":       {[]snapshot{b, a, a}, []bool{false, false, false}},
		"two edits in a row": {[]snapshot{b, b, c, c}, []bool{false, true, false, true}},
	} {
		f := follower{applied: a}
		for i, now := range tc.polls {
			if got := f.step(now); got != tc.want[i] {
				t.Errorf("%s: poll %d reloaded = %v, want %v", name, i, got, tc.want[i])
			}
		}
	}
}

func snap(content string) snapshot {
	var h [32]byte
	copy(h[:], content)
	return snapshot{"layout:main.qml": h}
}
