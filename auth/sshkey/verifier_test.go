package sshkey

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// keypair is a real ssh-keygen-generated key on disk.
type keypair struct {
	priv    string
	pubLine string // the authorized_keys/allowed_signers key portion
	comment string
}

func sshKeygen(t *testing.T) string {
	t.Helper()
	requireDelegation(t)
	bin, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not on PATH; skipping OpenSSH-dependent test")
	}
	return bin
}

// requireDelegation skips a test that needs NewOpenSSH to SUCCEED.
//
// On a platform without POSIX process groups the constructor refuses by design,
// so a test asserting successful construction is asserting the wrong thing
// there. Tests that assert construction FAILURE need no guard: a platform
// refusal is also an ErrVerifierUnavailable.
func requireDelegation(t *testing.T) {
	t.Helper()
	if err := supportsProcessGroups(); err != nil {
		t.Skipf("the delegating verifier is unsupported here: %v", err)
	}
}

func genKey(t *testing.T, bin, dir, comment string) keypair {
	t.Helper()
	priv := filepath.Join(dir, "id_"+strings.NewReplacer("@", "_", ".", "_").Replace(comment))
	if out, err := exec.Command(bin, "-t", "ed25519", "-N", "", "-C", comment, "-f", priv).CombinedOutput(); err != nil {
		t.Fatalf("keygen %s: %v\n%s", comment, err, out)
	}
	pub, err := os.ReadFile(priv + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	// "ssh-ed25519 AAAA... comment" -> keep just type+base64; allowed_signers
	// puts principals in front, so the comment cannot come along.
	f := strings.Fields(strings.TrimSpace(string(pub)))
	if len(f) < 2 {
		t.Fatalf("unexpected public key shape: %q", pub)
	}
	return keypair{priv: priv, pubLine: f[0] + " " + f[1], comment: comment}
}

// signWith produces a real SSHSIG armor over msg under namespace.
func signWith(t *testing.T, bin, dir string, kp keypair, namespace string, msg []byte) []byte {
	t.Helper()
	msgPath := filepath.Join(dir, "msg-"+kp.comment+"-"+namespace)
	if err := os.WriteFile(msgPath, msg, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "-Y", "sign", "-f", kp.priv, "-n", namespace, msgPath).CombinedOutput(); err != nil {
		t.Fatalf("sign: %v\n%s", err, out)
	}
	armor, err := os.ReadFile(msgPath + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	return armor
}

// allowedSigners writes an OpenSSH allowed_signers file binding each principal
// to a key, and returns the equivalent in-process allowed set. Building both
// from ONE source is the point: a divergence between the two verifiers can then
// only come from the verifiers, never from mismatched fixtures.
func allowedSigners(t *testing.T, dir string, pairs [][2]string) (string, []Allowed) {
	t.Helper()
	var lines []string
	var set []Allowed
	for _, p := range pairs {
		principal, pubLine := p[0], p[1]
		lines = append(lines, principal+" "+pubLine)
		parsed, err := ParseAuthorizedKeys([]byte(pubLine+" "+principal+"\n"), SubjectFromComment)
		if err != nil {
			t.Fatalf("parse %q: %v", pubLine, err)
		}
		set = append(set, parsed...)
	}
	path := filepath.Join(dir, fmt.Sprintf("allowed_signers_%d", len(pairs)))
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, set
}

// TestVerifiers_Agree is the load-bearing test for ADR-0001 §2.5's
// substitutability claim: swapping OpenSSH for PureGo must not change WHO gets
// in. The sentinel errors legitimately differ — ssh-keygen reports one exit
// status for every failure — so the assertion is on the accept/reject decision,
// which is the only part an authentication outcome depends on.
func TestVerifiers_Agree(t *testing.T) {
	bin := sshKeygen(t)
	dir := t.TempDir()
	alice := genKey(t, bin, dir, "alice@host")
	bob := genKey(t, bin, dir, "bob@host")

	// Only alice is in the allowlist. bob is a stranger holding a valid key.
	signersPath, allowedSet := allowedSigners(t, dir, [][2]string{{alice.comment, alice.pubLine}})

	const msg = "the exact bytes under signature"
	aliceSig := signWith(t, bin, dir, alice, testNS, []byte(msg))
	bobSig := signWith(t, bin, dir, bob, testNS, []byte(msg))
	aliceOtherNS := signWith(t, bin, dir, alice, "git", []byte(msg))

	cases := []struct {
		name    string
		sig     []byte
		message string
		claim   string
		wantOK  bool
	}{
		{"valid", aliceSig, msg, alice.comment, true},
		{"tampered message", aliceSig, msg + "!", alice.comment, false},
		{"wrong namespace at signing", aliceOtherNS, msg, alice.comment, false},
		{"signer not in the allowlist", bobSig, msg, bob.comment, false},
		{"allowlisted principal, stranger's key", bobSig, msg, alice.comment, false},
		{"allowlisted key, unknown principal claimed", aliceSig, msg, "carol@host", false},
		{"garbage signature", []byte("-----BEGIN SSH SIGNATURE-----\nZm9v\n-----END SSH SIGNATURE-----\n"), msg, alice.comment, false},
	}

	openssh, err := NewOpenSSH(signersPath, Binary(bin))
	if err != nil {
		t.Fatal(err)
	}
	pureGo, err := NewPureGo(allowedSet)
	if err != nil {
		t.Fatal(err)
	}
	verifiers := map[string]Verifier{"OpenSSH": openssh, "PureGo": pureGo}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for name, v := range verifiers {
				err := v.VerifySignature(context.Background(), []byte(c.message), c.sig, testNS, c.claim)
				if c.wantOK && err != nil {
					t.Errorf("%s: rejected a signature the other accepts: %v", name, err)
				}
				if !c.wantOK && err == nil {
					t.Errorf("%s: ACCEPTED %s — the verifiers disagree on who gets in", name, c.name)
				}
			}
		})
	}
}

