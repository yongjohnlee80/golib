package widget_test

import (
	"errors"
	"io/fs"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/i18n"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Widgets showing a tui.Message read it in the App's language as they are laid out, follow
// App.SetLanguage on the next frame, and show plain text, once given it, exactly as given.

// appCatalogs is golib's catalogs with an application's over them, in the languages given as
// lang, id, text, id, text, ….
func appCatalogs(t *testing.T, langs map[string][]string) *i18n.Set {
	t.Helper()
	s := i18n.Toolkit()
	for lang, msgs := range langs {
		var b strings.Builder
		b.WriteString(`<TS version="2.1" language="` + lang + `"><context><name>app</name>`)
		for i := 0; i+1 < len(msgs); i += 2 {
			b.WriteString(`<message id="` + msgs[i] + `"><translation>` +
				strings.ReplaceAll(msgs[i+1], "&", "&amp;") + `</translation></message>`)
		}
		b.WriteString(`</context></TS>`)
		c, err := i18n.ParseTS(strings.NewReader(b.String()))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Add(c); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestMessages_EachWidgetFollowsTheLanguageAndPlainTextStaysPut(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		build           func() (root tui.Component, setMsg, setPlain func())
		english, korean string
	}{
		{"Button", func() (tui.Component, func(), func()) {
			b := widget.NewButton("before")
			return b, func() { b.SetLabelMessage(tui.Msg("tui.button.cancel")) }, func() { b.SetLabel("plain") }
		}, "[ Cancel ]", "[ 취소(c) ]"},
		{"Text", func() (tui.Component, func(), func()) {
			x := widget.NewText("before")
			return x, func() { x.SetTextMessage(tui.Msg("tui.files.preview")) }, func() { x.SetText("plain") }
		}, "Preview", "미리 보기"},
		{"StatusBar", func() (tui.Component, func(), func()) {
			sb := widget.NewStatusBar()
			return sb, func() { sb.SetLeftMessage(tui.Msg("tui.files.folder")) }, func() { sb.SetLeft("plain") }
		}, "Folder", "폴더"},
		{"Box", func() (tui.Component, func(), func()) {
			bx := widget.NewBox(widget.NewText(""), widget.WithTitle("before"))
			return bx, func() { bx.SetTitleMessage(tui.Msg("tui.files.fileName")) }, func() { bx.SetTitle("plain") }
		}, "File name", "파일 이름"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, setMsg, setPlain := tc.build()
			h := startApp(t, root, 40, 3)
			defer h.stop()
			h.onLoop(setMsg)
			h.shows(tc.english)
			h.app.SetLanguage("ko_KR")
			h.shows(tc.korean)
			h.onLoop(setPlain)
			h.shows("plain")
			h.app.SetLanguage("ja_JP")
			h.settle()
			h.wantContains("plain") // a plain setter ends the message: nothing translates "plain"
		})
	}
}

func TestMessages_APlaceholderFollowsTheLanguage(t *testing.T) {
	t.Parallel()
	in := widget.NewTextInput(widget.WithPlaceholderMessage(tui.Msg("tui.files.empty")))
	h := startApp(t, in, 30, 1)
	defer h.stop()
	h.shows("(empty)")
	h.app.SetLanguage("ko_KR")
	h.shows("(비어 있음)")
}

func TestMessages_AnOpenDialogsTitleAndFooterFollowTheLanguage(t *testing.T) {
	t.Parallel()
	md := widget.NewModal(widget.NewText("body"),
		widget.WithModalTitleMessage(tui.Msg("tui.files.folder")),
		widget.WithButtons(widget.NewStandardButton(widget.StandardYes), widget.NewStandardButton(widget.StandardNo)))
	host := widget.NewOverlayHost(widget.NewText(""))
	h := startApp(t, host, 60, 8)
	defer h.stop()
	h.onLoop(func() {
		md.SetFooterMessage(tui.Msg("tui.files.hint.default"))
		if err := md.Open(host); err != nil {
			t.Fatal(err)
		}
	})
	h.shows("Folder")
	h.shows("Enter:press  Tab:next  Esc:cancel")
	h.shows("[ Yes ]")
	h.app.SetLanguage("ko_KR")
	h.shows("폴더")
	h.shows("Enter:누르기  Tab:다음  Esc:취소")
	h.shows("[ 예(y) ]")
	h.shows("[ 아니요(n) ]")
	var open bool
	h.onLoop(func() { open = md.IsOpen() })
	if !open {
		t.Error("the dialog closed on a language change; it must stay open")
	}
}

func TestStandardButtons_LabelRoleAndKeyInEnglishByteForByte(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		b      widget.StandardButton
		shown  string
		role   widget.ButtonRole
		hotkey rune
	}{
		{widget.StandardOk, "[ OK ]", widget.ButtonRoleAccept, 'o'},
		{widget.StandardSave, "[ Save ]", widget.ButtonRoleAccept, 's'},
		{widget.StandardYes, "[ Yes ]", widget.ButtonRoleAccept, 'y'},
		{widget.StandardNo, "[ No ]", widget.ButtonRoleReject, 'n'},
		{widget.StandardCancel, "[ Cancel ]", widget.ButtonRoleReject, 'c'},
		{widget.StandardClose, "[ Close(q) ]", widget.ButtonRoleReject, 'q'},
	} {
		b := widget.NewStandardButton(tc.b)
		// Before it is mounted the button already carries English and its key, so a dialog
		// validating its buttons at construction sees the key it will answer to.
		if b.Role() != tc.role || b.Mnemonic() != tc.hotkey {
			t.Errorf("%v before mount: role %v, key %q; want %v, %q", tc.b, b.Role(), b.Mnemonic(), tc.role, tc.hotkey)
		}
		h := startApp(t, b, 20, 1)
		h.shows(tc.shown)
		h.stop()
	}
}

