package sshkey

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/pem"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/yongjohnlee80/golib/auth"
)

const testNS = "webtui.golib.test"

// --- helpers ----------------------------------------------------------------

func newSigner(t *testing.T) (ssh.Signer, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s, s.PublicKey()
}

// signEnvelope builds an SSHSIG armored signature the way OpenSSH does. The
// interop test below proves this construction matches the real thing.
func signEnvelope(t *testing.T, s ssh.Signer, namespace, hashAlgo string, message []byte) []byte {
	t.Helper()
	var digest []byte
	switch hashAlgo {
	case "sha512":
		h := sha512.Sum512(message)
		digest = h[:]
	default:
		t.Fatalf("unsupported test hash %q", hashAlgo)
	}
	toSign := append([]byte(sigMagic), ssh.Marshal(signedData{
		Namespace: namespace, Reserved: "", HashAlgo: hashAlgo, Hash: digest,
	})...)
	sig, err := s.Sign(rand.Reader, toSign)
	if err != nil {
		t.Fatal(err)
	}
	body := append([]byte(sigMagic), ssh.Marshal(wire{
		Version:   sigVersion,
		PublicKey: s.PublicKey().Marshal(),
		Namespace: namespace,
		Reserved:  "",
		HashAlgo:  hashAlgo,
		Signature: ssh.Marshal(struct {
			Format string
			Blob   []byte
		}{sig.Format, sig.Blob}),
	})...)
	return pem.EncodeToMemory(&pem.Block{Type: sigPEMType, Bytes: body})
}

func harness(t *testing.T, pub ssh.PublicKey, subject string) (*Factor, *Challenger, *MemStore) {
	t.Helper()
	store := NewMemStore(0)
	allowed := []Allowed{{Key: pub, Subject: subject}}
	return New(allowed, store, Namespace(testNS)), NewChallenger(store, time.Minute), store
}

func present(chalID string, armor []byte, extra map[string]string) *auth.Request {
	creds := map[string]auth.Secret{
		"ssh-challenge": auth.NewSecret(chalID),
		"ssh-signature": auth.NewSecret(string(armor)),
	}
	for k, v := range extra {
		creds[k] = auth.NewSecret(v)
	}
	return &auth.Request{Credentials: creds}
}

// --- the test that matters most: real OpenSSH interop -----------------------

// TestInterop_RealSSHKeygenSignature verifies a signature produced by the
// actual `ssh-keygen -Y sign`. The SSHSIG envelope parser is hand-written
// (x/crypto ships no sshsig package), so a test against our own construction
// alone could be wrong in both directions at once. This one cannot.
func TestInterop_RealSSHKeygenSignature(t *testing.T) {
	bin, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not on PATH; skipping OpenSSH interop")
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if out, err := exec.Command(bin, "-t", "ed25519", "-N", "", "-C", "alice@host", "-f", keyPath).CombinedOutput(); err != nil {
		t.Fatalf("keygen: %v\n%s", err, out)
	}
	pubBytes, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := ParseAuthorizedKeys(pubBytes, SubjectFromComment)
	if err != nil {
		t.Fatalf("ParseAuthorizedKeys on a real ssh-keygen public key: %v", err)
	}
	if len(allowed) != 1 || allowed[0].Subject != "alice@host" {
		t.Fatalf("allowed = %+v", allowed)
	}

	store := NewMemStore(0)
	f := New(allowed, store, Namespace(testNS))
	ch, err := NewChallenger(store, time.Minute).Issue(Binding{})
	if err != nil {
		t.Fatal(err)
	}

	msgPath := filepath.Join(dir, "challenge.bin")
	if err := os.WriteFile(msgPath, ch.Message, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "-Y", "sign", "-f", keyPath, "-n", testNS, msgPath).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen -Y sign: %v\n%s", err, out)
	}
	armor, err := os.ReadFile(msgPath + ".sig")
	if err != nil {
		t.Fatal(err)
	}

	got, err := f.Verify(context.Background(), present(ch.ID, armor, nil))
	if err != nil {
		t.Fatalf("a genuine OpenSSH signature failed to verify: %v", err)
	}
	if got.Subject != "alice@host" {
		t.Errorf("Subject = %q, want alice@host", got.Subject)
	}
	if !got.ExpiresAt.IsZero() {
		t.Error("a signature is a point-in-time proof and must bound nothing")
	}
}