// The Factor must behave identically whichever verifier backs it, including the
// Subject it reports.
func TestFactor_OverOpenSSHVerifier(t *testing.T) {
	bin := sshKeygen(t)
	dir := t.TempDir()
	alice := genKey(t, bin, dir, "alice@host")
	signersPath, _ := allowedSigners(t, dir, [][2]string{{alice.comment, alice.pubLine}})

	v, err := NewOpenSSH(signersPath, Binary(bin))
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemStore(0)
	f := New(v, store, Namespace(testNS))
	ch, err := NewChallenger(store, time.Minute).Issue(Binding{})
	if err != nil {
		t.Fatal(err)
	}
	armor := signWith(t, bin, dir, alice, testNS, ch.Message)

	got, err := f.Verify(context.Background(), present(ch.ID, armor, map[string]string{"ssh-identity": alice.comment}))
	if err != nil {
		t.Fatalf("delegated verification of a genuine signature failed: %v", err)
	}
	if got.Subject != alice.comment {
		t.Errorf("Subject = %q, want %q", got.Subject, alice.comment)
	}
	if got.Method != "sshkey" {
		t.Errorf("Method = %q", got.Method)
	}

	// The nonce is spent, so the very same signature cannot be replayed.
	_, err = f.Verify(context.Background(), present(ch.ID, armor, map[string]string{"ssh-identity": alice.comment}))
	if !errors.Is(err, ErrChallengeUnknown) {
		t.Errorf("replay err = %v, want ErrChallengeUnknown", err)
	}
}

// An empty allowed_signers denies. Measured: ssh-keygen exits 255.
func TestOpenSSH_EmptyAllowedSignersDenies(t *testing.T) {
	bin := sshKeygen(t)
	dir := t.TempDir()
	alice := genKey(t, bin, dir, "alice@host")
	empty := filepath.Join(dir, "empty_signers")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sig := signWith(t, bin, dir, alice, testNS, []byte("m"))
	v, err := NewOpenSSH(empty, Binary(bin))
	if err != nil {
		t.Fatalf("an empty allowed_signers is readable and must construct: %v", err)
	}
	err = v.VerifySignature(context.Background(), []byte("m"), sig, testNS, alice.comment)
	if !errors.Is(err, ErrBadSignature) {
		t.Errorf("err = %v, want ErrBadSignature (an empty allowlist denies)", err)
	}
}

