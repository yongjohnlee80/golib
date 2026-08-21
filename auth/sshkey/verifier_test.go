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
)

// keypair is a real ssh-keygen-generated key on disk.
type keypair struct {
	priv    string
	pubLine string // the authorized_keys/allowed_signers key portion
	comment string
}

func sshKeygen(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not on PATH; skipping OpenSSH-dependent test")
	}
	return bin
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

	verifiers := map[string]Verifier{
		"OpenSSH": OpenSSH{AllowedSigners: signersPath, Binary: bin},
		"PureGo":  PureGo{Allowed: allowedSet},
	}

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

	store := NewMemStore(0)
	f := New(OpenSSH{AllowedSigners: signersPath, Binary: bin}, store, Namespace(testNS))
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
	err := OpenSSH{AllowedSigners: empty, Binary: bin}.
		VerifySignature(context.Background(), []byte("m"), sig, testNS, alice.comment)
	if !errors.Is(err, ErrBadSignature) {
		t.Errorf("err = %v, want ErrBadSignature (an empty allowlist denies)", err)
	}
}

// A misconfigured verifier must be distinguishable from a rejected credential:
// the operator needs to know it is their fault, and the caller must never see a
// missing binary as "bad password".
func TestOpenSSH_MisconfigurationIsNotRejection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	present := filepath.Join(dir, "signers")
	if err := os.WriteFile(present, []byte("x ssh-ed25519 AAAA\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := map[string]OpenSSH{
		"no allowed_signers path": {Binary: "/bin/true"},
		"allowed_signers missing": {AllowedSigners: filepath.Join(dir, "nope"), Binary: "/bin/true"},
		"binary missing":          {AllowedSigners: present, Binary: filepath.Join(dir, "no-such-ssh-keygen")},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			err := o.VerifySignature(context.Background(), []byte("m"), []byte("sig"), testNS, "alice")
			if !errors.Is(err, ErrVerifierUnavailable) {
				t.Errorf("err = %v, want ErrVerifierUnavailable", err)
			}
			if errors.Is(err, ErrBadSignature) {
				t.Error("a misconfiguration must never present as a failed signature")
			}
		})
	}
}

// A wedged ssh-keygen must not hold an attach open forever.
func TestOpenSSH_Timeout(t *testing.T) {
	t.Parallel()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no POSIX shell to build a stub with")
	}
	dir := t.TempDir()
	signers := filepath.Join(dir, "signers")
	if err := os.WriteFile(signers, []byte("x ssh-ed25519 AAAA\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A stub that ignores its arguments and hangs — real ssh-keygen would not,
	// but a wedged NFS mount or a stopped process makes it behave this way.
	stub := filepath.Join(dir, "hang")
	if err := os.WriteFile(stub, []byte("#!"+sh+"\nsleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = OpenSSH{AllowedSigners: signers, Binary: stub, Timeout: 100 * time.Millisecond}.
		VerifySignature(context.Background(), []byte("m"), []byte("sig"), testNS, "alice")
	if !errors.Is(err, ErrVerifierUnavailable) {
		t.Errorf("err = %v, want ErrVerifierUnavailable on timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %v — the timeout did not bound the subprocess", elapsed)
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
			// Both implementations must refuse before doing any work.
			if err := (PureGo{Allowed: []Allowed{{}}}).VerifySignature(context.Background(), nil, nil, testNS, id); !errors.Is(err, ErrIdentity) {
				t.Errorf("PureGo accepted identity %q: %v", id, err)
			}
			if err := (OpenSSH{AllowedSigners: "/etc/hostname"}).VerifySignature(context.Background(), nil, nil, testNS, id); !errors.Is(err, ErrIdentity) {
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
		_ = New(PureGo{}, nil, Namespace(testNS))
	})
}
