package paths

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What Encode writes, one case per rule. The result is the same on every OS.
func TestEncodeWritesOneFormForEveryOS(t *testing.T) {
	for _, c := range []struct{ name, want string }{
		{"plain-name_1.md", "plain-name_1.md"},
		{"", "%_"},
		{".", "%2E"},
		{"..", ".%2E"},
		{"a.", "a%2E"},
		{".hidden", ".hidden"},
		{"autodoc:c619", "autodoc%3Ac619"},
		{"A", "%41"},
		{"a b", "a%20b"},
		{"100%", "100%25"},
		{`a/b\c`, "a%2Fb%5Cc"},
		{"con", "%63on"},
		{"nul.txt", "%6Eul.txt"},
		{"com1", "%63om1"},
		{"lpt9.tar.gz", "%6Cpt9.tar.gz"},
		{"console", "console"},
		{"com", "com"},
		{"한", "%ED%95%9C"},
		{"\x00\xff", "%00%FF"},
	} {
		if got := Encode(c.name); got != c.want {
			t.Errorf("Encode(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// The limit is on the encoded length: a name short enough to read but long once escaped is hashed
// too. Two long names sharing a prefix stay apart, no escape is cut in half, and Decode says the
// name can't be had back.
func TestALongNameIsHashedOnItsEncodedLength(t *testing.T) {
	fits := strings.Repeat("a", MaxEncoded)
	if got := Encode(fits); got != fits {
		t.Fatalf("a name of exactly the limit was changed: %d bytes", len(got))
	}
	escaped := strings.Repeat(":", MaxEncoded/3+1) // short as a name, past the limit escaped
	cut := MaxEncoded - len(hashMark) - hashLen    // the two names below put an escape across it
	for _, name := range []string{fits + "b", escaped, strings.Repeat("a", cut-1) + strings.Repeat(":", 20), strings.Repeat("a", cut-2) + strings.Repeat(":", 20)} {
		e := Encode(name)
		if len(e) > MaxEncoded || !strings.Contains(e, hashMark) {
			t.Errorf("Encode of a %d-byte name = %d bytes %q", len(name), len(e), e)
		}
		if _, err := Decode(e); !errors.Is(err, ErrNotReversible) {
			t.Errorf("Decode(%q) = %v, want ErrNotReversible", e, err)
		}
	}
	if Encode(fits+"b") == Encode(fits+"c") {
		t.Error("two long names sharing a prefix encode the same")
	}
}

// Only what Encode writes decodes, so two elements never name one identity.
func TestDecodeRefusesWhatEncodeWouldNotWrite(t *testing.T) {
	for _, e := range []string{
		"%3a", "%", "%4", "%zz", "%41%", // lowercase hex, a bare or cut escape
		"%61",             // an escape that wasn't needed
		"A", "a b", "a:b", // a byte that must be escaped
		"a.", ".", "..", // a last dot left bare
		"con", "NUL.txt", // a reserved stem left bare
		"a%~0123", // a hash mark on nothing of the right shape
		strings.Repeat("a", MaxEncoded+1),
		"", // the empty element is not the empty name
	} {
		if got, err := Decode(e); err == nil {
			t.Errorf("Decode(%q) = %q, want it refused", e, got)
		}
	}
}

func FuzzEncodeThenDecode(f *testing.F) {
	for _, s := range []string{"", ".", "..", "con", "A", "a:b", "한", "\x00", strings.Repeat(":", 200)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		e := Encode(name)
		if e == "" || e == "." || e == ".." || len(e) > MaxEncoded {
			t.Fatalf("Encode(%q) = %q", name, e)
		}
		got, err := Decode(e)
		if errors.Is(err, ErrNotReversible) {
			return
		}
		if err != nil || got != name {
			t.Fatalf("Decode(Encode(%q)) = %q, %v", name, got, err)
		}
	})
}

func FuzzDecodeOnlyCanonical(f *testing.F) {
	for _, s := range []string{"%_", "%3A", "%3a", "a.%2E", "%63on", "con", "%", "a%~"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, element string) {
		name, err := Decode(element)
		if err != nil {
			return
		}
		if again := Encode(name); again != element {
			t.Fatalf("Decode(%q) accepted it, but Encode(%q) = %q", element, name, again)
		}
	})
}

// The OS itself judges the encoded names: each hostile name becomes a file of its own, read back
// under its element. On Windows this is what proves the reserved names, the trailing dots and the
// names differing only by case are legal and stay apart.
func TestEveryHostileNameIsAFileOfItsOwn(t *testing.T) {
	dir := t.TempDir()
	names := []string{"con", "NUL", "aux.txt", "com1", "lpt9", "a:b", "a.", "a ", "A", "a", `x\y`, "x/y", "..", ".", "", "<>|?*\"", "한글", strings.Repeat("z", 300)}
	seen := map[string]string{}
	for _, name := range names {
		e := Encode(name)
		if other, dup := seen[strings.ToLower(e)]; dup {
			t.Errorf("%q and %q encode to one element: %q", name, other, e)
		}
		seen[strings.ToLower(e)] = name
		if err := os.WriteFile(filepath.Join(dir, e), []byte(name), 0o600); err != nil {
			t.Errorf("the element for %q, %q: %v", name, e, err)
		}
	}
	for _, name := range names {
		got, err := os.ReadFile(filepath.Join(dir, Encode(name)))
		if err != nil || string(got) != name {
			t.Errorf("reading back %q: %q, %v", name, got, err)
		}
	}
	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) != len(names) {
		t.Errorf("%d files for %d names (%v)", len(ents), len(names), err)
	}
}
