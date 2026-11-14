package totp

import (
	"bytes"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/errs"
)

// The RFC 6238 Appendix B seeds: the ASCII digits repeated to 20, 32 and 64
// bytes, one per algorithm.
var (
	seed1   = []byte("12345678901234567890")
	seed256 = []byte("12345678901234567890123456789012")
	seed512 = []byte("1234567890123456789012345678901234567890123456789012345678901234")
)

func mustNew(t *testing.T, opts ...Option) Params {
	t.Helper()
	p, err := New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCode_RFC6238Vectors(t *testing.T) {
	t.Parallel()

	vectors := []struct {
		unix                 int64
		sha1, sha256, sha512 string
	}{
		{59, "94287082", "46119246", "90693936"},
		{1111111109, "07081804", "68084774", "25091201"},
		{1111111111, "14050471", "67062674", "99943326"},
		{1234567890, "89005924", "91819424", "93441116"},
		{2000000000, "69279037", "90698825", "38618901"},
		{20000000000, "65353130", "77737706", "47863826"},
	}
	p1 := mustNew(t, Digits(8))
	p256 := mustNew(t, Digits(8), Hash(SHA256))
	p512 := mustNew(t, Digits(8), Hash(SHA512))
	for _, v := range vectors {
		at := time.Unix(v.unix, 0).UTC()
		if got := p1.Code(seed1, at); got != v.sha1 {
			t.Errorf("SHA1 at %d = %s, want %s", v.unix, got, v.sha1)
		}
		if got := p256.Code(seed256, at); got != v.sha256 {
			t.Errorf("SHA256 at %d = %s, want %s", v.unix, got, v.sha256)
		}
		if got := p512.Code(seed512, at); got != v.sha512 {
			t.Errorf("SHA512 at %d = %s, want %s", v.unix, got, v.sha512)
		}
	}
}

func TestHOTP_RFC4226Vectors(t *testing.T) {
	t.Parallel()

	want := []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"}
	for counter, w := range want {
		if got := (Params{}).hotp(seed1, uint64(counter)); got != w {
			t.Errorf("HOTP(%d) = %s, want %s", counter, got, w)
		}
	}
}

func TestCode_DefaultsAreSixDigitSHA1ThirtySeconds(t *testing.T) {
	t.Parallel()

	// The 8-digit SHA-1 vector at 59 s is 94287082; its last six digits are the
	// default code, since both reduce the same 31-bit number.
	at := time.Unix(59, 0)
	if got := Code(seed1, at); got != "287082" {
		t.Errorf("Code = %s, want 287082", got)
	}
	var zero Params
	if zero.Algorithm() != SHA1 || zero.Digits() != 6 || zero.Period() != 30*time.Second {
		t.Errorf("zero Params = %v/%d/%v", zero.Algorithm(), zero.Digits(), zero.Period())
	}
	p := mustNew(t)
	if p != (Params{alg: SHA1, digits: 6, period: 30 * time.Second}) {
		t.Errorf("New() = %+v", p)
	}
	if p.Code(seed1, at) != zero.Code(seed1, at) {
		t.Error("New() and the zero Params disagree")
	}
}

func TestNew_RefusesOutOfRange(t *testing.T) {
	t.Parallel()

	for name, opt := range map[string]Option{
		"zero algorithm":     Hash(0),
		"unknown algorithm":  Hash(SHA512 + 1),
		"five digits":        Digits(5),
		"nine digits":        Digits(9),
		"zero period":        Period(0),
		"negative period":    Period(-time.Second),
		"sub-second period":  Period(500 * time.Millisecond),
		"fractional seconds": Period(1500 * time.Millisecond),
	} {
		if _, err := New(opt); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("%s: err = %v, want ErrInvalidArgument", name, err)
		}
	}
	if _, err := New(nil, Digits(7), Period(time.Minute)); err != nil {
		t.Errorf("a nil option and valid ones: %v", err)
	}
}

