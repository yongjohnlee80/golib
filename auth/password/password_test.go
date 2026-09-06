package password

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/auth"
)

// cheap is the cheapest LEGAL Argon2id configuration, so the suite runs in
// seconds rather than minutes. Production defaults are asserted separately.
func cheap() Argon2id {
	return Argon2id{Memory: minMemory, Time: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32}
}

func TestArgon2id_RoundTrip(t *testing.T) {
	t.Parallel()
	h := cheap()
	encoded, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(encoded, "correct horse battery staple"); err != nil {
		t.Fatalf("a freshly written credential must verify: %v", err)
	}
	if err := Verify(encoded, "correct horse battery stapl"); !errors.Is(err, ErrMismatch) {
		t.Errorf("err = %v, want ErrMismatch", err)
	}
	if err := Verify(encoded, ""); !errors.Is(err, ErrMismatch) {
		t.Errorf("empty password err = %v, want ErrMismatch", err)
	}
}

// The stored string must describe how to check itself, so a credential written
// under old parameters keeps verifying after policy changes.
func TestParametersTravelWithTheHash(t *testing.T) {
	t.Parallel()
	old := Argon2id{Memory: minMemory, Time: 2, Parallelism: 1, SaltLen: 16, KeyLen: 32}
	encoded, err := old.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("$argon2id$v=19$m=%d,t=2,p=1$", minMemory)
	if !strings.HasPrefix(encoded, want) {
		t.Fatalf("encoded = %q, want prefix %q", encoded, want)
	}
	// Current policy is different — verification must still follow the stored
	// parameters, not the configured ones.
	if err := Verify(encoded, "pw"); err != nil {
		t.Errorf("a credential under old parameters must still verify: %v", err)
	}
	if !Stale(encoded, cheap()) {
		t.Error("differing parameters must read as stale")
	}
	if Stale(encoded, old) {
		t.Error("identical parameters must not read as stale")
	}
}

// Salts must be per-credential: two hashes of the same password must differ.
func TestSaltIsPerCredential(t *testing.T) {
	t.Parallel()
	h := cheap()
	a, err := h.Hash("same")
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Hash("same")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two hashes of one password are identical — the salt is not random")
	}
	if err := Verify(a, "same"); err != nil {
		t.Error(err)
	}
	if err := Verify(b, "same"); err != nil {
		t.Error(err)
	}
}

// A stored credential is untrusted DATA. If an attacker can write to the store,
// an inflated cost parameter must fail the credential, not the process.
func TestHostileStoredParametersRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"absurd memory":      "$argon2id$v=19$m=4194304,t=3,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"absurd time":        "$argon2id$v=19$m=65536,t=999,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"absurd lanes":       "$argon2id$v=19$m=65536,t=3,p=250$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"memory below floor": "$argon2id$v=19$m=8,t=3,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"tiny key":           "$argon2id$v=19$m=65536,t=3,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAA",
		"short salt":         "$argon2id$v=19$m=65536,t=3,p=1$AAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"absurd iterations":  "$pbkdf2-sha256$i=999999999$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := Verify(encoded, "whatever")
			if !errors.Is(err, ErrParams) {
				t.Errorf("err = %v, want ErrParams", err)
			}
			if errors.Is(err, ErrMismatch) {
				t.Error("a poisoned credential must not read as a wrong password")
			}
		})
	}
}

