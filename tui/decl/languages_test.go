package decl_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/i18n"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// languages_test.go: qsTrId and the App's language, on what reaches the screen — a label
// that is translated but never redrawn passes every structural test.

// tsCatalog is a TS catalog for lang holding id, text pairs.
func tsCatalog(lang string, pairs ...string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n" +
		`<TS version="2.1" language="` + lang + `"><context><name>demo</name>`)
	for i := 0; i+1 < len(pairs); i += 2 {
		b.WriteString(`<message id="` + pairs[i] + `"><translation>` +
			strings.ReplaceAll(pairs[i+1], "&", "&amp;") + `</translation></message>`)
	}
	b.WriteString(`</context></TS>`)
	return []byte(b.String())
}

// demoCatalogs is an application's catalogs: English and Korean, a language golib does not
// ship (German), and a Korean rewording of one of golib's own buttons.
func demoCatalogs() fstest.MapFS {
	return fstest.MapFS{
		"i18n/app_en.xml": {Data: tsCatalog("en",
			"demo.menu.file", "&File", "demo.file.save", "&Save", "demo.greeting", "Hello",
			"demo.dialog.title", "Unsaved", "demo.status", "ready",
			"demo.go", "&Go", "demo.wrap", "&Wrap", "demo.panel", "Panel", "demo.help", "press a key",
			"demo.hint", "type here")},
		"i18n/app_ko_KR.xml": {Data: tsCatalog("ko_KR",
			"demo.menu.file", "파일(&f)", "demo.file.save", "저장(&s)", "demo.greeting", "안녕",
			"demo.dialog.title", "저장 안 됨", "demo.status", "준비",
			"tui.button.cancel", "그만(&c)",
			"demo.go", "가기(&g)", "demo.wrap", "줄 바꿈(&w)", "demo.panel", "패널", "demo.help", "키를 누르세요",
			"demo.hint", "여기에 입력")},
		"i18n/app_de.xml": {Data: tsCatalog("de", "demo.greeting", "Hallo")},
		"i18n/notes.txt":  {Data: []byte("not a catalog")},
	}
}

const languagesDoc = `import tui 1.0
Window {
 MenuBar { Dock.edge: Tui.Top
  Menu { title: qsTrId("demo.menu.file")
   MenuItem { text: qsTrId("demo.file.save") } } }
 Text { text: qsTrId("demo.greeting") }
 Dialog { id: d; title: qsTrId("demo.dialog.title"); standardButtons: Dialog.Ok | Dialog.Cancel
  Text { text: "body" } }
}`

func runLanguages(t *testing.T, doc string, extra ...tuidecl.ProgramOption) *decltest.Screen {
	t.Helper()
	return decltest.Run(t, 60, 14, append([]tuidecl.ProgramOption{
		tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Translations(demoCatalogs(), "i18n", "app"),
	}, extra...)...)
}

func setLanguage(t *testing.T, s *decltest.Screen, tag string) {
	t.Helper()
	onScreenLoop(t, s, func() { s.Program.App().SetLanguage(tag) })
}

func openDialog(t *testing.T, s *decltest.Screen, id string) {
	t.Helper()
	onScreenLoop(t, s, func() {
		if err := s.Program.Call(id, "open"); err != nil {
			t.Error(err)
		}
	})
}

func waitAll(t *testing.T, s *decltest.Screen, what string, subs ...string) {
	t.Helper()
	s.WaitFor(t, what, func(sc string) bool {
		for _, sub := range subs {
			if !strings.Contains(sc, sub) {
				return false
			}
		}
		return true
	})
}

// A Text, a Menu's title, a MenuItem, a Dialog's title and its standard buttons all follow
// App.SetLanguage — the dialog while it is open — and the menu keeps English's key.
func TestQsTrIdFollowsTheAppsLanguage(t *testing.T) {
	s := runLanguages(t, languagesDoc)
	waitAll(t, s, "English", "Hello", "File")
	openDialog(t, s, "d")
	waitAll(t, s, "the English dialog", "┌ Unsaved ", "[ OK ]", "[ Cancel ]")

	setLanguage(t, s, "ko_KR")
	waitAll(t, s, "the open dialog in Korean", "┌ 저장 안 됨 ", "[ 확인(o) ]", "[ 그만(c) ]")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	waitAll(t, s, "the Korean screen", "안녕", "파일(f)")
	if sc := s.String(); strings.Contains(sc, "Hello") || strings.Contains(sc, "File") {
		t.Errorf("English is left on the Korean screen:\n%s", sc)
	}
	s.Keys(t, decltest.Alt('f'))
	s.WaitForText(t, "저장(s)")
}

