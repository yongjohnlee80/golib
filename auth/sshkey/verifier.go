package sshkey

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
//
// Both honor ctx: an already-cancelled context is refused before any work, so
// the two cannot disagree on admission merely because one of them ignored it.
type Verifier interface {
	VerifySignature(ctx context.Context, message, armoredSig []byte, namespace, identity string) error
}

var (
	// ErrIdentity covers a claimed identity that is unusable or not allowed.
	ErrIdentity = errors.New("sshkey: identity not allowed")

	// ErrVerifierUnavailable means the verifier could not reach a verdict — a
	// missing ssh-keygen, an unreadable allowed_signers, a cancelled context, a
	// subprocess that had to be killed. It is deliberately distinct from a
	// failed verification so an operator can tell "misconfigured" from
	// "rejected", and so a caller never reads an outage as a bad credential.
	ErrVerifierUnavailable = errors.New("sshkey: verifier unavailable")
)

// --- OpenSSH: delegate to the reference implementation ----------------------

// OpenSSH verifies through `ssh-keygen -Y verify`, passing the message on
// stdin. Measured behavior (OpenSSH 10.3): exit 0 on a good signature, 255 on
// every failure — wrong namespace, tampered message, unknown identity, empty
// allowed_signers — so the decision is exit-code driven and no output is parsed.
//
// Build one with [NewOpenSSH]. The zero value is not usable: a verifier whose
// configuration is only discovered to be wrong at the first login is a
// misconfiguration that surfaces during an incident instead of at startup.
type OpenSSH struct {
	allowedSigners string
	binary         string
	timeout        time.Duration
}

// OpenSSHOption configures [NewOpenSSH].
type OpenSSHOption func(*OpenSSH)

// Binary sets an explicit ssh-keygen path, skipping the PATH lookup.
//
// RECOMMENDED for hardened deployments. PATH is deployment-supplied trust; Go
// refuses a current-directory-relative lookup result, but an absolute path
// chosen by you removes the question.
func Binary(path string) OpenSSHOption { return func(o *OpenSSH) { o.binary = path } }

// VerifyTimeout bounds one subprocess. Zero keeps the 5s default.
func VerifyTimeout(d time.Duration) OpenSSHOption { return func(o *OpenSSH) { o.timeout = d } }

// DefaultVerifyTimeout bounds a single ssh-keygen invocation.
const DefaultVerifyTimeout = 5 * time.Second

// waitDelay is the final I/O bound, applied AFTER whole-process-group
// cancellation rather than instead of it. Cancellation kills the tree; this only
// covers the window where a pipe is still draining.
const waitDelay = time.Second

// maxStderr caps what is captured from a subprocess. ssh-keygen emits one line;
// an unbounded buffer would let a wedged or wrong binary spool into memory.
const maxStderr = 4 << 10

// NewOpenSSH builds a delegating verifier over an OpenSSH allowed_signers file.
//
// Both the binary and the allowed_signers file are resolved and checked HERE, so
// a broken deployment fails at construction rather than at someone's first
// login. The file is re-checked per call as well, because it can be replaced
// underneath a long-running process.
func NewOpenSSH(allowedSigners string, opts ...OpenSSHOption) (*OpenSSH, error) {
	o := &OpenSSH{allowedSigners: allowedSigners, timeout: DefaultVerifyTimeout}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	if o.allowedSigners == "" {
		return nil, fmt.Errorf("%w: no allowed_signers path", ErrVerifierUnavailable)
	}
	if o.timeout <= 0 {
		o.timeout = DefaultVerifyTimeout
	}
	if o.binary == "" {
		found, err := exec.LookPath("ssh-keygen")
		if err != nil {
			return nil, fmt.Errorf("%w: ssh-keygen not found on PATH: %v", ErrVerifierUnavailable, err)
		}
		o.binary = found // resolved once; PATH is not re-consulted per attempt
	}
	if err := readablePolicy(o.binary); err != nil {
		return nil, fmt.Errorf("%w: ssh-keygen %q: %v", ErrVerifierUnavailable, o.binary, err)
	}
	if err := readablePolicy(o.allowedSigners); err != nil {
		return nil, fmt.Errorf("%w: allowed_signers %q: %v", ErrVerifierUnavailable, o.allowedSigners, err)
	}
	return o, nil
}

// readablePolicy proves a path is a regular file this process can actually READ.
//
// os.Stat is not enough: it succeeds on a mode-000 file and on a directory, and
// the failure then arrives as a nonzero ssh-keygen exit — indistinguishable from
// a rejected signature. Opening is the only check that answers the question
// being asked.
func readablePolicy(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.IsDir() {
		return errors.New("is a directory")
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("not a regular file (mode %s)", st.Mode())
	}
	return nil
}