// A malformed or unknown credential is an operator problem. Reporting it as
// ErrMismatch would make a storage bug look like a wave of wrong passwords.
func TestMalformedIsNotMismatch(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		encoded string
		want    error
	}{
		"empty":               {"", ErrEncoding},
		"no leading dollar":   {"argon2id$v=19$m=8192,t=1,p=1$AAAA$AAAA", ErrEncoding},
		"scheme only":         {"$argon2id", ErrEncoding},
		"unknown scheme":      {"$bcrypt$12$abcdef", ErrUnknownScheme},
		"too few segments":    {"$argon2id$v=19$m=8192,t=1,p=1$AAAAAAAAAAAAAAAAAAAAAA", ErrEncoding},
		"wrong argon version": {"$argon2id$v=16$m=8192,t=1,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ErrUnknownScheme},
		"bad base64 salt":     {"$argon2id$v=19$m=8192,t=1,p=1$!!!!$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ErrEncoding},
		"reordered params":    {"$argon2id$v=19$t=1,m=8192,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ErrEncoding},
		"missing key":         {"$argon2id$v=19$8192,1,1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ErrEncoding},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := Verify(c.encoded, "pw")
			if !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
			if errors.Is(err, ErrMismatch) {
				t.Error("a malformed credential must not read as a wrong password")
			}
		})
	}
}

// The candidate digest is derived at the STORED digest's length, so
// subtle.ConstantTimeCompare never sees a length mismatch — the early-return
// path that would defeat it. A truncated stored digest must therefore fail as
// out-of-range or corrupt, never as a partial match.
func TestFixedLengthBeforeCompare(t *testing.T) {
	t.Parallel()
	encoded, err := cheap().Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(encoded, "$")
	full := parts[len(parts)-1]

	// A digest truncated to a still-legal length must reach the COMPARISON and
	// fail there as a mismatch — not bail out as corrupt. Reaching ErrMismatch
	// is the observable proof that the candidate was derived at the stored
	// digest's length, since a length-mismatched compare would have returned
	// ErrEncoding instead.
	trunc := full[:32] // 32 raw-base64 chars -> 24 bytes, inside [16,64]
	parts[len(parts)-1] = trunc
	if err := Verify(strings.Join(parts, "$"), "pw"); !errors.Is(err, ErrMismatch) {
		t.Fatalf("err = %v, want ErrMismatch: the candidate is not being derived "+
			"at the stored digest's length", err)
	}

	// A digest truncated BELOW the floor is refused on parameters, before any
	// derivation happens.
	parts[len(parts)-1] = full[:8]
	if err := Verify(strings.Join(parts, "$"), "pw"); !errors.Is(err, ErrParams) {
		t.Errorf("err = %v, want ErrParams for an undersized digest", err)
	}
}

func TestPasswordLengthCap(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", MaxPasswordLen+1)
	if _, err := cheap().Hash(long); !errors.Is(err, ErrTooLong) {
		t.Errorf("Hash err = %v, want ErrTooLong", err)
	}
	encoded, err := cheap().Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(encoded, long); !errors.Is(err, ErrTooLong) {
		t.Errorf("Verify err = %v, want ErrTooLong", err)
	}
	// Exactly at the limit is fine.
	atLimit := strings.Repeat("a", MaxPasswordLen)
	e2, err := cheap().Hash(atLimit)
	if err != nil {
		t.Fatalf("a password at exactly the limit must be accepted: %v", err)
	}
	if err := Verify(e2, atLimit); err != nil {
		t.Error(err)
	}
}

// --- PBKDF2: only when explicitly selected ---------------------------------

func TestPBKDF2_OnlyWhenSelected(t *testing.T) {
	t.Parallel()
	p := PBKDF2{Iterations: minIterations, SaltLen: 16, KeyLen: 32}
	encoded, err := p.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, fmt.Sprintf("$pbkdf2-sha256$i=%d$", minIterations)) {
		t.Fatalf("encoded = %q", encoded)
	}
	// A PBKDF2 credential verifies regardless of current policy — the stored
	// string decides.
	if err := Verify(encoded, "pw"); err != nil {
		t.Errorf("a PBKDF2 credential must verify: %v", err)
	}
	if err := Verify(encoded, "nope"); !errors.Is(err, ErrMismatch) {
		t.Errorf("err = %v, want ErrMismatch", err)
	}
	// ...but it is stale under an Argon2id policy, so a successful login
	// migrates it.
	if !Stale(encoded, cheap()) {
		t.Error("a PBKDF2 credential must read as stale under an Argon2id policy")
	}
	if Stale(encoded, p) {
		t.Error("a PBKDF2 credential must not be stale under the same PBKDF2 policy")
	}
	if _, err := (PBKDF2{Iterations: 1000, SaltLen: 16, KeyLen: 32}).Hash("pw"); !errors.Is(err, ErrParams) {
		t.Errorf("an iteration count below the floor must be refused: %v", err)
	}
}

