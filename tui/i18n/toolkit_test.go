package i18n

import (
	"sort"
	"testing"
	"unicode"
)

// The literals the toolkit ids replace. English must reproduce them exactly: applications
// and their tests read these labels on screen today, and a program that installs no catalog
// of its own must look exactly as it did.
var toolkitEnglish = map[string]string{
	"tui.button.ok":              "&OK",
	"tui.button.save":            "&Save",
	"tui.button.yes":             "&Yes",
	"tui.button.no":              "&No",
	"tui.button.cancel":          "&Cancel",
	"tui.button.close":           "Close(&q)",
	"tui.filedialog.open":        "&Open",
	"tui.filedialog.save":        "&Save",
	"tui.filedialog.select":      "&Select",
	"tui.filedialog.close":       "Close (&q)",
	"tui.files.preview":          "Preview",
	"tui.files.fileName":         "File name",
	"tui.files.folder":           "Folder",
	"tui.files.folderNotAFolder": "Folder — not a folder",
	"tui.files.empty":            "(empty)",
	"tui.files.isFolder":         "(folder)",
	"tui.files.cannotRead":       "(cannot read: %1)",
	"tui.files.binary":           "(binary file — no preview)",
	"tui.files.emptyFile":        "(empty file)",
	"tui.files.titleCannotRead":  "%1 — cannot read",
	"tui.files.hint.list":        "↑↓:move  Enter:open folder",
	"tui.files.hint.preview":     "j/k:scroll  gg/G:top/end",
	"tui.files.hint.default":     "Enter:press  Tab:next  Esc:cancel",
	"tui.files.hint.openList":    "↑↓:move  Enter:open folder, or open file  Tab:next  Esc:cancel",
	"tui.files.hint.previewPart": "j/k:scroll  gg/G:top/end  Tab:next  Esc:cancel",
	"tui.files.hint.saveName":    "Enter:save  Tab:next  Esc:cancel",
	"tui.files.hint.saveList":    "↑↓:move  Enter:open folder, or save over file  Tab:next  Esc:cancel",
	"tui.files.hint.folderPath":  "Enter:go to folder  Tab:next  Esc:cancel",
	"tui.files.hint.listPart":    "↑↓:move  Enter:open folder  Tab:next  Esc:cancel",
	"tui.select.error":           "error: %1",
	"tui.tooSmall.title":         "Screen too small",
	"tui.tooSmall.size":          "%1 × %2 — needs %3 × %4",
	"tui.tooSmall.enlarge":       "Enlarge the window to continue",
	"tui.tooSmall.quit":          "Quit",
	"tui.tooSmall.quitKeys":      "or press q / Ctrl+C",
	"tui.editor.menu.undo":       "&Undo",
	"tui.editor.menu.redo":       "&Redo",
	"tui.editor.menu.copy":       "&Copy",
	"tui.editor.menu.cut":        "Cu&t",
	"tui.editor.menu.paste":      "&Paste",
}

var shippedLanguages = []string{"en", "ko_KR", "ja_JP", "zh_CN", "pt_BR", "es"}

func TestToolkit_EnglishIsTheLiteralsItReplaces(t *testing.T) {
	t.Parallel()
	s := Toolkit()
	for id, want := range toolkitEnglish {
		if got, ok := s.Lookup(English, id); !ok || got != want {
			t.Errorf("%s = %q, %v; want %q", id, got, ok, want)
		}
	}
	en := s.byLang[English]
	if len(en) != 1 || len(en[0].texts) != len(toolkitEnglish) {
		t.Fatalf("English holds %d catalogs; want one with exactly the %d pinned messages", len(en), len(toolkitEnglish))
	}
}

func TestToolkit_EveryShippedLanguageTranslatesEveryMessage(t *testing.T) {
	t.Parallel()
	s := Toolkit()
	var langs []string
	for lang := range s.byLang {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	want := append([]string(nil), shippedLanguages...)
	sort.Strings(want)
	if len(langs) != len(want) {
		t.Fatalf("shipped languages = %v, want %v", langs, want)
	}
	for i := range want {
		if langs[i] != want[i] {
			t.Fatalf("shipped languages = %v, want %v", langs, want)
		}
	}
	for _, lang := range shippedLanguages {
		texts := s.byLang[lang][0].texts
		for id := range toolkitEnglish {
			if _, ok := texts[id]; !ok {
				t.Errorf("%s has no translation of %s, so it would show English", lang, id)
			}
		}
		if len(texts) != len(toolkitEnglish) {
			t.Errorf("%s holds %d messages, want %d: an id English lacks is never looked up", lang, len(texts), len(toolkitEnglish))
		}
	}
}

func TestToolkit_EveryTranslationKeepsTheEnglishMnemonic(t *testing.T) {
	t.Parallel()
	s := Toolkit()
	for _, lang := range shippedLanguages {
		for id, en := range toolkitEnglish {
			text, _ := s.Lookup(lang, id)
			if got, want := marked(text), marked(en); got != want {
				t.Errorf("%s %s = %q marks %q; want %q, English's", lang, id, text, got, want)
			}
		}
	}
}

func TestToolkit_EachCallIsASeparateSet(t *testing.T) {
	t.Parallel()
	a, b := Toolkit(), Toolkit()
	_ = a.Add(catalog(t, "ko_KR", map[string]string{"tui.button.yes": "네(&y)"}))
	if got, _ := b.Lookup("ko_KR", "tui.button.yes"); got == "네(&y)" {
		t.Error("an application's catalog reached another Toolkit() set")
	}
}

// marked is the letter "&" marks, lower-cased, or 0.
func marked(text string) rune {
	rs := []rune(text)
	for i := 0; i+1 < len(rs); i++ {
		if rs[i] != '&' {
			continue
		}
		if rs[i+1] == '&' {
			i++
			continue
		}
		return unicode.ToLower(rs[i+1])
	}
	return 0
}
