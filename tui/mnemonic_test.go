package tui

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/i18n"
)

func TestParseMnemonic(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text, label string
		hotkey      rune
		idx         int
	}{
		{"&File", "File", 'f', 0},
		{"E&xit", "Exit", 'x', 1},
		{"Save &As…", "Save As…", 'a', 5},
		{"Close(&q)", "Close(q)", 'q', 6},
		{"파일(&f)", "파일(f)", 'f', 3}, // a wide cluster is one cluster
		{"é&x", "éx", 'x', 1},     // so is a letter with its combining mark
		{"&&Co", "&Co", 0, -1},
		{"R&&&D", "R&D", 'd', 2},
		{"a&b&c", "abc", 'b', 1}, // only the first marker counts
		{"trailing&", "trailing&", 0, -1},
		{"plain", "plain", 0, -1},
		{"", "", 0, -1},
	} {
		label, hotkey, idx := ParseMnemonic(tc.text)
		if label != tc.label || hotkey != tc.hotkey || idx != tc.idx {
			t.Errorf("ParseMnemonic(%q) = %q, %q, %d; want %q, %q, %d",
				tc.text, label, hotkey, idx, tc.label, tc.hotkey, tc.idx)
		}
	}
}

func mustCatalog(t *testing.T, lang string, msgs ...string) *i18n.Catalog {
	t.Helper()
	var b strings.Builder
	b.WriteString(`<TS version="2.1" language="` + lang + `"><context><name>t</name>`)
	for i := 0; i+1 < len(msgs); i += 2 {
		b.WriteString(`<message id="` + msgs[i] + `"><translation>` +
			strings.ReplaceAll(msgs[i+1], "&", "&amp;") + `</translation></message>`)
	}
	b.WriteString(`</context></TS>`)
	c, err := i18n.ParseTS(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEnglishMnemonic_KeepsTheEnglishLetterWhateverACatalogMarks(t *testing.T) {
	t.Parallel()
	var s i18n.Set
	_ = s.Add(mustCatalog(t, "en", "file", "&File", "save", "&Save", "preview", "Preview"))
	for _, tc := range []struct{ name, id, text, want string }{
		{"the translator marked English's letter", "save", "&Salvar", "&Salvar"},
		{"another letter is marked", "file", "&Datei", "Datei(&f)"},
		{"nothing is marked", "file", "Datei", "Datei(&f)"},
		{"English has no key to keep", "preview", "&Vorschau", "Vorschau"},
		{"a literal ampersand survives", "save", "A&&B", "A&&B(&s)"},
		{"an id English lacks is as written", "app.only", "&Zzz", "&Zzz"},
	} {
		if got := englishMnemonic(tc.text, &s, tc.id); got != tc.want {
			t.Errorf("%s: englishMnemonic(%q) = %q, want %q", tc.name, tc.text, got, tc.want)
		}
	}
}