func TestStandardButtons_KeepTheirEnglishKeyInEveryShippedLanguage(t *testing.T) {
	t.Parallel()
	b := widget.NewStandardButton(widget.StandardClose)
	h := startApp(t, b, 30, 1)
	defer h.stop()
	for _, tc := range []struct{ lang, shown string }{
		{"ko_KR", "[ 닫기(q) ]"}, {"ja_JP", "[ 閉じる(q) ]"}, {"zh_CN", "[ 关闭(q) ]"},
		{"pt_BR", "[ Fechar(q) ]"}, {"es", "[ Cerrar(q) ]"}, {"en", "[ Close(q) ]"},
	} {
		h.app.SetLanguage(tc.lang)
		h.shows(tc.shown)
		var key rune
		h.onLoop(func() { key = b.Mnemonic() })
		if key != 'q' {
			t.Errorf("%s: key %q, want q", tc.lang, key)
		}
	}
}

// underlinedColumns are the columns of row y whose cell is underlined.
func underlinedColumns(h *harness, y int) []int {
	var cols []int
	for x, c := range h.tb.Snapshot()[y] {
		if c.Attrs.Mask&tui.AttrUnderline != 0 {
			cols = append(cols, x)
		}
	}
	return cols
}

func TestButton_UnderlinesTheMarkedLetterNotTheFirstMatch(t *testing.T) {
	t.Parallel()
	s := appCatalogs(t, map[string][]string{
		"en":    {"app.saveAs", "Save &As…"},
		"pt_BR": {"app.saveAs", "Salvar como(&a)…"},
	})
	b := widget.NewButton("", widget.WithLabelMessage(tui.Msg("app.saveAs")))
	h := startAppOpts(t, b, 30, 1, tui.WithTranslations(s), tui.WithLanguage("pt_BR"))
	defer h.stop()
	h.shows("[ Salvar como(a)… ]")
	row := h.row(0)
	want := len([]rune(row[:strings.Index(row, "(a)")])) + 1 // the a inside the parentheses
	got := underlinedColumns(h, 0)
	if len(got) != 1 || got[0] != want {
		t.Errorf("underlined columns %v, want only %d (the marked a), not the a of Salvar:\n%s", got, want, row)
	}
}

func TestButton_AKeyGivenAloneStillUnderlinesTheFirstMatch(t *testing.T) {
	t.Parallel()
	b := widget.NewButton("Salvar como", widget.WithMnemonic('a'))
	h := startApp(t, b, 30, 1)
	defer h.stop()
	h.shows("[ Salvar como ]")
	row := h.row(0)
	want := strings.Index(row, "a") // "[ Sa…": the first a
	if got := underlinedColumns(h, 0); len(got) != 1 || got[0] != want {
		t.Errorf("underlined columns %v, want only %d, the first a:\n%s", got, want, row)
	}
}

func TestMenu_AMessageRowFollowsTheLanguageInAnOpenDropdownAndAnswersTheEnglishLetter(t *testing.T) {
	t.Parallel()
	s := appCatalogs(t, map[string][]string{
		"en":    {"app.file", "&File", "app.quit", "E&xit"},
		"ko_KR": {"app.file", "파일(&f)", "app.quit", "끝내기(&x)"},
	})
	file := widget.NewSubmenu("file", "", []widget.MenuItemModel{widget.NewCommand("quit", "", nil)})
	file.LabelMsg = tui.Msg("app.file")
	file.Children[0].LabelMsg = tui.Msg("app.quit")
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{file}); err != nil {
		t.Fatal(err)
	}
	host := widget.NewOverlayHost(widget.NewMenuBar(m))
	h := startAppOpts(t, host, 40, 8, tui.WithTranslations(s))
	defer h.stop()
	var activated atomic.Value
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.MenuActivatedEvent) { activated.Store(ev.ItemID) })
	defer unsub()

	h.onLoop(func() { m.Context().RequestFocus() })
	h.shows("File")
	h.onLoop(func() {
		if err := m.Open("file"); err != nil {
			t.Error(err)
		}
	})
	h.shows("Exit")
	h.app.SetLanguage("ko_KR")
	h.shows("파일(f)")
	h.shows("끝내기(x)") // the open dropdown re-resolved, without being reopened
	h.wantNotContains("Exit")

	h.inject(key('x'))
	h.waitFor("the quit row's activation", func() bool { return activated.Load() == widget.ItemID("quit") })
}

