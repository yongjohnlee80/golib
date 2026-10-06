package main

import (
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
)

// SWITCHING LANGUAGE FROM THE MENU — Option › Language.
//
// The language belongs to the App: the document binds its labels with qsTrId("editor.…"),
// the widgets look each id up as they are laid out, and switching is one call. Nothing is
// reloaded or rebuilt, so what is typed, where the cursor is and an open dialog all stay.
// The editor's own words are in i18n/editor_<language>.xml; golib's (the dialog buttons, the
// file dialogs) come with golib.

// languages are what Option › Language offers, each with the source that checks its row.
var languages = []struct{ tag, source string }{
	{"en", "App.langEn"},
	{"ko_KR", "App.langKo"},
	{"pt_BR", "App.langPt"},
	{"zh_CN", "App.langZh"},
}

// languageState is what the menu reads: which language is checked.
func languageState(tag string) map[string]any {
	st := map[string]any{}
	for _, l := range languages {
		st[l.source] = l.tag == tag
	}
	return st
}

// useLanguage switches the screen to tag. A language the menu does not offer is refused, and
// the screen stays as it was.
func (h *Host) useLanguage(tag string) error {
	offered := false
	for _, l := range languages {
		offered = offered || l.tag == tag
	}
	if !offered {
		return h.message(fmt.Sprintf("no language %q", tag))
	}
	h.p.App().SetLanguage(tag)
	return h.p.SetMany(languageState(tag))
}

// say puts a catalog message in the status bar's centre. It follows a later language switch.
func (h *Host) say(id string) error { return h.p.Set("App.status", tui.Msg(id)) }

// sayf puts a line built around arg in the status bar's centre, in the current language. It is
// composed once, so it keeps the language it was built in.
func (h *Host) sayf(id, arg string) error { return h.message(h.tr(id, arg)) }

// tr is id's text in the current language, with arg in place of its %1.
func (h *Host) tr(id, arg string) string {
	return strings.ReplaceAll(h.p.App().Translate(tui.Msg(id)), "%1", arg)
}
