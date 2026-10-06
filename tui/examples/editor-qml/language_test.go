package main

import (
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// TestEveryLanguageIsComplete: each of the editor's languages translates every message
// English has, keeps English's mnemonic letter, and gives no two rows of one menu level the
// same key.
func TestEveryLanguageIsComplete(t *testing.T) {
	h := &Host{}
	decltest.CheckLanguages(t, h.options(Options{Now: fixedNow, Tick: time.Hour})...)
}

// pickLanguage chooses Option › Language › name from the menu bar as it reads now: option and
// language are the two menus' titles in the language showing.
func (r *running) pickLanguage(t *testing.T, option, language, name string) {
	t.Helper()
	r.clickLabel(t, 0, option)
	r.waitFor(t, "the Option dropdown", func(s string) bool { return strings.Contains(s, language) })
	r.clickLabel(t, rowOf(r.rows(), language), language)
	r.waitFor(t, "the Language submenu", func(s string) bool { return strings.Contains(s, name) })
	r.clickLabel(t, rowOf(r.rows(), name), name)
}

// TestTheLanguageMenuRelabelsTheScreenAndKeepsTheBuffer: choosing 한국어 relabels the menu bar
// and the status line in place, with what was typed still there, and English comes back the
// same way.
func TestTheLanguageMenuRelabelsTheScreenAndKeepsTheBuffer(t *testing.T) {
	r := start(t, "")
	r.key(t, runeKey('i'), runeKey('k'), runeKey('e'), runeKey('p'), runeKey('t'), escape)
	r.waitFor(t, "the text", func(s string) bool { return strings.Contains(s, "kept") })

	r.pickLanguage(t, "Option", "Language", "2. 한국어")
	r.waitFor(t, "the Korean screen", func(s string) bool {
		rows := strings.Split(s, "\n")
		return strings.Contains(rows[0], "파일(f)") && strings.Contains(rows[0], "도움말(h)") &&
			strings.Contains(lastNonEmpty(rows), "일반") && strings.Contains(lastNonEmpty(rows), "[이름 없음]")
	})
	if !strings.Contains(r.screen(), "kept") {
		t.Errorf("switching language lost what was typed:\n%s", r.screen())
	}

	r.pickLanguage(t, "옵션(o)", "언어(l)", "1. English")
	r.waitFor(t, "English again", func(s string) bool {
		rows := strings.Split(s, "\n")
		return strings.Contains(rows[0], "File") && strings.Contains(lastNonEmpty(rows), "NORMAL")
	})
}

// TestSwitchingLanguageKeepsAnOpenDialog: the Quit dialog stays open across a switch, and
// its title, question and golib's own buttons all read in the new language.
func TestSwitchingLanguageKeepsAnOpenDialog(t *testing.T) {
	r := start(t, "")
	r.key(t, runeKey('i'), runeKey('x'), escape) // unsaved, so the question says so
	r.key(t, ctrl('q'))
	r.waitFor(t, "the quit dialog", func(s string) bool { return strings.Contains(s, "Unsaved changes will be lost.") })

	r.s.Program.Post(func() { _ = r.host.useLanguage("pt_BR") })
	r.waitFor(t, "the dialog in Portuguese", func(s string) bool {
		return strings.Contains(s, "As alterações não salvas serão perdidas.") &&
			strings.Contains(s, "Sim(y)") && strings.Contains(s, "Não")
	})
	r.key(t, runeKey('n')) // No is still on n
	r.notQuit(t)
}

// TestTheEnglishLetterOpensTheMenuInEveryLanguage: Alt+F is File, x Exit and y Yes, whatever
// the labels say.
func TestTheEnglishLetterOpensTheMenuInEveryLanguage(t *testing.T) {
	for lang, want := range map[string]struct{ exit, question string }{
		"ko_KR": {"끝내기(x)", "정말 끝내시겠습니까?"},
		"pt_BR": {"Sair(x)", "Tem certeza de que deseja sair?"},
		"zh_CN": {"退出(x)", "确定要退出吗？"},
	} {
		t.Run(lang, func(t *testing.T) {
			r := startOpts(t, Options{Language: lang, Now: fixedNow, Tick: time.Hour}, 80, 14)
			r.key(t, alt('f'))
			r.waitFor(t, "the File dropdown", func(s string) bool { return strings.Contains(s, want.exit) })
			r.key(t, runeKey('x'))
			r.waitFor(t, "the quit dialog", func(s string) bool { return strings.Contains(s, want.question) })
			r.key(t, runeKey('y'))
			r.quits(t, "Yes, by its English letter")
		})
	}
}

// TestAComposedStatusLineIsInTheLanguageShowing: a line built around a value is written in
// the language showing when it is built.
func TestAComposedStatusLineIsInTheLanguageShowing(t *testing.T) {
	r := startOpts(t, Options{Language: "zh_CN", Now: fixedNow, Tick: time.Hour}, 80, 14)
	r.key(t, ctrl('p'))
	r.waitFor(t, "the prompt", func(s string) bool { return strings.Contains(s, "┌ 命令 ") })
	r.key(t, runeKey('z'), runeKey('z'), enter)
	r.waitFor(t, "the refusal", func(s string) bool {
		return strings.Contains(lastNonEmpty(strings.Split(s, "\n")), "不是命令：zz")
	})
}
