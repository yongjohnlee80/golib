package query

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
)

func TestTerms(t *testing.T) {
	for _, c := range []struct {
		q     string
		terms []Term
		words []string
	}{
		{"storage", []Term{{"storage", false}}, []string{"storage"}},
		{"stor*", []Term{{"stor", true}}, []string{"stor"}},
		// only the last word keeps a prefix; any other loses its '*'
		{"stor* sqlite*", []Term{{"stor", false}, {"sqlite", true}}, []string{"stor", "sqlite"}},
		// dialect syntax is words, and punctuation alone is dropped
		{`storage OR "x" NEAR(y) -z ^w col:val !!! ,`, []Term{{"storage", false}, {"OR", false}, {`"x"`, false}, {"NEAR(y)", false}, {"-z", false}, {"^w", false}, {"col:val", false}}, []string{"storage", "or", `"x"`, "near(y)", "-z", "^w", "col:val"}},
		{"#Design", []Term{{"#Design", false}}, []string{"design"}},
		{"東京 café", []Term{{"東京", false}, {"café", false}}, []string{"東京", "café"}},
		{"   ", nil, nil},
		{"*", nil, nil},
	} {
		terms, words := Terms(c.q)
		if !reflect.DeepEqual(terms, c.terms) || !reflect.DeepEqual(words, c.words) {
			t.Errorf("Terms(%q) = %+v, %q; want %+v, %q", c.q, terms, words, c.terms, c.words)
		}
	}
}

func TestFTS5(t *testing.T) {
	terms, _ := Terms(`storage say"hi" stor*`)
	if got, want := FTS5(terms), `"storage" "say""hi""" "stor"*`; got != want {
		t.Errorf("FTS5 = %s, want %s", got, want)
	}
	if got := FTS5(nil); got != "" {
		t.Errorf("FTS5(nil) = %q", got)
	}
}

// fields declares type (any text) and count (an integer).
type fields map[string]bool

func (f fields) Declared(name string) bool { return f[name] }

func (f fields) FacetValue(name, text string) (string, error) {
	if name == "count" {
		n, err := strconv.Atoi(text)
		if err != nil {
			return "", fmt.Errorf("%q is not an integer", text)
		}
		return strconv.Itoa(n), nil
	}
	return text, nil
}

// nilSafe is a pointer whose nil value declares nothing, as a consumer's schema may be.
type nilSafe struct{ f fields }

func (n *nilSafe) Declared(name string) bool { return n != nil && n.f.Declared(name) }
func (n *nilSafe) FacetValue(name, text string) (string, error) {
	if n == nil {
		return "", errors.New("no schema")
	}
	return n.f.FacetValue(name, text)
}

func TestFacets(t *testing.T) {
	f := fields{"type": true, "count": true}
	rest, facets, err := Facets("type:adr storage type:adr re:x http://h count:007", map[string][]string{"type": {"note"}}, f)
	if err != nil {
		t.Fatal(err)
	}
	if rest != "storage re:x http://h" {
		t.Errorf("rest = %q", rest)
	}
	if want := map[string][]string{"type": {"note", "adr"}, "count": {"7"}}; !reflect.DeepEqual(facets, want) {
		t.Errorf("facets = %v, want %v", facets, want)
	}

	// the same word once the field is no longer declared is searched as text
	rest, facets, err = Facets("type:adr storage", nil, fields{"count": true})
	if err != nil || rest != "type:adr storage" || facets != nil {
		t.Errorf("an undeclared field: %q %v %v", rest, facets, err)
	}
	if _, _, err := Facets("storage", map[string][]string{"nope": {"x"}}, f); !errors.Is(err, ErrUnknownFacet) {
		t.Errorf("a given undeclared field: %v", err)
	}
	if _, _, err := Facets("count:many storage", nil, f); !errors.Is(err, ErrFacetValue) {
		t.Errorf("a bad value: %v", err)
	}
	if _, _, err := Facets("storage", map[string][]string{"type": {}}, f); !errors.Is(err, ErrFacetValue) {
		t.Errorf("a given field without values: %v", err)
	}

	// no fields: a nil interface and a typed nil both declare nothing
	var typedNil *nilSafe
	for name, none := range map[string]Fields{"nil interface": nil, "typed nil": typedNil} {
		rest, facets, err := Facets("type:adr storage", nil, none)
		if err != nil || rest != "type:adr storage" || facets != nil {
			t.Errorf("%s: %q %v %v", name, rest, facets, err)
		}
		if _, _, err := Facets("storage", map[string][]string{"type": {"adr"}}, none); !errors.Is(err, ErrUnknownFacet) {
			t.Errorf("%s: a given field: %v", name, err)
		}
	}
}