// Misconfiguration must fail at CONSTRUCTION, so a broken deployment surfaces at
// startup rather than during an incident — and must never be reachable as
// ErrBadSignature, which would report an outage as a bad credential.
func TestNewOpenSSH_MisconfigurationFailsAtConstruction(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := filepath.Join(dir, "signers")
	if err := os.WriteFile(good, []byte("x ssh-ed25519 AAAA\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A file that EXISTS but cannot be read. os.Stat succeeds on this; only
	// opening it reveals the problem, and without that check the failure arrives
	// later as a nonzero ssh-keygen exit — i.e. as a rejected signature.
	unreadable := filepath.Join(dir, "unreadable")
	if err := os.WriteFile(unreadable, []byte("x ssh-ed25519 AAAA\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(dir, "adir")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}

	cases := map[string][]any{
		"no allowed_signers path":        {"", Binary("/bin/true")},
		"allowed_signers missing":        {filepath.Join(dir, "nope"), Binary("/bin/true")},
		"allowed_signers is a directory": {subdir, Binary("/bin/true")},
		"binary missing":                 {good, Binary(filepath.Join(dir, "no-such-ssh-keygen"))},
		"binary is a directory":          {good, Binary(subdir)},
	}
	if os.Geteuid() != 0 {
		// root reads mode-000 files, so the case is meaningless there.
		cases["allowed_signers unreadable"] = []any{unreadable, Binary("/bin/true")}
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v, err := NewOpenSSH(c[0].(string), c[1].(OpenSSHOption))
			if err == nil {
				t.Fatalf("NewOpenSSH accepted a broken configuration (%v)", v)
			}
			if !errors.Is(err, ErrVerifierUnavailable) {
				t.Errorf("err = %v, want ErrVerifierUnavailable", err)
			}
			if errors.Is(err, ErrBadSignature) {
				t.Error("a misconfiguration must never present as a failed signature")
			}
		})
	}
}

// A configuration that breaks AFTER construction — the file is deleted or
// chmod'ed while the process runs — is still not a rejected credential.
func TestOpenSSH_PolicyBreaksAfterConstruction(t *testing.T) {
	t.Parallel()
	requireDelegation(t)
	dir := t.TempDir()
	signers := filepath.Join(dir, "signers")
	if err := os.WriteFile(signers, []byte("x ssh-ed25519 AAAA\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := NewOpenSSH(signers, Binary("/bin/true"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(signers); err != nil {
		t.Fatal(err)
	}
	err = v.VerifySignature(context.Background(), []byte("m"), []byte("sig"), testNS, "alice")
	if !errors.Is(err, ErrVerifierUnavailable) {
		t.Errorf("err = %v, want ErrVerifierUnavailable", err)
	}
}

// A zero-value verifier must deny rather than half-work.
func TestZeroVerifiersDeny(t *testing.T) {
	t.Parallel()
	if err := (&OpenSSH{}).VerifySignature(context.Background(), nil, nil, testNS, "alice"); !errors.Is(err, ErrVerifierUnavailable) {
		t.Errorf("zero OpenSSH err = %v, want ErrVerifierUnavailable", err)
	}
	if err := (&PureGo{}).VerifySignature(context.Background(), nil, nil, testNS, "alice"); !errors.Is(err, ErrNoAllowedKeys) {
		t.Errorf("zero PureGo err = %v, want ErrNoAllowedKeys", err)
	}
}

// An already-cancelled context must be refused by BOTH, or the two disagree
// about admission — the one thing they may never disagree about.
func TestVerifiers_RespectCancellation(t *testing.T) {
	bin := sshKeygen(t)
	dir := t.TempDir()
	alice := genKey(t, bin, dir, "alice@host")
	signersPath, allowedSet := allowedSigners(t, dir, [][2]string{{alice.comment, alice.pubLine}})
	sig := signWith(t, bin, dir, alice, testNS, []byte("m"))

	openssh, err := NewOpenSSH(signersPath, Binary(bin))
	if err != nil {
		t.Fatal(err)
	}
	pureGo, err := NewPureGo(allowedSet)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, v := range map[string]Verifier{"OpenSSH": openssh, "PureGo": pureGo} {
		// The same inputs verify fine under a live context, so only the
		// cancellation can be responsible for the refusal.
		if err := v.VerifySignature(context.Background(), []byte("m"), sig, testNS, alice.comment); err != nil {
			t.Fatalf("%s: fixture does not verify under a live context: %v", name, err)
		}
		if err := v.VerifySignature(ctx, []byte("m"), sig, testNS, alice.comment); !errors.Is(err, ErrVerifierUnavailable) {
			t.Errorf("%s: cancelled err = %v, want ErrVerifierUnavailable", name, err)
		}
	}
}

func TestNewPureGo_Validation(t *testing.T) {
	t.Parallel()
	_, pub := newSigner(t)
	for name, in := range map[string][]Allowed{
		"nil":           nil,
		"empty":         {},
		"entry no key":  {{Subject: "alice"}},
		"entry no subj": {{Key: pub}},
	} {
		if _, err := NewPureGo(in); !errors.Is(err, ErrNoAllowedKeys) {
			t.Errorf("%s: err = %v, want ErrNoAllowedKeys", name, err)
		}
	}

	// The set is cloned: a caller mutating their slice afterwards must not be
	// able to change who may log in.
	set := []Allowed{{Key: pub, Subject: "alice"}}
	v, err := NewPureGo(set)
	if err != nil {
		t.Fatal(err)
	}
	set[0].Subject = "attacker"
	if v.allowed[0].Subject != "alice" {
		t.Error("NewPureGo aliased the caller's slice — the allowlist is mutable from outside")
	}
}

// The claimed identity reaches an argv slot, so it is screened first. Both
// verifiers must screen it the same way, or PureGo would accept a claim OpenSSH
// refuses to even ask about.
func TestValidIdentity(t *testing.T) {
	t.Parallel()

	bad := map[string]string{
		"empty":            "",
		"leading dash":     "-f",
		"embedded newline": "alice\nbob",
		"NUL":              "alice\x00",
		"DEL":              "alice\x7f",
		"too long":         strings.Repeat("a", 257),
	}
	for name, id := range bad {
		t.Run(name, func(t *testing.T) {
			if err := validIdentity(id); !errors.Is(err, ErrIdentity) {
				t.Errorf("validIdentity(%q) = %v, want ErrIdentity", id, err)
			}
			// Both implementations must refuse before doing any work — and
			// before consulting their configuration, so the screen cannot be
			// bypassed by a verifier that happens to be misconfigured.
			pg, pgErr := NewPureGo([]Allowed{{Key: idScreenKey(t), Subject: "alice"}})
			if pgErr != nil {
				t.Fatal(pgErr)
			}
			if err := pg.VerifySignature(context.Background(), nil, nil, testNS, id); !errors.Is(err, ErrIdentity) {
				t.Errorf("PureGo accepted identity %q: %v", id, err)
			}
			if err := (&OpenSSH{allowedSigners: "/etc/hostname", binary: "/bin/true"}).
				VerifySignature(context.Background(), nil, nil, testNS, id); !errors.Is(err, ErrIdentity) {
				t.Errorf("OpenSSH accepted identity %q: %v", id, err)
			}
		})
	}

	for _, id := range []string{"alice", "alice@host", "alice+ci@example.com", "a b", strings.Repeat("a", 256)} {
		if err := validIdentity(id); err != nil {
			t.Errorf("validIdentity(%q) = %v, want nil", id, err)
		}
	}
}

// The Factor must refuse a request that claims nothing, rather than inventing a
// subject for it.
func TestFactor_RequiresIdentityClaim(t *testing.T) {
	t.Parallel()
	_, pub := newSigner(t)
	f, c, _ := harness(t, pub, "alice")
	ch, _ := c.Issue(Binding{})

	r := present(ch.ID, []byte("sig"), nil)
	delete(r.Credentials, "ssh-identity")
	if _, err := f.Verify(context.Background(), r); !errors.Is(err, ErrNoIdentity) {
		t.Errorf("err = %v, want ErrNoIdentity", err)
	}
}

func TestNew_RequiresVerifierAndStore(t *testing.T) {
	t.Parallel()

	t.Run("nil verifier panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("New with a nil Verifier must panic rather than pick one silently")
			}
		}()
		_ = New(nil, NewMemStore(0), Namespace(testNS))
	})
	t.Run("nil store panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("New with a nil ChallengeStore must panic")
			}
		}()
		_ = New(&PureGo{}, nil, Namespace(testNS))
	})
}

