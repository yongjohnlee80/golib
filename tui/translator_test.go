package tui

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui/i18n"
)

// fakeCatalog is a Translator over a map of language → id → text, recording what it is asked.
type fakeCatalog struct {
	text  map[string]map[string]string
	asked []string
}

func (f *fakeCatalog) Lookup(language, id string) (string, bool) {
	f.asked = append(f.asked, language+"/"+id)
	t, ok := f.text[language][id]
	return t, ok
}

// Any Translator serves the App: a Message is looked up in the App's language, the hotkey kept on
// English's letter (asked in i18n.English), and the id itself shown where no catalog has it.
// SetTranslator replaces the catalogs, as SetTranslations does an i18n.Set.
func TestAnyTranslatorServesTheApp(t *testing.T) {
	ko := &fakeCatalog{text: map[string]map[string]string{
		"ko_KR":      {"menu.file": "파일"},
		i18n.English: {"menu.file": "&File"},
	}}
	p := &probe{name: "p"}
	h := startApp(t, p, 10, 3, WithTranslator(ko), WithLanguage("ko_KR"))
	var file, missing string
	h.onLoop(func() { file, missing = h.app.Translate(Msg("menu.file")), h.app.Translate(Msg("menu.nope")) })
	if file != "파일(&f)" {
		t.Errorf("Translate(menu.file) = %q, want the Korean label on English's hotkey, %q", file, "파일(&f)")
	}
	if missing != "menu.nope" {
		t.Errorf("Translate(menu.nope) = %q, want the id", missing)
	}
	h.onLoop(func() {
		if len(ko.asked) < 2 || ko.asked[0] != "ko_KR/menu.file" || ko.asked[1] != i18n.English+"/menu.file" {
			t.Errorf("the catalog was asked %q; want the App's language, then English for the hotkey", ko.asked)
		}
	})

	es := &fakeCatalog{text: map[string]map[string]string{"ko_KR": {"menu.file": "다른"}}}
	h.app.SetTranslator(es)
	h.sync()
	h.onLoop(func() { file = h.app.Translate(Msg("menu.file")) })
	if file != "다른" {
		t.Errorf("after SetTranslator: Translate = %q, want the new catalogs' %q", file, "다른")
	}
}

// Nil catalogs are refused where they are given, with errs.Fatal naming the call: a nil
// Translator, and a typed nil (a nil *i18n.Set inside the interface), which a plain == nil would
// let through to fail at the first lookup.
func TestNilCatalogsAreRefusedWhereGiven(t *testing.T) {
	app := NewApp(&probe{name: "p"}, WithBackend(NewTestBackend(4, 2)))
	for name, give := range map[string]func(){
		"WithTranslator(nil)":            func() { WithTranslator(nil) },
		"WithTranslator(nil *i18n.Set)":  func() { WithTranslator((*i18n.Set)(nil)) },
		"SetTranslator(nil *i18n.Set)":   func() { app.SetTranslator((*i18n.Set)(nil)) },
		"ApplyTranslator(nil *i18n.Set)": func() { app.ApplyTranslator((*i18n.Set)(nil)) },
		"WithTranslations(nil)":          func() { WithTranslations(nil) },
	} {
		func() {
			defer func() {
				r := recover()
				err, _ := r.(error)
				var fatal errs.Fatal
				if !errors.As(err, &fatal) {
					t.Errorf("%s: recovered %v, want an errs.Fatal", name, r)
				}
			}()
			give()
		}()
	}
}