// A signature made for a DIFFERENT namespace must not verify here, proven with
// a real ssh-keygen signature — this is the replay the namespace exists to stop.
func TestInterop_WrongNamespaceRejected(t *testing.T) {
	bin, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not on PATH")
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if out, err := exec.Command(bin, "-t", "ed25519", "-N", "", "-C", "alice@host", "-f", keyPath).CombinedOutput(); err != nil {
		t.Fatalf("keygen: %v\n%s", err, out)
	}
	pubBytes, _ := os.ReadFile(keyPath + ".pub")
	allowed, err := ParseAuthorizedKeys(pubBytes, SubjectFromComment)
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemStore(0)
	f := New(allowed, store, Namespace(testNS))
	ch, err := NewChallenger(store, time.Minute).Issue(Binding{})
	if err != nil {
		t.Fatal(err)
	}
	msgPath := filepath.Join(dir, "challenge.bin")
	if err := os.WriteFile(msgPath, ch.Message, 0o600); err != nil {
		t.Fatal(err)
	}
	// Signed under someone else's namespace — e.g. a git signing flow.
	if out, err := exec.Command(bin, "-Y", "sign", "-f", keyPath, "-n", "git", msgPath).CombinedOutput(); err != nil {
		t.Fatalf("sign: %v\n%s", err, out)
	}
	armor, _ := os.ReadFile(msgPath + ".sig")
	if _, err := f.Verify(context.Background(), present(ch.ID, armor, nil)); !errors.Is(err, ErrNamespace) {
		t.Errorf("err = %v, want ErrNamespace", err)
	}
}

// --- authorized_keys parsing ------------------------------------------------