func TestDefaultProfiles(t *testing.T) {
	t.Parallel()
	// The shipped profiles must be legal and match their documented values;
	// a typo here weakens every deployment that took the default.
	d := Default()
	if d.Memory != 64*1024 || d.Time != 3 || d.Parallelism != 4 || d.KeyLen != 32 || d.SaltLen != 16 {
		t.Errorf("Default() = %+v, want RFC 9106's 64 MiB / t=3 / p=4", d)
	}
	i := Interactive()
	if i.Memory != 19*1024 || i.Time != 2 || i.Parallelism != 1 {
		t.Errorf("Interactive() = %+v, want OWASP's 19 MiB / t=2 / p=1", i)
	}
	if f := FIPS(); f.Iterations != 600_000 {
		t.Errorf("FIPS() = %+v, want 600000 iterations", f)
	}
	for name, h := range map[string]interface{ validate() error }{
		"Default": d, "Interactive": i, "FIPS": FIPS(),
	} {
		if err := h.validate(); err != nil {
			t.Errorf("%s is not a legal configuration: %v", name, err)
		}
	}
}

// --- the Factor -------------------------------------------------------------

func request(subject, pw string) *auth.Request {
	return &auth.Request{Credentials: map[string]auth.Secret{
		"subject":  auth.NewSecret(subject),
		"password": auth.NewSecret(pw),
	}}
}

