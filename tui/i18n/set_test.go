package i18n

import (
	"encoding/xml"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/errs"
)

func catalog(t *testing.T, lang string, msgs map[string]string) *Catalog {
	t.Helper()
	var b strings.Builder
	for id, text := range msgs {
		b.WriteString(`<message id="` + id + `"><translation>`)
		_ = xml.EscapeText(&b, []byte(text))
		b.WriteString(`</translation></message>`)
	}
	c, err := ParseTS(strings.NewReader(ts(lang, b.String())))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSet_LookupFallsBackThroughTheBaseLanguageToEnglish(t *testing.T) {
	t.Parallel()
	var s Set
	for _, c := range []*Catalog{
		catalog(t, "en", map[string]string{"a": "A", "b": "B", "c": "C"}),
		catalog(t, "es", map[string]string{"a": "A-es", "b": "B-es"}),
		catalog(t, "es_MX", map[string]string{"a": "A-mx"}),
	} {
		if err := s.Add(c); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ lang, id, want string }{
		{"es_MX", "a", "A-mx"}, // the language itself
		{"es-MX", "a", "A-mx"}, // either separator
		{"es_MX", "b", "B-es"}, // its base
		{"es_MX", "c", "C"},    // English
		{"es", "c", "C"},
		{"en", "a", "A"},
		{"fr", "a", "A"}, // a language with no catalog reads English
	} {
		got, ok := s.Lookup(tc.lang, tc.id)
		if !ok || got != tc.want {
			t.Errorf("Lookup(%q, %q) = %q, %v; want %q", tc.lang, tc.id, got, ok, tc.want)
		}
	}
	if got, ok := s.Lookup("es_MX", "nowhere"); ok {
		t.Errorf("Lookup of an id no catalog has = %q, true; want it reported missing", got)
	}
}

func TestSet_ALaterCatalogWinsWithinALanguage(t *testing.T) {
	t.Parallel()
	var s Set
	_ = s.Add(catalog(t, "ko_KR", map[string]string{"a": "toolkit", "b": "toolkit"}))
	_ = s.Add(catalog(t, "ko_KR", map[string]string{"a": "app"}))
	if got, _ := s.Lookup("ko_KR", "a"); got != "app" {
		t.Errorf("a = %q, want the later catalog's", got)
	}
	if got, _ := s.Lookup("ko_KR", "b"); got != "toolkit" {
		t.Errorf("b = %q, want the earlier catalog's, which the later one does not hold", got)
	}
}

func TestSet_AddRefusesACatalogWithNoLanguage(t *testing.T) {
	t.Parallel()
	var s Set
	for _, c := range []*Catalog{nil, catalog(t, "", nil)} {
		if err := s.Add(c); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("Add(%v) = %v, want %v", c, err, errs.ErrInvalidArgument)
		}
	}
}

func TestSet_LoadDirNamesTheLanguageByFile(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"i18n/editor_ko_KR.xml": {Data: []byte(ts("ko_KR", `<message id="a"><translation>가</translation></message>`))},
		"i18n/editor_es.xml":    {Data: []byte(ts("", `<message id="a"><translation>a-es</translation></message>`))},
		"i18n/other_fr.xml":     {Data: []byte(ts("fr", `<message id="a"><translation>a-fr</translation></message>`))},
		"i18n/editor_notes.txt": {Data: []byte("not a catalog")},
	}
	var s Set
	if err := s.LoadDir(fsys, "i18n", "editor"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Lookup("ko_KR", "a"); got != "가" {
		t.Errorf("ko_KR a = %q, want 가", got)
	}
	if got, _ := s.Lookup("es", "a"); got != "a-es" {
		t.Errorf("es a = %q, want a-es: a catalog naming no language takes its file name's", got)
	}
	if got, ok := s.Lookup("fr", "a"); ok {
		t.Errorf("fr a = %q: another prefix's file must not load", got)
	}
}

func TestSet_LoadDirRefusesAMisnamedCatalogAndAddsNothing(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"i18n/editor_en.xml": {Data: []byte(ts("en", `<message id="a"><translation>A</translation></message>`))},
		"i18n/editor_ko.xml": {Data: []byte(ts("ja_JP", `<message id="a"><translation>あ</translation></message>`))},
	}
	var s Set
	err := s.LoadDir(fsys, "i18n", "editor")
	if !errors.Is(err, errs.ErrInvalidArgument) || !strings.Contains(err.Error(), "editor_ko.xml") {
		t.Fatalf("err = %v, want an invalid argument naming editor_ko.xml", err)
	}
	if got, ok := s.Lookup("en", "a"); ok {
		t.Errorf("en a = %q: a refused directory must add none of its catalogs", got)
	}
}

func TestSet_LoadDirNamesTheFileOfAMalformedCatalog(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"i18n/editor_en.xml": {Data: []byte(`<TS language="en"><message id="a">`)}}
	var s Set
	err := s.LoadDir(fsys, "i18n", "editor")
	if !errors.Is(err, errs.ErrInvalidArgument) || !strings.Contains(err.Error(), "i18n/editor_en.xml") {
		t.Fatalf("err = %v, want an invalid argument naming i18n/editor_en.xml", err)
	}
}
