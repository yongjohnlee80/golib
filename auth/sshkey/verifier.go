package sshkey

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Verifier checks an SSHSIG signature. It returns nil only when the signature
// is valid over message under namespace AND identity is allowed to sign.
//
// Two implementations ship: [OpenSSH], which delegates to `ssh-keygen -Y
// verify`, and [PureGo], which parses the envelope in-process. OpenSSH is the
// default because it is the reference implementation and it brings
// allowed_signers semantics — principals, validity windows, cert-authority
// lines — that an in-process parser would have to reimplement to match.
type Verifier interface {
	VerifySignature(ctx context.Context, message, armoredSig []byte, namespace, identity string) error
}

var (
	// ErrIdentity covers a claimed identity that is unusable or not allowed.
	ErrIdentity = errors.New("sshkey: identity not allowed")

	// ErrVerifierUnavailable means the verifier cannot run at all — a missing
	// ssh-keygen, an unreadable allowed_signers file. It is deliberately
	// distinct from a failed verification so an operator can tell
	// "misconfigured" from "rejected".
	ErrVerifierUnavailable = errors.New("sshkey: verifier unavailable")
)

// --- OpenSSH: delegate to the reference implementation ----------------------

// OpenSSH verifies through `ssh-keygen -Y verify`, passing the message on
// stdin. Measured behavior (OpenSSH 10.3): exit 0 on a good signature, 255 on
// every failure — wrong namespace, tampered message, unknown identity, empty
// allowed_signers — so the decision is exit-code driven and no output parsing
// is involved.
type OpenSSH struct {
	// AllowedSigners is the path to an OpenSSH allowed_signers file. OpenSSH
	// owns its full semantics; we do not reimplement them.
	AllowedSigners string

	// Binary is the ssh-keygen to run. Empty means look up "ssh-keygen" on PATH.
	Binary string

	// Timeout bounds the subprocess so a wedged verifier cannot stall an
	// attach. Zero means 5s.
	Timeout time.Duration
}

// VerifySignature writes the signature to a private temporary file — ssh-keygen
// requires -s to be a path — pipes the message on stdin, and reports the exit
// status.
func (o OpenSSH) VerifySignature(ctx context.Context, message, armoredSig []byte, namespace, identity string) error {
	if err := validIdentity(identity); err != nil {
		return err
	}
	if o.AllowedSigners == "" {
		return fmt.Errorf("%w: no allowed_signers path configured", ErrVerifierUnavailable)
	}
	if _, err := os.Stat(o.AllowedSigners); err != nil {
		return fmt.Errorf("%w: allowed_signers: %v", ErrVerifierUnavailable, err)
	}
	bin := o.Binary
	if bin == "" {
		found, err := exec.LookPath("ssh-keygen")
		if err != nil {
			return fmt.Errorf("%w: ssh-keygen not found: %v", ErrVerifierUnavailable, err)
		}
		bin = found
	}

	dir, err := os.MkdirTemp("", "sshkey-verify-")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrVerifierUnavailable, err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("%w: %v", ErrVerifierUnavailable, err)
	}
	sigPath := filepath.Join(dir, "sig")
	if err := os.WriteFile(sigPath, armoredSig, 0o600); err != nil {
		return fmt.Errorf("%w: %v", ErrVerifierUnavailable, err)
	}

	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// argv only — never a shell — so nothing in identity or namespace can be
	// interpreted by an interpreter that is not there. validIdentity above also
	// refuses a leading '-' so a value can never look like a flag.
	cmd := exec.CommandContext(ctx, bin,
		"-Y", "verify",
		"-f", o.AllowedSigners,
		"-I", identity,
		"-n", namespace,
		"-s", sigPath,
	)
	cmd.Stdin = bytes.NewReader(message)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = nil

	// WaitDelay is what actually makes the timeout above bind.
	//
	// Because Stderr is a Writer rather than a File, os/exec plumbs a pipe and
	// copies in a goroutine, and Wait blocks until that copy ends. Cancelling
	// the context kills ssh-keygen but NOT anything it left behind holding the
	// write end, so without a WaitDelay a wedged descendant keeps Run blocked
	// long past the deadline — measured at the full sleep duration, deadline
	// ignored. WaitDelay closes the pipes and gives up.
	cmd.WaitDelay = time.Second

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: ssh-keygen timed out", ErrVerifierUnavailable)
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// A non-zero exit is a REJECTION, not a malfunction. The detail goes
			// nowhere near the caller; auth.Policy returns ErrUnauthenticated.
			return fmt.Errorf("%w: ssh-keygen: %s", ErrBadSignature, firstLine(stderr.String()))
		}
		return fmt.Errorf("%w: %v", ErrVerifierUnavailable, err)
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// validIdentity screens a CLAIMED identity before it reaches argv.
//
// The value is untrusted until verification succeeds, and although exec.Command
// uses argv rather than a shell, a value beginning with '-' could still be read
// as an option by some tool. Control characters and absurd lengths are refused
// on principle.
func validIdentity(identity string) error {
	switch {
	case identity == "":
		return fmt.Errorf("%w: empty identity", ErrIdentity)
	case len(identity) > 256:
		return fmt.Errorf("%w: identity too long", ErrIdentity)
	case strings.HasPrefix(identity, "-"):
		return fmt.Errorf("%w: identity may not begin with '-'", ErrIdentity)
	}
	for _, r := range identity {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: identity contains a control character", ErrIdentity)
		}
	}
	return nil
}

// --- PureGo: in-process envelope parsing -----------------------------------

// PureGo verifies in-process, using the SSHSIG parser in this package and
// x/crypto's signature verification. It exists for environments without an
// OpenSSH binary — a scratch container, a distroless image.
//
// It is deliberately semantically INTERCHANGEABLE with [OpenSSH]: the claimed
// identity selects the keys, and the signature must verify against one of them.
// The two differ in reach, not in meaning — PureGo understands a plain
// allowed-key set, while OpenSSH additionally honors the parts of
// allowed_signers this package does not model (validity windows,
// cert-authority lines, namespace patterns).
type PureGo struct {
	// Allowed is the allowed-key set. Several entries may share a Subject; a
	// principal with three keys is ordinary.
	Allowed []Allowed
}

// VerifySignature parses the envelope, requires the namespace to match, requires
// the signing key to belong to the claimed identity, then verifies.
func (p PureGo) VerifySignature(_ context.Context, message, armoredSig []byte, namespace, identity string) error {
	if err := validIdentity(identity); err != nil {
		return err
	}
	if len(p.Allowed) == 0 {
		// An empty allowlist denies. It never means "allow everyone".
		return ErrNoAllowedKeys
	}
	env, err := parseEnvelope(armoredSig)
	if err != nil {
		return err
	}
	if env.Namespace != namespace {
		return ErrNamespace
	}
	if !p.permits(identity, env.PublicKey) {
		// One error for "no such identity" and "identity does not hold that
		// key": distinguishing them would tell a prober which principals exist.
		return fmt.Errorf("%w: %q may not sign with the presented key", ErrIdentity, identity)
	}
	return env.verify(message)
}

// permits reports whether identity holds the presented key.
func (p PureGo) permits(identity string, pub ssh.PublicKey) bool {
	presented := pub.Marshal()
	found := false
	for _, a := range p.Allowed {
		// No early break: the loop cost stays independent of WHERE a match sits
		// in the set, and of whether one exists at all.
		if a.Subject == identity && keysEqual(a.Key.Marshal(), presented) {
			found = true
		}
	}
	return found
}

// compile-time proof both verifiers satisfy the interface
var (
	_ Verifier = OpenSSH{}
	_ Verifier = PureGo{}
)