func TestStep(t *testing.T) {
	t.Parallel()

	var p Params
	for _, c := range []struct {
		unix, step int64
	}{{0, 0}, {29, 0}, {30, 1}, {59, 1}, {60, 2}, {-1, -1}, {-30, -1}, {-31, -2}} {
		if got := p.Step(time.Unix(c.unix, 0)); got != c.step {
			t.Errorf("Step(%d) = %d, want %d", c.unix, got, c.step)
		}
	}
	if got := mustNew(t, Period(time.Minute)).Step(time.Unix(125, 0)); got != 2 {
		t.Errorf("60 s Step(125) = %d, want 2", got)
	}
}

func TestVerify_DriftWindow(t *testing.T) {
	t.Parallel()

	secret, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1_800_000_015, 0)
	now := Params{}.Step(at)
	for offset := int64(-3); offset <= 3; offset++ {
		code := Code(secret, at.Add(time.Duration(offset)*30*time.Second))
		step, ok := Verify(secret, code, at, -1)
		inWindow := offset >= -1 && offset <= 1
		if ok != inWindow {
			t.Errorf("offset %d: ok = %v, want %v", offset, ok, inWindow)
			continue
		}
		if ok && step != now+offset {
			t.Errorf("offset %d: step = %d, want %d", offset, step, now+offset)
		}
	}
}

func TestVerify_RefusesReplay(t *testing.T) {
	t.Parallel()

	secret, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1_800_000_015, 0)
	code := Code(secret, at)
	step, ok := Verify(secret, code, at, -1)
	if !ok {
		t.Fatal("a fresh code was refused")
	}
	if _, ok := Verify(secret, code, at, step); ok {
		t.Error("the same code was accepted twice")
	}
	if _, ok := Verify(secret, code, at.Add(20*time.Second), step); ok {
		t.Error("the same code was accepted again later in its window")
	}
	// The previous step's code is inside the drift window but older than the
	// one accepted, so it is refused too.
	prev := Code(secret, at.Add(-30*time.Second))
	if _, ok := Verify(secret, prev, at, step); ok {
		t.Error("an older step's code was accepted after a newer one")
	}
	// The next step's code is still newer, and is accepted once.
	next := Code(secret, at.Add(30*time.Second))
	nextStep, ok := Verify(secret, next, at, step)
	if !ok || nextStep != step+1 {
		t.Errorf("the next step's code: step %d ok %v, want %d true", nextStep, ok, step+1)
	}
	if _, ok := Verify(secret, next, at, nextStep); ok {
		t.Error("the next step's code was accepted twice")
	}
}

func TestVerify_RefusesMalformedCodes(t *testing.T) {
	t.Parallel()

	at := time.Unix(1_800_000_015, 0)
	code := Code(seed1, at)
	for _, bad := range []string{
		"",
		code[:5],
		code + "0",
		"0" + code,
		" " + code,
		code[:3] + " " + code[3:],
		code[:5] + "a",
		"-" + code[1:],
		"１２３４５６", // full-width digits
	} {
		if _, ok := Verify(seed1, bad, at, -1); ok {
			t.Errorf("Verify(%q) accepted a malformed code", bad)
		}
	}
	if _, ok := Verify(seed1, code, at, -1); !ok {
		t.Error("the well-formed code was refused")
	}
	// An 8-digit Params refuses the 6-digit form of its code.
	p8 := mustNew(t, Digits(8))
	if _, ok := p8.Verify(seed1, p8.Code(seed1, at)[2:], at, -1); ok {
		t.Error("an 8-digit Params accepted 6 digits")
	}
	if _, ok := p8.Verify(seed1, p8.Code(seed1, at), at, -1); !ok {
		t.Error("an 8-digit Params refused its own code")
	}
}

func TestVerify_WrongSecretAndEpoch(t *testing.T) {
	t.Parallel()

	at := time.Unix(1_800_000_015, 0)
	other := bytes.Repeat([]byte{7}, SecretLen)
	if _, ok := Verify(other, Code(seed1, at), at, -1); ok {
		t.Error("a code was accepted under another secret")
	}
	// At the epoch the window's earlier step is negative and skipped.
	epoch := time.Unix(0, 0)
	if step, ok := Verify(seed1, Code(seed1, epoch), epoch, -1); !ok || step != 0 {
		t.Errorf("at the epoch: step %d ok %v", step, ok)
	}
}

