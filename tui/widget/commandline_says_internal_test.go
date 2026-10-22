package widget

import (
	"errors"
	"fmt"
	"testing"
)

// The prompt says the command line's own refusals in the user's words, never their package name
// or errs kind, and any other error as it reads itself, a host's included, whatever it starts with.
func TestThePromptSaysARefusalInTheUsersWords(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"an unknown verb", NewEditorCore().RunCommand("frob"), "not an editor command: frob"},
		{"no writer", ErrNoWriter, "this editor cannot save"},
		{"no quit", ErrNoQuit, "this editor cannot quit"},
		{"read-only, wrapped by a host command", fmt.Errorf("saving: %w", ErrReadOnly), "the editor is read-only"},
		{"a host's error", errors.New("disk full"), "disk full"},
		{"a host's error naming widget", errors.New("widget: rejected by the host"), "widget: rejected by the host"},
	} {
		if got := promptSays(c.err); got != c.want {
			t.Errorf("%s: the prompt says %q, want %q", c.name, got, c.want)
		}
	}
}