func TestFileViews_HintsAndNotesFollowTheLanguage(t *testing.T) {
	t.Parallel()
	src := widget.FileSource{FS: fstest.MapFS{"docs/a.txt": {Data: []byte("a")}}, Root: "/"}
	v := widget.NewFileOpenView(widget.WithFileViewSource(src), widget.WithFileViewDir("/"))
	h := startBrowser(t, v)
	defer h.stop()
	var hint string
	h.onLoop(func() { hint = v.Hint() })
	if hint != "↑↓:move  Enter:open folder, or open file  Tab:next  Esc:cancel" {
		t.Errorf("English hint = %q, unchanged from the literal it replaces", hint)
	}
	h.shows("Preview")
	h.app.SetLanguage("ko_KR")
	h.shows("미리 보기")
	h.onLoop(func() { hint = v.Hint() })
	if hint != "↑↓:이동  Enter:폴더 또는 파일 열기  Tab:다음  Esc:취소" {
		t.Errorf("Korean hint = %q", hint)
	}
	var msg tui.Message
	h.onLoop(func() { msg = v.HintMessage() })
	if msg != tui.Msg("tui.files.hint.openList") {
		t.Errorf("HintMessage() = %v, want tui.files.hint.openList", msg)
	}
}

func TestFilePreview_ANoteReadsInTheNewLanguage(t *testing.T) {
	t.Parallel()
	p := widget.NewFilePreview(widget.FilePaneStyles{}, false)
	h := startApp(t, p, 40, 6)
	defer h.stop()
	src := widget.FileSource{FS: fstest.MapFS{"docs/a.txt": {Data: []byte("a")}}, Root: "/"}
	h.onLoop(func() { p.Show(src, "docs", true) })
	h.shows("(folder)")
	h.app.SetLanguage("ko_KR")
	h.shows("(폴더)")
	var text string
	h.onLoop(func() { text = p.Text() })
	if text != "(폴더)" {
		t.Errorf("Text() = %q, want the note in Korean", text)
	}
}

func TestSelect_ALoadErrorReadsInTheNewLanguage(t *testing.T) {
	t.Parallel()
	s := widget.NewSelect(widget.WithWidth[int](24))
	h := startApp(t, s, 30, 1)
	defer h.stop()
	h.onLoop(func() { s.HandleEvent(tui.TaskResult{Owner: s.NodeID(), Err: errors.New("no route")}) })
	h.shows("error: no route")
	h.app.SetLanguage("ko_KR")
	h.shows("오류: no route")
}

func TestList_AnEmptyListsTextFollowsTheLanguageAndPlainTextStaysPut(t *testing.T) {
	t.Parallel()
	l := widget.NewList(widget.WithItems[string](nil, func(s string) string { return s }), widget.WithEmptyTextMessage[string](tui.Msg("tui.files.empty")))
	h := startApp(t, l, 30, 3)
	defer h.stop()
	h.shows("(empty)")
	h.app.SetLanguage("ko_KR")
	h.shows("(비어 있음)")

	plain := widget.NewList(widget.WithItems[string](nil, func(s string) string { return s }), widget.WithEmptyTextMessage[string](tui.Msg("tui.files.empty")),
		widget.WithEmptyText[string]("nothing here"))
	h2 := startAppOpts(t, plain, 30, 3, tui.WithLanguage("ko_KR"))
	defer h2.stop()
	h2.shows("nothing here")
}

func TestFileSaveView_TheNamePaneAndHintsFollowTheLanguage(t *testing.T) {
	t.Parallel()
	src := widget.FileSource{FS: fstest.MapFS{"docs/a.txt": {Data: []byte("a")}}, Root: "/"}
	v := widget.NewFileSaveView(widget.WithFileViewSource(src), widget.WithFileViewDir("/"))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("File name")
	var hint string
	h.onLoop(func() { hint = v.Hint() })
	if hint != "Enter:save  Tab:next  Esc:cancel" {
		t.Errorf("English hint = %q, unchanged from the literal it replaces", hint)
	}
	h.app.SetLanguage("ko_KR")
	h.shows("파일 이름")
	h.onLoop(func() { hint = v.Hint() })
	if hint != "Enter:저장  Tab:다음  Esc:취소" {
		t.Errorf("Korean hint = %q", hint)
	}
}