func factor(t *testing.T, store Store, opts ...Option) *Factor {
	t.Helper()
	f, err := New(store, append([]Option{Hash(cheap())}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFactor_Verify(t *testing.T) {
	t.Parallel()
	store := NewMemStore()
	if err := store.Add("alice", "s3cret", cheap()); err != nil {
		t.Fatal(err)
	}
	f := factor(t, store)

	got, err := f.Verify(context.Background(), request("alice", "s3cret"))
	if err != nil {
		t.Fatalf("correct password rejected: %v", err)
	}
	if got.Subject != "alice" || got.Method != "password" {
		t.Errorf("contribution = %+v", got)
	}
	if !got.ExpiresAt.IsZero() {
		t.Error("a password proves a moment and must bound nothing")
	}

	for name, r := range map[string]*auth.Request{
		"wrong password":  request("alice", "S3cret"),
		"unknown subject": request("bob", "s3cret"),
	} {
		if _, err := f.Verify(context.Background(), r); !errors.Is(err, ErrMismatch) {
			t.Errorf("%s: err = %v, want ErrMismatch", name, err)
		}
	}

	for name, r := range map[string]*auth.Request{
		"nil request":  nil,
		"no subject":   {Credentials: map[string]auth.Secret{"password": auth.NewSecret("x")}},
		"no password":  {Credentials: map[string]auth.Secret{"subject": auth.NewSecret("alice")}},
		"empty secret": {Credentials: map[string]auth.Secret{"subject": auth.NewSecret("alice"), "password": {}}},
	} {
		if _, err := f.Verify(context.Background(), r); !errors.Is(err, ErrNoCredential) {
			t.Errorf("%s: err = %v, want ErrNoCredential", name, err)
		}
	}
}

// Upgrade-on-verify is the only way stored hashes ever get stronger, because a
// successful login is the only time the plaintext exists to rehash with.
func TestFactor_UpgradesOnSuccessfulVerify(t *testing.T) {
	t.Parallel()
	weak := Argon2id{Memory: minMemory, Time: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32}
	strong := Argon2id{Memory: minMemory, Time: 2, Parallelism: 1, SaltLen: 16, KeyLen: 32}

	store := NewMemStore()
	if err := store.Add("alice", "pw", weak); err != nil {
		t.Fatal(err)
	}
	before, _ := store.Lookup(context.Background(), "alice")
	f := factor(t, store, Hash(strong))

	if _, err := f.Verify(context.Background(), request("alice", "pw")); err != nil {
		t.Fatal(err)
	}
	after, _ := store.Lookup(context.Background(), "alice")
	if after == before {
		t.Fatal("a stale credential was not rewritten after a successful verify")
	}
	if !strings.HasPrefix(after, strong.scheme()+"$") {
		t.Errorf("rewritten credential = %q, want current parameters", after)
	}
	if err := Verify(after, "pw"); err != nil {
		t.Errorf("the rewritten credential must verify: %v", err)
	}
	if Stale(after, strong) {
		t.Error("the rewritten credential must not still read as stale")
	}

	// A FAILED login must never rewrite anything — it has no verified plaintext.
	snapshot, _ := store.Lookup(context.Background(), "alice")
	if _, err := f.Verify(context.Background(), request("alice", "wrong")); !errors.Is(err, ErrMismatch) {
		t.Fatal(err)
	}
	if now, _ := store.Lookup(context.Background(), "alice"); now != snapshot {
		t.Error("a failed attempt rewrote the stored credential")
	}
}

// A store that cannot be written to still authenticates. Turning a correct
// password into a rejection because a rehash failed would be a self-inflicted
// outage.
func TestFactor_UpgradeFailureDoesNotFailAuth(t *testing.T) {
	t.Parallel()
	weak := Argon2id{Memory: minMemory, Time: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32}
	strong := Argon2id{Memory: minMemory, Time: 2, Parallelism: 1, SaltLen: 16, KeyLen: 32}
	encoded, err := weak.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}

	var reported []error
	var mu sync.Mutex
	f := factor(t, brokenRehash{encoded: encoded}, Hash(strong),
		OnError(func(e error) { mu.Lock(); reported = append(reported, e); mu.Unlock() }))

	if _, err := f.Verify(context.Background(), request("alice", "pw")); err != nil {
		t.Fatalf("a failed rehash must not fail the authentication: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 1 || !errors.Is(reported[0], errRehash) {
		t.Errorf("reported = %v, want the rehash failure surfaced to the operator", reported)
	}
}

// A read-only store simply never gets upgraded; that must not be an error path.
func TestFactor_NonRehasherStoreIsSilent(t *testing.T) {
	t.Parallel()
	weak := Argon2id{Memory: minMemory, Time: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32}
	encoded, err := weak.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	var reported []error
	f := factor(t, readOnlyStore{encoded: encoded},
		Hash(Argon2id{Memory: minMemory, Time: 2, Parallelism: 1, SaltLen: 16, KeyLen: 32}),
		OnError(func(e error) { reported = append(reported, e) }))
	if _, err := f.Verify(context.Background(), request("alice", "pw")); err != nil {
		t.Fatal(err)
	}
	if len(reported) != 0 {
		t.Errorf("a store that cannot rehash is not an error: %v", reported)
	}
}

// A broken store and a corrupt credential are operator problems. The caller
// still gets one uniform rejection; the operator still gets told.
func TestFactor_OperatorProblemsAreReportedNotLeaked(t *testing.T) {
	t.Parallel()
	cases := map[string]Store{
		"store failure":      failingStore{},
		"corrupt credential": readOnlyStore{encoded: "$argon2id$garbage"},
	}
	for name, store := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var reported []error
			f := factor(t, store, OnError(func(e error) { reported = append(reported, e) }))
			_, err := f.Verify(context.Background(), request("alice", "pw"))
			if !errors.Is(err, ErrMismatch) {
				t.Errorf("err = %v, want the uniform ErrMismatch", err)
			}
			if len(reported) == 0 {
				t.Error("the operator was never told")
			}
		})
	}
}

// An unknown subject must cost what a known one costs. Without the dummy hash,
// "no such user" returns in microseconds while a real check takes tens of
// milliseconds, and that gap is a user-enumeration oracle readable remotely.
func TestFactor_UnknownSubjectCostsTheSame(t *testing.T) {
	store := NewMemStore()
	if err := store.Add("alice", "pw", cheap()); err != nil {
		t.Fatal(err)
	}
	f := factor(t, store)
	ctx := context.Background()

	measure := func(r *auth.Request) time.Duration {
		// Median of several runs: a mean is dragged around by one scheduling
		// hiccup, which is exactly what CI supplies.
		var samples []time.Duration
		for range 7 {
			start := time.Now()
			_, _ = f.Verify(ctx, r)
			samples = append(samples, time.Since(start))
		}
		for i := range samples {
			for j := i + 1; j < len(samples); j++ {
				if samples[j] < samples[i] {
					samples[i], samples[j] = samples[j], samples[i]
				}
			}
		}
		return samples[len(samples)/2]
	}

	known := measure(request("alice", "wrong"))
	unknown := measure(request("nobody", "wrong"))
	if known <= 0 || unknown <= 0 {
		t.Fatalf("unusable measurements: known=%v unknown=%v", known, unknown)
	}
	// Deliberately tolerant. The structural guarantee is that both paths run one
	// full hash at identical parameters; this only has to catch the failure mode
	// where one path does no hashing at all, which is an ORDERS-of-magnitude
	// difference, not a percentage.
	ratio := float64(known) / float64(unknown)
	if ratio > 4 || ratio < 0.25 {
		t.Errorf("known=%v unknown=%v (ratio %.2f): the two paths are distinguishable by timing",
			known, unknown, ratio)
	}
}