// VerifySignature writes the signature to a private temporary file — ssh-keygen
// requires -s to be a path — pipes the message on stdin, and reports the exit
// status.
func (o *OpenSSH) VerifySignature(ctx context.Context, message, armoredSig []byte, namespace, identity string) error {
	if o == nil || o.binary == "" || o.allowedSigners == "" {
		return fmt.Errorf("%w: OpenSSH verifier was not built with NewOpenSSH", ErrVerifierUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrVerifierUnavailable, err)
	}
	if err := validIdentity(identity); err != nil {
		return err
	}
	// Re-checked per call: the file NewOpenSSH validated can be replaced,
	// chmod'ed or deleted while the process runs. This narrows the window to the
	// gap between here and exec, which cannot be closed from outside the kernel
	// — see the ADR. Anything landing inside that gap surfaces as a nonzero exit
	// and is reported as a rejection; a subsequent attempt reports it correctly.
	if err := readablePolicy(o.allowedSigners); err != nil {
		return fmt.Errorf("%w: allowed_signers: %v", ErrVerifierUnavailable, err)
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

	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	// argv only — never a shell — so nothing in identity or namespace can be
	// interpreted by an interpreter that is not there. validIdentity above also
	// refuses a leading '-' so a value can never read as a flag.
	cmd := exec.CommandContext(ctx, o.binary,
		"-Y", "verify",
		"-f", o.allowedSigners,
		"-I", identity,
		"-n", namespace,
		"-s", sigPath,
	)
	cmd.Stdin = bytes.NewReader(message)
	stderr := &capped{limit: maxStderr}
	cmd.Stderr = stderr
	cmd.Stdout = nil

	// Cancellation must reach the whole PROCESS TREE, not just ssh-keygen.
	//
	// CommandContext's default Cancel kills cmd.Process alone, and WaitDelay
	// only closes our end of the pipes. Neither reaps a descendant: measured on
	// the timeout fixture, the call returned in ~1.1s and the grandchild was
	// still running afterwards, so repeated timeouts would accumulate processes.
	// isolateProcessGroup puts the child in its own group and kills the group.
	isolateProcessGroup(cmd)
	cmd.WaitDelay = waitDelay

	runErr := cmd.Run()
	// Belt and braces: even on a clean exit, anything the child left behind in
	// its group dies here. On success the group is already empty and this is a
	// no-op. Our own process is never in that group — the group id IS the
	// child's pid — so this can never signal us.
	reapProcessGroup(cmd)

	if runErr != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: ssh-keygen timed out after %s", ErrVerifierUnavailable, o.timeout)
		}
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			// A non-zero exit is a REJECTION, not a malfunction. The detail goes
			// nowhere near the caller; auth.Policy returns ErrUnauthenticated.
			return fmt.Errorf("%w: ssh-keygen: %s", ErrBadSignature, firstLine(stderr.String()))
		}
		// Could not run it at all, or WaitDelay expired: no verdict was reached.
		return fmt.Errorf("%w: %v", ErrVerifierUnavailable, runErr)
	}
	return nil
}

// capped is a bounded io.Writer: it keeps the first limit bytes and counts the
// rest away.
type capped struct {
	limit int
	buf   bytes.Buffer
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.limit - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil // never fail the pipe; the output is diagnostic only
}

func (c *capped) String() string { return c.buf.String() }

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
//
// Build one with [NewPureGo]. The zero value denies everything.
type PureGo struct {
	allowed []Allowed
}

// NewPureGo builds an in-process verifier over an allowed-key set.
//
// An empty set is a construction error, not a runtime denial: a verifier that
// can never admit anyone is a misconfiguration, and golib fails those at
// construction. The slice is cloned, so a caller mutating theirs afterwards
// cannot silently change who may log in.
func NewPureGo(allowed []Allowed) (*PureGo, error) {
	if len(allowed) == 0 {
		return nil, fmt.Errorf("%w: empty allowed-key set", ErrNoAllowedKeys)
	}
	for i, a := range allowed {
		if a.Key == nil {
			return nil, fmt.Errorf("%w: entry %d has no key", ErrNoAllowedKeys, i)
		}
		if a.Subject == "" {
			return nil, fmt.Errorf("%w: entry %d has no subject", ErrNoAllowedKeys, i)
		}
	}
	return &PureGo{allowed: slices.Clone(allowed)}, nil
}

// VerifySignature parses the envelope, requires the namespace to match, requires
// the signing key to belong to the claimed identity, then verifies.
func (p *PureGo) VerifySignature(ctx context.Context, message, armoredSig []byte, namespace, identity string) error {
	// Checked for the same reason OpenSSH checks it: if one verifier honored
	// cancellation and the other did not, an already-cancelled call would be
	// admitted by one and refused by the other — a disagreement about admission,
	// which is the one thing they may never disagree about.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrVerifierUnavailable, err)
	}
	if err := validIdentity(identity); err != nil {
		return err
	}
	if p == nil || len(p.allowed) == 0 {
		// Unreachable via NewPureGo; reachable via a zero PureGo{}. An empty
		// allowlist denies. It never means "allow everyone".
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
	if err := env.verify(message); err != nil {
		return err
	}
	// Re-checked after the work: a context cancelled mid-derivation must not
	// yield an admission, matching what OpenSSH does when its subprocess is
	// killed part-way.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrVerifierUnavailable, err)
	}
	return nil
}

// permits reports whether identity holds the presented key.
func (p *PureGo) permits(identity string, pub ssh.PublicKey) bool {
	presented := pub.Marshal()
	found := false
	for _, a := range p.allowed {
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
	_ Verifier = (*OpenSSH)(nil)
	_ Verifier = (*PureGo)(nil)
)