func TestParseAuthorizedKeys(t *testing.T) {
	t.Parallel()

	_, pub := newSigner(t)
	line := string(ssh.MarshalAuthorizedKey(pub))
	withComment := strings.TrimRight(line, "\n") + " alice@host\n"

	t.Run("comments and blank lines are fine", func(t *testing.T) {
		in := "# a comment\n\n" + withComment + "\n"
		got, err := ParseAuthorizedKeys([]byte(in), SubjectFromComment)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Subject != "alice@host" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("a malformed line is an ERROR, not a skip", func(t *testing.T) {
		in := withComment + "ssh-ed25519 not-valid-base64!!! bob@host\n"
		if _, err := ParseAuthorizedKeys([]byte(in), SubjectFromComment); err == nil {
			t.Error("a bad entry must fail loudly: silently skipping it denies someone access with no signal")
		}
	})

	t.Run("a key with no comment has no subject", func(t *testing.T) {
		if _, err := ParseAuthorizedKeys([]byte(line), SubjectFromComment); err == nil {
			t.Error("expected an error for a key with no comment")
		}
	})

	t.Run("a nil mapping is rejected", func(t *testing.T) {
		if _, err := ParseAuthorizedKeys([]byte(withComment), nil); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("empty input yields nothing", func(t *testing.T) {
		got, err := ParseAuthorizedKeys([]byte("\n# only comments\n"), SubjectFromComment)
		if err != nil || len(got) != 0 {
			t.Errorf("got %+v, %v", got, err)
		}
	})
}

// --- envelope tamper matrix -------------------------------------------------

func TestVerify_EnvelopeTampering(t *testing.T) {
	t.Parallel()

	signer, pub := newSigner(t)
	good := func(t *testing.T) (*Factor, string, []byte) {
		t.Helper()
		f, c, _ := harness(t, pub, "alice")
		ch, err := c.Issue(Binding{})
		if err != nil {
			t.Fatal(err)
		}
		return f, ch.ID, signEnvelope(t, signer, testNS, "sha512", ch.Message)
	}

	t.Run("valid", func(t *testing.T) {
		f, id, armor := good(t)
		if _, err := f.Verify(context.Background(), present(id, armor, nil)); err != nil {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("not PEM", func(t *testing.T) {
		f, id, _ := good(t)
		if _, err := f.Verify(context.Background(), present(id, []byte("not a signature"), nil)); !errors.Is(err, ErrMalformed) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("wrong PEM type", func(t *testing.T) {
		f, id, armor := good(t)
		bad := strings.ReplaceAll(string(armor), sigPEMType, "SSH PRIVATE KEY")
		if _, err := f.Verify(context.Background(), present(id, []byte(bad), nil)); !errors.Is(err, ErrMalformed) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("truncated body", func(t *testing.T) {
		f, id, armor := good(t)
		block, _ := pem.Decode(armor)
		trunc := pem.EncodeToMemory(&pem.Block{Type: sigPEMType, Bytes: block.Bytes[:len(block.Bytes)/2]})
		if _, err := f.Verify(context.Background(), present(id, trunc, nil)); !errors.Is(err, ErrMalformed) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("bad magic", func(t *testing.T) {
		f, id, armor := good(t)
		block, _ := pem.Decode(armor)
		b := append([]byte{}, block.Bytes...)
		copy(b, []byte("XXXXXX"))
		if _, err := f.Verify(context.Background(), present(id, pem.EncodeToMemory(&pem.Block{Type: sigPEMType, Bytes: b}), nil)); !errors.Is(err, ErrMalformed) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("wrong version", func(t *testing.T) {
		f, id, _ := good(t)
		ch, _ := NewChallenger(NewMemStore(0), time.Minute).Issue(Binding{})
		_ = ch
		body := append([]byte(sigMagic), ssh.Marshal(wire{
			Version: 99, PublicKey: pub.Marshal(), Namespace: testNS, HashAlgo: "sha512",
			Signature: ssh.Marshal(struct {
				Format string
				Blob   []byte
			}{"ssh-ed25519", []byte("x")}),
		})...)
		armor := pem.EncodeToMemory(&pem.Block{Type: sigPEMType, Bytes: body})
		if _, err := f.Verify(context.Background(), present(id, armor, nil)); !errors.Is(err, ErrMalformed) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("non-empty reserved field", func(t *testing.T) {
		f, id, _ := good(t)
		body := append([]byte(sigMagic), ssh.Marshal(wire{
			Version: sigVersion, PublicKey: pub.Marshal(), Namespace: testNS,
			Reserved: "surprise", HashAlgo: "sha512",
			Signature: ssh.Marshal(struct {
				Format string
				Blob   []byte
			}{"ssh-ed25519", []byte("x")}),
		})...)
		armor := pem.EncodeToMemory(&pem.Block{Type: sigPEMType, Bytes: body})
		if _, err := f.Verify(context.Background(), present(id, armor, nil)); !errors.Is(err, ErrMalformed) {
			t.Errorf("reserved must be refused, not ignored: err = %v", err)
		}
	})

	t.Run("unsupported hash algorithm", func(t *testing.T) {
		f, id, _ := good(t)
		body := append([]byte(sigMagic), ssh.Marshal(wire{
			Version: sigVersion, PublicKey: pub.Marshal(), Namespace: testNS, HashAlgo: "md5",
			Signature: ssh.Marshal(struct {
				Format string
				Blob   []byte
			}{"ssh-ed25519", []byte("x")}),
		})...)
		armor := pem.EncodeToMemory(&pem.Block{Type: sigPEMType, Bytes: body})
		if _, err := f.Verify(context.Background(), present(id, armor, nil)); !errors.Is(err, ErrHashAlgorithm) {
			t.Errorf("err = %v, want ErrHashAlgorithm", err)
		}
	})

	t.Run("signature over a different message", func(t *testing.T) {
		f, c, _ := harness(t, pub, "alice")
		ch, _ := c.Issue(Binding{})
		armor := signEnvelope(t, signer, testNS, "sha512", []byte("some other message"))
		if _, err := f.Verify(context.Background(), present(ch.ID, armor, nil)); !errors.Is(err, ErrBadSignature) {
			t.Errorf("err = %v, want ErrBadSignature", err)
		}
	})

	t.Run("key not in the allowed set", func(t *testing.T) {
		other, _ := newSigner(t)
		f, c, _ := harness(t, pub, "alice")
		ch, _ := c.Issue(Binding{})
		armor := signEnvelope(t, other, testNS, "sha512", ch.Message)
		if _, err := f.Verify(context.Background(), present(ch.ID, armor, nil)); !errors.Is(err, ErrUnknownKey) {
			t.Errorf("err = %v, want ErrUnknownKey", err)
		}
	})

	t.Run("empty allowed set denies", func(t *testing.T) {
		store := NewMemStore(0)
		f := New(nil, store, Namespace(testNS))
		ch, _ := NewChallenger(store, time.Minute).Issue(Binding{})
		armor := signEnvelope(t, signer, testNS, "sha512", ch.Message)
		if _, err := f.Verify(context.Background(), present(ch.ID, armor, nil)); !errors.Is(err, ErrNoAllowedKeys) {
			t.Errorf("err = %v, want ErrNoAllowedKeys", err)
		}
	})
}

// --- challenges -------------------------------------------------------------

func TestChallenge_SingleUseAndExpiry(t *testing.T) {
	t.Parallel()

	signer, pub := newSigner(t)

	t.Run("a nonce cannot be reused", func(t *testing.T) {
		f, c, _ := harness(t, pub, "alice")
		ch, _ := c.Issue(Binding{})
		armor := signEnvelope(t, signer, testNS, "sha512", ch.Message)
		if _, err := f.Verify(context.Background(), present(ch.ID, armor, nil)); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Verify(context.Background(), present(ch.ID, armor, nil)); !errors.Is(err, ErrChallengeUnknown) {
			t.Errorf("replay err = %v, want ErrChallengeUnknown", err)
		}
	})

	t.Run("a FAILED attempt still consumes the nonce", func(t *testing.T) {
		f, c, _ := harness(t, pub, "alice")
		ch, _ := c.Issue(Binding{})
		bad := signEnvelope(t, signer, testNS, "sha512", []byte("wrong"))
		if _, err := f.Verify(context.Background(), present(ch.ID, bad, nil)); !errors.Is(err, ErrBadSignature) {
			t.Fatalf("err = %v", err)
		}
		good := signEnvelope(t, signer, testNS, "sha512", ch.Message)
		if _, err := f.Verify(context.Background(), present(ch.ID, good, nil)); !errors.Is(err, ErrChallengeUnknown) {
			t.Error("a nonce must not survive a failed attempt — otherwise it can be brute-forced")
		}
	})

	t.Run("expired", func(t *testing.T) {
		now := time.Now()
		store := NewMemStore(0)
		f := New([]Allowed{{Key: pub, Subject: "alice"}}, store,
			Namespace(testNS), Clock(func() time.Time { return now.Add(time.Hour) }))
		ch, _ := NewChallenger(store, time.Minute, Clock(func() time.Time { return now })).Issue(Binding{})
		armor := signEnvelope(t, signer, testNS, "sha512", ch.Message)
		if _, err := f.Verify(context.Background(), present(ch.ID, armor, nil)); !errors.Is(err, ErrChallengeExpired) {
			t.Errorf("err = %v, want ErrChallengeExpired", err)
		}
	})

	t.Run("unknown id", func(t *testing.T) {
		f, _, _ := harness(t, pub, "alice")
		armor := signEnvelope(t, signer, testNS, "sha512", []byte("x"))
		if _, err := f.Verify(context.Background(), present("nope", armor, nil)); !errors.Is(err, ErrChallengeUnknown) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("no challenge presented", func(t *testing.T) {
		f, _, _ := harness(t, pub, "alice")
		r := &auth.Request{Credentials: map[string]auth.Secret{"ssh-signature": auth.NewSecret("x")}}
		if _, err := f.Verify(context.Background(), r); !errors.Is(err, ErrNoChallenge) {
			t.Errorf("err = %v", err)
		}
	})
}

// Exactly one of many simultaneous redeemers of one nonce may win.
func TestChallenge_ConsumeIsAtomic(t *testing.T) {
	t.Parallel()

	signer, pub := newSigner(t)
	for trial := 0; trial < 10; trial++ {
		f, c, store := harness(t, pub, "alice")
		ch, _ := c.Issue(Binding{})
		armor := signEnvelope(t, signer, testNS, "sha512", ch.Message)

		var wg sync.WaitGroup
		var mu sync.Mutex
		wins := 0
		start := make(chan struct{})
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, err := f.Verify(context.Background(), present(ch.ID, armor, nil)); err == nil {
					mu.Lock()
					wins++
					mu.Unlock()
				}
			}()
		}
		close(start)
		wg.Wait()
		if wins != 1 {
			t.Fatalf("trial %d: %d winners, want exactly 1", trial, wins)
		}
		if store.Len() != 0 {
			t.Fatalf("trial %d: nonce still outstanding", trial)
		}
	}
}

func TestChallenge_Bindings(t *testing.T) {
	t.Parallel()

	signer, pub := newSigner(t)

	t.Run("origin must match", func(t *testing.T) {
		f, c, _ := harness(t, pub, "alice")
		ch, _ := c.Issue(Binding{Origin: "https://good.example"})
		armor := signEnvelope(t, signer, testNS, "sha512", ch.Message)

		bad := present(ch.ID, armor, nil)
		bad.Metadata = map[string][]string{"Origin": {"https://evil.example"}}
		if _, err := f.Verify(context.Background(), bad); !errors.Is(err, ErrBinding) {
			t.Errorf("err = %v, want ErrBinding", err)
		}
	})

	t.Run("origin matching passes", func(t *testing.T) {
		f, c, _ := harness(t, pub, "alice")
		ch, _ := c.Issue(Binding{Origin: "https://good.example"})
		armor := signEnvelope(t, signer, testNS, "sha512", ch.Message)
		ok := present(ch.ID, armor, nil)
		ok.Metadata = map[string][]string{"Origin": {"https://good.example"}}
		if _, err := f.Verify(context.Background(), ok); err != nil {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("session must match", func(t *testing.T) {
		f, c, _ := harness(t, pub, "alice")
		ch, _ := c.Issue(Binding{Session: "sess-1"})
		armor := signEnvelope(t, signer, testNS, "sha512", ch.Message)
		if _, err := f.Verify(context.Background(), present(ch.ID, armor, map[string]string{"session": "sess-2"})); !errors.Is(err, ErrBinding) {
			t.Errorf("err = %v, want ErrBinding", err)
		}
		// A FRESH challenge is required for the positive case: the mismatched
		// attempt above consumed the first nonce, which is exactly the
		// single-use-even-on-failure behavior asserted elsewhere.
		ch2, _ := c.Issue(Binding{Session: "sess-1"})
		armor2 := signEnvelope(t, signer, testNS, "sha512", ch2.Message)
		if _, err := f.Verify(context.Background(), present(ch2.ID, armor2, map[string]string{"session": "sess-1"})); err != nil {
			t.Errorf("matching session: err = %v", err)
		}
	})

	// The bindings are inside the signed message, so swapping them invalidates
	// the signature rather than merely failing a comparison.
	t.Run("bindings are covered by the signature", func(t *testing.T) {
		store := NewMemStore(0)
		c := NewChallenger(store, time.Minute)
		bound, _ := c.Issue(Binding{Session: "sess-1", Origin: "https://good.example"})
		unbound, _ := c.Issue(Binding{})
		if string(bound.Message) == string(unbound.Message) {
			t.Fatal("bindings must change the message that gets signed")
		}
	})
}

func TestMemStore_Bounded(t *testing.T) {
	t.Parallel()

	store := NewMemStore(2)
	c := NewChallenger(store, time.Minute)
	if _, err := c.Issue(Binding{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Issue(Binding{}); err != nil {
		t.Fatal(err)
	}
	// An unauthenticated caller can ask for challenges, so the store must refuse
	// rather than grow.
	if _, err := c.Issue(Binding{}); !errors.Is(err, ErrFull) {
		t.Errorf("err = %v, want ErrFull", err)
	}
}

func TestNew_RequiresNamespace(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("New without a Namespace must panic: a signature from another purpose could be replayed")
		}
	}()
	_, pub := newSigner(t)
	_ = New([]Allowed{{Key: pub, Subject: "a"}}, NewMemStore(0))
}

func TestFactor_IsIdentityBearing(t *testing.T) {
	t.Parallel()

	_, pub := newSigner(t)
	f, _, _ := harness(t, pub, "alice")
	if f.Kind() != auth.FactorIdentity {
		t.Fatal("sshkey must be identity-bearing")
	}
	if _, err := auth.NewPolicy(auth.Leaf(f)); err != nil {
		t.Errorf("sshkey alone must form a valid policy: %v", err)
	}
}