// Every other property showing UI text takes a message, and follows the language: a Button, a
// CheckBox, a Frame's title, a Dialog's helpText and a TextField's placeholder.
func TestEveryUITextPropertyFollowsTheLanguage(t *testing.T) {
	s := decltest.Run(t, 70, 16, tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window {
 Flex { direction: Tui.Vertical
  Button { text: qsTrId("demo.go") }
  CheckBox { text: qsTrId("demo.wrap") }
  Frame { title: qsTrId("demo.panel"); TextField { placeholderText: qsTrId("demo.hint") } } }
 Dialog { id: d; title: "help"; helpText: qsTrId("demo.help"); Text { text: "body" } }
}`)), tuidecl.Types(controls.Types()...), tuidecl.Translations(demoCatalogs(), "i18n", "app"))
	waitAll(t, s, "English", "[ Go ]", "Wrap", "┌ Panel ", "type here")
	setLanguage(t, s, "ko_KR")
	waitAll(t, s, "Korean", "[ 가기(g) ]", "줄 바꿈(w)", "┌ 패널 ", "여기에 입력")
	openDialog(t, s, "d")
	s.WaitForText(t, "키를 누르세요")
	setLanguage(t, s, "en")
	s.WaitForText(t, "press a key")
}

// A language golib does not ship comes from the application's catalogs alone; golib's own
// buttons fall back to English in it.
func TestADownstreamLanguageFallsBackToEnglishForGolibsText(t *testing.T) {
	s := runLanguages(t, languagesDoc, tuidecl.AppOptions(tui.WithLanguage("de")))
	waitAll(t, s, "German, and English where German has nothing", "Hallo", "File")
	openDialog(t, s, "d")
	waitAll(t, s, "golib's buttons in English", "┌ Unsaved ", "[ OK ]", "[ Cancel ]")
}

// A host publishes a message as a source; its StatusBar shows it in each language with
// nothing republished.
func TestAHostMessageFollowsTheLanguage(t *testing.T) {
	s := runLanguages(t, "import tui 1.0\nimport demo 1.0\nWindow {\n Text { text: \"body\" }\n"+
		" StatusBar { Dock.edge: Tui.Bottom; left: App.status } }",
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.status": tui.Msg("demo.status")}))
	s.WaitForText(t, "ready")
	setLanguage(t, s, "ko_KR")
	s.WaitForText(t, "준비")
	if v, err := tuidecl.Value(tui.Msg("demo.status")); err != nil || v.Obj != tui.Msg("demo.status") {
		t.Errorf("Value(tui.Msg) = %+v, %v; want the message as an object", v, err)
	}
}

// A FileDialog's footer is its view's hint as a message, so it follows the language while
// the dialog is open.
func TestAFileDialogsFooterFollowsTheLanguage(t *testing.T) {
	files := widget.FileSource{FS: fstest.MapFS{"foo.txt": {Data: []byte("x")}}, Root: "mem://"}
	s := decltest.Run(t, 90, 22, tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow {\n Text { text: \"body\" }\n FileDialog { id: fd } }")),
		tuidecl.Translations(demoCatalogs(), "i18n", "app"), tuidecl.Files(files))
	s.WaitForText(t, "body")
	openDialog(t, s, "fd")
	waitAll(t, s, "the English picker", "foo.txt", "Esc:cancel", "[ Open ]")
	setLanguage(t, s, "ko_KR")
	waitAll(t, s, "the Korean footer and buttons", "Esc:취소", "[ 열기(o) ]", "[ 닫기(q) ]")
}

// An AppOptions tui.WithTranslations is the App's own option, and wins over Translations.
func TestTheAppsOwnTranslationsWin(t *testing.T) {
	var own i18n.Set
	c, err := i18n.ParseTS(strings.NewReader(string(tsCatalog("en", "demo.greeting", "Howdy"))))
	if err != nil {
		t.Fatal(err)
	}
	if err := own.Add(c); err != nil {
		t.Fatal(err)
	}
	s := runLanguages(t, languagesDoc, tuidecl.AppOptions(tui.WithTranslations(&own)))
	s.WaitForText(t, "Howdy")
}

// qsTrId takes one message id, and Editor.text — a document, not UI text — takes no message;
// each refusal says where.
func TestWhatQsTrIdAndUITextRefuse(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Text { text: qsTrId() }`, "qsTrId takes one string"},
		{`Text { text: qsTrId(1) }`, "qsTrId takes one string"},
		{`Text { text: qsTrId("") }`, "qsTrId takes one string"},
		{`Editor { text: qsTrId("demo.greeting") }`, "want a string, got object"},
		{`Button { text: 3 }`, `a catalog message is written qsTrId("id")`},
	} {
		err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow {\n "+c.src+" }")))
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "main.qml:3") {
			t.Errorf("%s: err = %v, want %q at main.qml:3", c.src, err, c.want)
		}
	}
	if _, err := tuidecl.Value(struct{}{}); err == nil || !strings.Contains(err.Error(), "tui.Message") {
		t.Errorf("Value's refusal does not name tui.Message: %v", err)
	}
}