func TestNew_Validation(t *testing.T) {
	t.Parallel()
	if _, err := New(nil); err == nil {
		t.Error("New(nil) must fail: there is nothing to verify against")
	}
	if _, err := New(NewMemStore(), Hash(nil)); err == nil {
		t.Error("Hash(nil) must fail rather than silently taking the default")
	}
	f, err := New(NewMemStore(), Hash(cheap()))
	if err != nil {
		t.Fatal(err)
	}
	if f.dummy == "" {
		t.Fatal("no dummy credential was built — the unknown-subject path would be free")
	}
	if err := Verify(f.dummy, "anything"); !errors.Is(err, ErrMismatch) {
		t.Errorf("the dummy must be a real credential that simply never matches: %v", err)
	}
	if !strings.HasPrefix(f.dummy, cheap().scheme()+"$") {
		t.Error("the dummy must use the same parameters as a real credential, or the costs differ")
	}
}

func TestFactor_IsIdentityBearing(t *testing.T) {
	t.Parallel()
	f := factor(t, NewMemStore())
	if f.Kind() != auth.FactorIdentity {
		t.Fatal("password must be identity-bearing")
	}
	if _, err := auth.NewPolicy(auth.Leaf(f)); err != nil {
		t.Errorf("password alone must form a valid policy: %v", err)
	}
}

func TestMemStore_RehashDoesNotCreate(t *testing.T) {
	t.Parallel()
	m := NewMemStore()
	if err := m.Rehash(context.Background(), "ghost", "$argon2id$x"); !errors.Is(err, ErrNoCredential) {
		t.Errorf("err = %v: the upgrade path must not be able to create a credential", err)
	}
	if _, err := m.Lookup(context.Background(), "ghost"); !errors.Is(err, ErrNoCredential) {
		t.Error("a credential appeared through the upgrade path")
	}
}

// --- fakes ------------------------------------------------------------------

var errRehash = errors.New("rehash refused")

type brokenRehash struct{ encoded string }

func (b brokenRehash) Lookup(context.Context, string) (string, error) { return b.encoded, nil }
func (b brokenRehash) Rehash(context.Context, string, string) error   { return errRehash }

type readOnlyStore struct{ encoded string }

func (r readOnlyStore) Lookup(context.Context, string) (string, error) { return r.encoded, nil }

type failingStore struct{}

func (failingStore) Lookup(context.Context, string) (string, error) {
	return "", errors.New("database is on fire")
}

// sameDigest is the last line before the comparison, and the length check has to
// come first: subtle.ConstantTimeCompare RETURNS EARLY on a length mismatch, so
// handing it variable-length material reintroduces the very leak it exists to
// prevent.
func TestSameDigest(t *testing.T) {
	t.Parallel()
	a := []byte("0123456789abcdef")
	if err := sameDigest(a, []byte("0123456789abcdef")); err != nil {
		t.Errorf("equal digests: %v", err)
	}
	if err := sameDigest(a, []byte("0123456789abcdeF")); !errors.Is(err, ErrMismatch) {
		t.Errorf("differing digests: err = %v, want ErrMismatch", err)
	}
	for name, got := range map[string][]byte{
		"shorter": []byte("0123456789abcde"),
		"longer":  []byte("0123456789abcdefg"),
		"empty":   {},
		"nil":     nil,
	} {
		// A length mismatch means a corrupt stored credential, because every
		// caller derives its candidate at the stored length. It must NOT report
		// as a wrong password.
		if err := sameDigest(a, got); !errors.Is(err, ErrEncoding) {
			t.Errorf("%s: err = %v, want ErrEncoding", name, err)
		}
	}
}
