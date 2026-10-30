package sshkey

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/yongjohnlee80/golib/errs"
)

// TestAKeyWithOptionsIsRefused: a key line with options (from=, expiry-time=, …) is refused, not
// taken with its restrictions dropped; the same key without them is taken.
func TestAKeyWithOptionsIsRefused(t *testing.T) {
	_, pub := newSigner(t)
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " alice\n"
	for _, opts := range []string{`from="10.0.0.0/8" `, `expiry-time="20200101" `, `no-pty,command="true" `} {
		_, err := ParseAuthorizedKeys([]byte(opts+line), SubjectFromComment)
		if err == nil || !errors.Is(err, errs.ErrInvalidArgument) || !strings.Contains(err.Error(), "key options") {
			t.Errorf("%s: %v, want the options refused", opts, err)
		}
	}
	got, err := ParseAuthorizedKeys([]byte(line), SubjectFromComment)
	if err != nil || len(got) != 1 || got[0].Subject != "alice" {
		t.Errorf("the plain line: %v %v", got, err)
	}
}