func TestEncodeDecode_RoundTrip(t *testing.T) {
	t.Parallel()

	for i := 0; i < 50; i++ {
		secret, err := NewSecret()
		if err != nil {
			t.Fatal(err)
		}
		if len(secret) != SecretLen {
			t.Fatalf("NewSecret length %d", len(secret))
		}
		enc := Encode(secret)
		if len(enc) != 32 || strings.ToUpper(enc) != enc || strings.Contains(enc, "=") {
			t.Fatalf("Encode = %q, want 32 upper-case characters and no padding", enc)
		}
		var grouped strings.Builder
		for j := 0; j < len(enc); j += 4 {
			if j > 0 {
				grouped.WriteByte(' ')
			}
			grouped.WriteString(strings.ToLower(enc[j : j+4]))
		}
		for _, form := range []string{enc, strings.ToLower(enc), grouped.String(), enc[:16] + "-" + enc[16:], " " + enc + "\n"} {
			got, err := Decode(form)
			if err != nil || !bytes.Equal(got, secret) {
				t.Fatalf("Decode(%q) = %x, %v; want %x", form, got, err, secret)
			}
		}
	}
	// The RFC 4648 vector, and a padded form.
	if got := Encode([]byte("foobar")); got != "MZXW6YTBOI" {
		t.Errorf("Encode(foobar) = %s", got)
	}
	if got, err := Decode("mzxw6ytboi======"); err != nil || string(got) != "foobar" {
		t.Errorf("Decode(padded) = %q, %v", got, err)
	}
}

func TestDecode_RefusesMalformed(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"", "   ", "----", "MZXW6YTBO1", "MZXW6YTBOI!", "A", "ABC"} {
		if _, err := Decode(bad); !errors.Is(err, ErrSecret) {
			t.Errorf("Decode(%q) err = %v, want ErrSecret", bad, err)
		}
	}
}