// A catalog that does not load fails NewProgram, naming its file.
func TestABadCatalogFailsNewProgram(t *testing.T) {
	files := demoCatalogs()
	files["i18n/app_es.xml"] = &fstest.MapFile{Data: []byte("<TS><context>")}
	_, err := tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte(languagesDoc)),
		tuidecl.Translations(files, "i18n", "app"))
	if err == nil || !strings.Contains(err.Error(), "app_es.xml") {
		t.Fatalf("err = %v, want the catalog named", err)
	}
}

// Under HotReload a saved catalog relabels the screen; a broken one is refused and the screen
// kept; and a good catalog saved beside a broken layout waits for the layout.
func TestHotReloadFollowsTheCatalogs(t *testing.T) {
	d := newHotDir(t, hotLayout(`Text { text: qsTrId("demo.greeting") }`))
	if err := os.MkdirAll(filepath.Join(d.root, "i18n"), 0o755); err != nil {
		t.Fatal(err)
	}
	d.write("i18n/app_en.xml", string(tsCatalog("en", "demo.greeting", "greeting v1")))
	s := d.run(tuidecl.Translations(os.DirFS(d.root), "i18n", "app"))
	s.WaitForText(t, "greeting v1")

	d.write("i18n/app_en.xml", string(tsCatalog("en", "demo.greeting", "greeting v2")))
	s.WaitForText(t, "greeting v2")

	_, before := d.counts()
	d.write("i18n/app_en.xml", "<TS><context><message")
	s.WaitFor(t, "the broken catalog refused", func(string) bool { _, n := d.counts(); return n == before+1 })
	if d.mu.Lock(); !strings.Contains(d.refusals[before].Error(), "app_en.xml") {
		t.Errorf("the refusal does not name the catalog: %v", d.refusals[before])
	}
	d.mu.Unlock()

	d.write("i18n/app_en.xml", string(tsCatalog("en", "demo.greeting", "greeting v3")))
	d.write("main.qml", hotLayout(`Text { txt: qsTrId("demo.greeting") }`))
	s.WaitFor(t, "the broken layout refused", func(string) bool { _, n := d.counts(); return n == before+2 })
	settle()
	if sc := s.String(); !strings.Contains(sc, "greeting v2") || strings.Contains(sc, "greeting v3") {
		t.Errorf("a refused reload relabelled the screen:\n%s", sc)
	}
	d.write("main.qml", hotLayout(`Text { text: qsTrId("demo.greeting") }`))
	s.WaitForText(t, "greeting v3")
}