// idScreenKey is a throwaway key for building a legal PureGo whose allowlist is
// never actually consulted.
func idScreenKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, pub := newSigner(t)
	return pub
}

// A binary that is READABLE but not EXECUTABLE must fail at construction.
//
// This is the case an open-based check accepts: a mode-0600 text file opens
// fine, so the verifier constructed successfully and the truth — EACCES or
// ENOEXEC — waited for somebody's first login. Executability is the question
// actually being asked.
func TestNewOpenSSH_BinaryMustBeExecutable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	signers := filepath.Join(dir, "signers")
	if err := os.WriteFile(signers, []byte("x ssh-ed25519 AAAA\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	notExec := filepath.Join(dir, "ssh-keygen")
	if err := os.WriteFile(notExec, []byte("#!/bin/sh\ntrue\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewOpenSSH(signers, Binary(notExec)); err == nil {
		t.Error("a readable-but-not-executable binary must not construct")
	} else if !errors.Is(err, ErrVerifierUnavailable) {
		t.Errorf("err = %v, want ErrVerifierUnavailable", err)
	}

	// The same file, now executable, constructs.
	requireDelegation(t)
	if err := os.Chmod(notExec, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewOpenSSH(signers, Binary(notExec)); err != nil {
		t.Errorf("an executable binary must construct: %v", err)
	}
}

// Only zero means "use the default". A negative duration is a mistake, and
// treating it as the default would hide it.
func TestNewOpenSSH_TimeoutValidation(t *testing.T) {
	t.Parallel()
	requireDelegation(t)
	dir := t.TempDir()
	signers := filepath.Join(dir, "signers")
	if err := os.WriteFile(signers, []byte("x ssh-ed25519 AAAA\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewOpenSSH(signers, Binary("/bin/true"), VerifyTimeout(-time.Second)); err == nil {
		t.Error("a negative VerifyTimeout must fail at construction")
	}
	v, err := NewOpenSSH(signers, Binary("/bin/true"), VerifyTimeout(0))
	if err != nil {
		t.Fatal(err)
	}
	if v.timeout != DefaultVerifyTimeout {
		t.Errorf("timeout = %v, want the documented default %v", v.timeout, DefaultVerifyTimeout)
	}
	v, err = NewOpenSSH(signers, Binary("/bin/true"), VerifyTimeout(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if v.timeout != 3*time.Second {
		t.Errorf("timeout = %v, want 3s", v.timeout)
	}
}

// Caller cancellation and our own timeout are different operator facts and must
// read differently, while both remaining ErrVerifierUnavailable.
func TestOpenSSH_CancellationIsDistinctFromTimeout(t *testing.T) {
	bin := sshKeygen(t)
	dir := t.TempDir()
	signers := filepath.Join(dir, "signers")
	if err := os.WriteFile(signers, []byte("x ssh-ed25519 AAAA\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := NewOpenSSH(signers, Binary(bin))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = v.VerifySignature(ctx, []byte("m"), []byte("sig"), testNS, "alice")
	if !errors.Is(err, ErrVerifierUnavailable) {
		t.Fatalf("err = %v, want ErrVerifierUnavailable", err)
	}
	if !strings.Contains(err.Error(), "cancelled by the caller") {
		t.Errorf("err = %q, want it to name caller cancellation rather than a timeout", err)
	}
}