func TestURI_Escaping(t *testing.T) {
	t.Parallel()

	got := URI("Example Co", "alice@example.com", []byte("foobar"))
	want := "otpauth://totp/Example%20Co:alice%40example.com?secret=MZXW6YTBOI&issuer=Example%20Co&algorithm=SHA1&digits=6&period=30"
	if got != want {
		t.Errorf("URI =\n %s\nwant\n %s", got, want)
	}

	// A colon, slash, ampersand, plus, hash, question mark, percent and a
	// non-ASCII letter in either part are all escaped, so the label still has
	// exactly one separating colon and the query exactly the parameters it was
	// given.
	issuer, account := "A:B/C&D+E", "x?y#z%w é=1"
	raw := mustNew(t, Hash(SHA256), Digits(8), Period(time.Minute)).URI(issuer, account, []byte("foobar"))
	if strings.ContainsAny(strings.TrimPrefix(raw, "otpauth://"), " +#é") {
		t.Errorf("unescaped character in %s", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" {
		t.Errorf("scheme/host = %s/%s", u.Scheme, u.Host)
	}
	label := strings.TrimPrefix(u.EscapedPath(), "/")
	if strings.Count(label, ":") != 1 {
		t.Fatalf("label %q does not have exactly one separating colon", label)
	}
	parts := strings.SplitN(label, ":", 2)
	gotIssuer, _ := url.PathUnescape(parts[0])
	gotAccount, _ := url.PathUnescape(parts[1])
	if gotIssuer != issuer || gotAccount != account {
		t.Errorf("label decodes to %q:%q, want %q:%q", gotIssuer, gotAccount, issuer, account)
	}
	q := u.Query()
	if len(q) != 5 {
		t.Errorf("query has %d parameters: %v", len(q), q)
	}
	for k, v := range map[string]string{
		"secret": "MZXW6YTBOI", "issuer": issuer, "algorithm": "SHA256", "digits": "8", "period": "60",
	} {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
}

func TestURI_NoIssuer(t *testing.T) {
	t.Parallel()

	got := URI("", "alice", []byte("foobar"))
	want := "otpauth://totp/alice?secret=MZXW6YTBOI&algorithm=SHA1&digits=6&period=30"
	if got != want {
		t.Errorf("URI = %s, want %s", got, want)
	}
}

func TestAlgorithm_String(t *testing.T) {
	t.Parallel()

	for a, want := range map[Algorithm]string{SHA1: "SHA1", SHA256: "SHA256", SHA512: "SHA512", 0: "Algorithm(0)", 9: "Algorithm(9)"} {
		if got := a.String(); got != want {
			t.Errorf("%d.String() = %s, want %s", uint8(a), got, want)
		}
	}
}

func TestRecoveryCodes_ShapeAndUniqueness(t *testing.T) {
	t.Parallel()

	codes, err := RecoveryCodes(500)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 500 {
		t.Fatalf("got %d codes", len(codes))
	}
	seen := map[string]bool{}
	used := map[rune]bool{}
	for _, c := range codes {
		if len(c) != RecoveryCodeLen {
			t.Errorf("code %q is %d characters", c, len(c))
		}
		for _, r := range c {
			if !strings.ContainsRune(RecoveryAlphabet, r) {
				t.Errorf("code %q has %q, outside the alphabet", c, r)
			}
			used[r] = true
		}
		if seen[c] {
			t.Errorf("code %q repeated", c)
		}
		seen[c] = true
		if n, err := NormalizeRecoveryCode(c); err != nil || n != c {
			t.Errorf("NormalizeRecoveryCode(%q) = %q, %v", c, n, err)
		}
	}
	// 5,000 draws from 32 characters miss one with probability far below any
	// flake worth tolerating; a miss means the mapping lost part of the alphabet.
	if len(used) != 32 {
		t.Errorf("codes use %d of the 32 characters", len(used))
	}
}

func TestRecoveryAlphabet(t *testing.T) {
	t.Parallel()

	if len(RecoveryAlphabet) != 32 {
		t.Fatalf("alphabet has %d characters", len(RecoveryAlphabet))
	}
	seen := map[rune]bool{}
	for _, r := range RecoveryAlphabet {
		if seen[r] {
			t.Errorf("%q repeated", r)
		}
		seen[r] = true
	}
	for _, ambiguous := range "01IOl" {
		if seen[ambiguous] {
			t.Errorf("alphabet holds the ambiguous %q", ambiguous)
		}
	}
}

func TestRecoveryCodes_RefusesCount(t *testing.T) {
	t.Parallel()

	for _, n := range []int{0, -1} {
		if _, err := RecoveryCodes(n); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("RecoveryCodes(%d) err = %v", n, err)
		}
	}
	codes, err := RecoveryCodes(1)
	if err != nil || len(codes) != 1 {
		t.Errorf("RecoveryCodes(1) = %v, %v", codes, err)
	}
}

func TestNormalizeRecoveryCode(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"ABCDE-FGHJK":    "ABCDEFGHJK",
		"abcde fghjk":    "ABCDEFGHJK",
		" a-b-c-d-e f2 ": "ABCDEF2",
		"23456\t789ab\n": "23456789AB",
	} {
		got, err := NormalizeRecoveryCode(in)
		if len(want) != RecoveryCodeLen {
			if !errors.Is(err, ErrRecoveryCode) {
				t.Errorf("%q: err = %v, want ErrRecoveryCode for the wrong length", in, err)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("NormalizeRecoveryCode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ABCDEFGHJ", "ABCDEFGHJKL", "ABCDEFGHJ0", "ABCDEFGHJ1", "ABCDEFGHJI", "ABCDEFGHJO", "ABCDEFGHJ_", "ABCDEFGHJé"} {
		if _, err := NormalizeRecoveryCode(bad); !errors.Is(err, ErrRecoveryCode) {
			t.Errorf("NormalizeRecoveryCode(%q) err = %v, want ErrRecoveryCode", bad, err)
		}
	}
}