// With no OnReloadError, a refused catalog reaches the ErrorSink.
func TestARefusedCatalogReachesTheSink(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "i18n"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(file string, src []byte) {
		if err := os.WriteFile(filepath.Join(root, file), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.qml", []byte("import tui 1.0\nText { text: qsTrId(\"demo.greeting\") }"))
	write("i18n/app_en.xml", tsCatalog("en", "demo.greeting", "sunk v1"))
	var mu sync.Mutex
	var sunk []error
	fsys := os.DirFS(root)
	s := decltest.Run(t, 40, 4, tuidecl.Layout(fsys, "main.qml"),
		tuidecl.Translations(fsys, "i18n", "app"),
		tuidecl.HotReload(tuidecl.ReloadInterval(tick)),
		tuidecl.ErrorSink(func(err error) { mu.Lock(); sunk = append(sunk, err); mu.Unlock() }))
	s.WaitForText(t, "sunk v1")
	write("i18n/app_en.xml", []byte("<TS><context><message"))
	s.WaitFor(t, "the refusal sunk", func(string) bool {
		mu.Lock()
		defer mu.Unlock()
		return len(sunk) == 1 && strings.Contains(sunk[0].Error(), "app_en.xml")
	})
	if !strings.Contains(s.String(), "sunk v1") {
		t.Errorf("a refused catalog lost the screen:\n%s", s)
	}
}

// CheckLanguages passes golib's own catalogs, and reports a marker on another letter than
// English's, a message English lacks, and two rows answering to one key.
func TestCheckLanguagesJudgesTheCatalogsAgainstTheMenus(t *testing.T) {
	decltest.CheckLanguages(t, tuidecl.LayoutSource("main.qml", []byte(languagesDoc)),
		tuidecl.Translations(demoCatalogs(), "i18n", "app"))

	files := fstest.MapFS{
		"i18n/app_en.xml":    {Data: tsCatalog("en", "demo.apple", "&Apple", "demo.pear", "&Pear")},
		"i18n/app_ko_KR.xml": {Data: tsCatalog("ko_KR", "demo.apple", "사과", "demo.pear", "배(&b)", "demo.extra", "더")},
	}
	err := tuidecl.CheckLanguages(tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window {
 MenuBar { Dock.edge: Tui.Top
  Menu { title: "&Fruit"
   MenuItem { text: "&Avocado" }
   MenuItem { text: qsTrId("demo.apple") }
   MenuItem { text: qsTrId("demo.pear") } } }
 Text { }
}`)), tuidecl.Translations(files, "i18n", "app"))
	if err == nil {
		t.Fatal("CheckLanguages found nothing")
	}
	got := err.Error()
	for _, want := range []string{
		`ko_KR: message "demo.pear" is "배(&b)", marking 'b' where English marks 'p'`,
		`ko_KR: message "demo.extra" has no English`,
		`menu "Fruit": "Avocado" and "Apple" both answer to 'a'`,
		"the first keeps the key",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("CheckLanguages does not report %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "both answer to"); n != 1 {
		t.Errorf("the collision is reported %d times, want once across the languages:\n%s", n, got)
	}
}

// framesBackend is a TestBackend that keeps every frame it is sent, as text: what a reload
// shows on the way, not only where it settles.
type framesBackend struct {
	*tui.TestBackend
	mu     sync.Mutex
	frames []string
}

func (b *framesBackend) Flush(diff []tui.CellUpdate) error {
	err := b.TestBackend.Flush(diff)
	b.mu.Lock()
	b.frames = append(b.frames, b.TestBackend.String())
	b.mu.Unlock()
	return err
}

func (b *framesBackend) seen() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.frames...)
}

// swapFS serves one set of files, then another, all at once: a layout and its catalog
// changed together, which two writes to a folder cannot promise.
type swapFS struct {
	mu  sync.Mutex
	cur fstest.MapFS
}

func (s *swapFS) Open(name string) (fs.File, error) {
	s.mu.Lock()
	m := s.cur
	s.mu.Unlock()
	return m.Open(name)
}

func (s *swapFS) swap(m fstest.MapFS) {
	s.mu.Lock()
	s.cur = m
	s.mu.Unlock()
}

// A reload that changes the layout and the catalog together shows them together: no frame has
// the new layout in the old catalog, or the old one in the new, and OnReload reads the new
// text. Reconciled in place, the layout is live within the reload; with its root replaced, the
// new root is mounted a drain later. Each path has its own way to split the pair.
func TestAReloadOfLayoutAndCatalogShowsNoFrameMixingThem(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{
		{"reconciled in place", "Vertical", "Vertical"},
		{"its root replaced", "Vertical", "Horizontal"}, // direction is taken at construction
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := func(dir, greeting string, second bool) fstest.MapFS {
				body := `Text { text: qsTrId("demo.greeting") }`
				if second {
					body += "\nText { text: \"second row\" }"
				}
				return fstest.MapFS{
					"main.qml":        {Data: []byte("import tui 1.0\nFlex { direction: Tui." + dir + "\n" + body + "\n}")},
					"i18n/app_en.xml": {Data: tsCatalog("en", "demo.greeting", greeting)},
				}
			}
			fsys := &swapFS{cur: files(tc.before, "greeting v1", false)}
			be := &framesBackend{TestBackend: tui.NewTestBackend(60, 4)}
			var (
				p       *tuidecl.Program
				mu      sync.Mutex
				reloads []string // the greeting as OnReload reads it
			)
			p, err := tuidecl.NewProgram(
				tuidecl.ErrorSink(func(err error) { t.Errorf("handler error: %v", err) }),
				tuidecl.Layout(fsys, "main.qml"),
				tuidecl.Translations(fsys, "i18n", "app"),
				tuidecl.HotReload(tuidecl.ReloadInterval(tick), tuidecl.OnReload(func(decl.Result) {
					text := p.App().Translate(tui.Msg("demo.greeting"))
					mu.Lock()
					reloads = append(reloads, text)
					mu.Unlock()
				})),
				tuidecl.AppOptions(tui.WithBackend(be), tui.WithMinFrameInterval(0)),
			)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- p.Run(ctx) }()
			defer func() {
				cancel()
				if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
					t.Errorf("Run: %v", err)
				}
			}()
			waitScreen := func(what string, ok func(string) bool) {
				t.Helper()
				for deadline := time.Now().Add(decltest.WaitTimeout); !ok(be.String()); {
					if time.Now().After(deadline) {
						t.Fatalf("timed out waiting for %s:\n%s", what, be.String())
					}
					time.Sleep(time.Millisecond)
				}
			}
			waitScreen("the first catalog", func(s string) bool { return strings.Contains(s, "greeting v1") })
			settle() // the poller has taken its snapshot of the first files

			fsys.swap(files(tc.after, "greeting v2", true))
			waitScreen("the reload", func(s string) bool {
				return strings.Contains(s, "greeting v2") && strings.Contains(s, "second row")
			})
			settle()

			for i, f := range be.seen() {
				newLayout, newText := strings.Contains(f, "second row"), strings.Contains(f, "greeting v2")
				if newLayout != newText {
					t.Errorf("frame %d mixes the reload: new layout %v, new catalog %v:\n%s", i, newLayout, newText, f)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(reloads) == 0 {
				t.Fatal("OnReload never ran")
			}
			for _, text := range reloads {
				if text != "greeting v2" {
					t.Errorf("OnReload read %q, want the reloaded catalog's greeting v2", text)
				}
			}
		})
	}
}

// A host publishing a message to properties already on screen — a CheckBox's text, a Dialog's
// title and help line — shows it in the App's language, a CheckBox with no text before it
// too. A buttonless Dialog with no help
// gains the rule over the help line it is given, and a Dialog's plain help, given at
// construction, stays as written.
func TestAHostMessageReachesPropertiesAlreadyMounted(t *testing.T) {
	s := runLanguages(t, `import tui 1.0
import demo 1.0
Window {
 CheckBox { text: App.box }
 Dialog { id: d; title: App.title; helpText: App.help; Text { text: "body" } }
 Dialog { id: p; title: "plain"; helpText: "written help"; Text { text: "body" } }
}`, tuidecl.Types(controls.Types()...), tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.box": "", "App.title": "a title", "App.help": ""}))
	s.WaitForText(t, "[ ]") // a box with no text: a message set now must still relabel it
	onScreenLoop(t, s, func() {
		if err := s.Program.SetMany(map[string]any{
			"App.box": tui.Msg("demo.wrap"), "App.title": tui.Msg("demo.dialog.title"), "App.help": tui.Msg("demo.help"),
		}); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "Wrap")
	openDialog(t, s, "d")
	waitAll(t, s, "the dialog's messages", "┌ Unsaved ", "press a key")
	setLanguage(t, s, "ko_KR")
	waitAll(t, s, "the dialog in Korean, the rule over its help", "┌ 저장 안 됨 ", "├─", "키를 누르세요")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	s.WaitForText(t, "줄 바꿈(w)") // the CheckBox, behind the dialog until now
	openDialog(t, s, "p")
	waitAll(t, s, "the plain dialog", "┌ plain ", "written help")
}