// A title set on an open dialog follows the language, and a plain title set after it ends the
// message: it stays as written whatever the language.
func TestMessages_AnOpenModalsTitleBecomesAMessageAndPlainAgain(t *testing.T) {
	t.Parallel()
	md := widget.NewModal(widget.NewText("body"), widget.WithModalTitle("plain title"))
	host := widget.NewOverlayHost(widget.NewText(""))
	h := startApp(t, host, 60, 8)
	defer h.stop()
	h.onLoop(func() {
		if err := md.Open(host); err != nil {
			t.Error(err)
		}
	})
	h.shows("plain title")
	h.onLoop(func() { md.SetTitleMessage(tui.Msg("tui.files.folder")) })
	h.shows("Folder")
	h.app.SetLanguage("ko_KR")
	h.shows("폴더")
	h.onLoop(func() {
		md.SetTitle("written")
		md.SetFooter("a help line")
	})
	h.shows("written")
	h.shows("a help line")
	h.app.SetLanguage("ja_JP")
	h.settle()
	h.wantContains("written")
	h.wantNotContains("폴더")
}

// A file list's and a preview's hints are their keys in the App's language, and the message
// each names is the one it shows.
func TestFileListAndPreview_HintsFollowTheLanguage(t *testing.T) {
	t.Parallel()
	src := widget.FileSource{FS: fstest.MapFS{"a.txt": {Data: []byte("a")}}, Root: "/"}
	l := widget.NewFileList(widget.WithFileListSource(src), widget.WithFileListDir("/"))
	p := widget.NewFilePreview(widget.FilePaneStyles{}, false)
	for _, tc := range []struct {
		name            string
		root            tui.Component
		hint            func() (string, tui.Message)
		msg             tui.Message
		english, korean string
	}{
		{"FileList", l, func() (string, tui.Message) { return l.Hint(), l.HintMessage() },
			tui.Msg("tui.files.hint.list"), "↑↓:move  Enter:open folder", "↑↓:이동  Enter:폴더 열기"},
		{"FilePreview", p, func() (string, tui.Message) { return p.Hint(), p.HintMessage() },
			tui.Msg("tui.files.hint.preview"), "j/k:scroll  gg/G:top/end", "j/k:스크롤  gg/G:처음/끝"},
	} {
		h := startApp(t, tc.root, 60, 6)
		var hint string
		var msg tui.Message
		h.onLoop(func() { hint, msg = tc.hint() })
		if hint != tc.english || msg != tc.msg {
			t.Errorf("%s in English: %q, %v; want %q, %v", tc.name, hint, msg, tc.english, tc.msg)
		}
		h.app.SetLanguage("ko_KR")
		h.settle()
		h.onLoop(func() { hint, _ = tc.hint() })
		if hint != tc.korean {
			t.Errorf("%s in Korean: %q, want %q", tc.name, hint, tc.korean)
		}
		h.stop()
	}
}

// failingFS opens every file, and fails every read of one.
type failingFS struct{ fstest.MapFS }

type failingFile struct{ fs.File }

func (f failingFile) Read([]byte) (int, error) { return 0, errors.New("disk on fire") }

func (s failingFS) Open(name string) (fs.File, error) {
	f, err := s.MapFS.Open(name)
	if err != nil || name != "bad.txt" {
		return f, err
	}
	return failingFile{f}, nil
}

// A preview's notes about a file it cannot show — empty, missing, unreadable — are in the
// App's language, with the reason as written.
func TestFilePreview_NotesAboutAFileItCannotShowFollowTheLanguage(t *testing.T) {
	t.Parallel()
	src := widget.FileSource{FS: failingFS{fstest.MapFS{"empty.txt": {}, "bad.txt": {Data: []byte("x")}}}, Root: "/"}
	for _, tc := range []struct{ path, english, korean string }{
		{"empty.txt", "(empty file)", "(빈 파일)"},
		{"gone.txt", "(cannot read: open gone.txt: file does not exist)", "(읽을 수 없음: open gone.txt: file does not exist)"},
		{"bad.txt", "(cannot read: disk on fire)", "(읽을 수 없음: disk on fire)"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			p := widget.NewFilePreview(widget.FilePaneStyles{}, false)
			h := startApp(t, p, 70, 4)
			defer h.stop()
			h.onLoop(func() { p.Show(src, tc.path, false) })
			h.shows(tc.english)
			h.app.SetLanguage("ko_KR")
			h.shows(tc.korean)
		})
	}
}
