package covercheck

import (
	"errors"
	"strings"
	"testing"
)

func TestParseProfile(t *testing.T) {
	t.Parallel()
	profile, err := ParseProfile(strings.NewReader(`mode: atomic
example.com/project/pkg/a.go:10.2,12.3 2 0
example.com/project/pkg/a.go:10.2,12.3 2 4
example.com/project/pkg/b.go:2.1,2.9 1 1
`), WithModulePath("example.com/project"))
	if err != nil {
		t.Fatal(err)
	}
	if profile.Mode != ModeAtomic {
		t.Fatalf("mode = %q, want %q", profile.Mode, ModeAtomic)
	}
	if got := len(profile.Files["pkg/a.go"]); got != 1 {
		t.Fatalf("duplicate blocks = %d, want 1", got)
	}
	if got := profile.Files["pkg/a.go"][0].Count; got != 4 {
		t.Fatalf("merged count = %d, want 4", got)
	}
}

func TestParseProfileRejectsMalformedInput(t *testing.T) {
	t.Parallel()
	tests := []string{
		"",
		"mode: branch\n",
		"mode: set\nexample.com/p/a.go:nope 1 1\n",
		"mode: count\nexample.com/p/a.go:1.1,2.2 x 1\n",
		"mode: atomic\nexample.com/p/a.go:1.1,2.2 1 -1\n",
	}
	for _, input := range tests {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := ParseProfile(strings.NewReader(input), WithModulePath("example.com/p"))
			if !errors.Is(err, ErrInvalidProfile) {
				t.Fatalf("error = %v, want ErrInvalidProfile", err)
			}
		})
	}
}

func TestParseProfileRejectsOtherModule(t *testing.T) {
	t.Parallel()
	_, err := ParseProfile(strings.NewReader("mode: set\ngithub.com/other/p/a.go:1.1,1.2 1 1\n"), WithModulePath("example.com/p"))
	if !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("error = %v, want ErrInvalidProfile", err)
	}
}
