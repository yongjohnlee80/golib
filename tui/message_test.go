package tui

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui/i18n"
	"github.com/yongjohnlee80/golib/tui/style"
)

// msgLabel is a widget showing a Message the way golib's widgets do: resolved as it is laid
// out, painted as resolved.
type msgLabel struct {
	ctx     *Context
	msg     Message
	text    string
	layouts atomic.Int64
}

func (l *msgLabel) Init(ctx *Context) { l.ctx = ctx }
func (l *msgLabel) Layout(c Constraints) Size {
	l.layouts.Add(1)
	l.text = l.ctx.Translate(l.msg)
	return c.Constrain(Size{W: l.ctx.StringWidth(l.text), H: 1})
}
func (l *msgLabel) Render(s Surface) {
	x := 0
	for g := range Graphemes(l.text) {
		s.SetCell(x, 0, g, style.Style{})
		x += s.StringWidth(g)
	}
}
func (l *msgLabel) HandleEvent(Event) bool { return false }

// firstLine is the screen's top row, as painted.
func firstLine(h *harness) string {
	return strings.TrimRight(strings.SplitN(h.tb.String(), "\n", 2)[0], " ")
}

// shows waits for the top row to read want: a change on the loop is painted on a later frame.
func shows(t *testing.T, h *harness, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if firstLine(h) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("screen = %q, want %q", firstLine(h), want)
}

func TestApp_ShowsAMessageInEnglishByDefault(t *testing.T) {
	t.Parallel()
	l := &msgLabel{msg: Msg("tui.button.yes")}
	h := startApp(t, l, 20, 1)
	shows(t, h, "&Yes") // English is the toolkit's literal
}

func TestApp_SetLanguageRelaysOutInTheNewLanguage(t *testing.T) {
	t.Parallel()
	l := &msgLabel{msg: Msg("tui.button.cancel")}
	h := startApp(t, l, 20, 1)
	shows(t, h, "&Cancel")
	before := l.layouts.Load()
	h.app.SetLanguage("ko_KR")
	shows(t, h, "취소(&c)")
	if l.layouts.Load() == before {
		t.Error("SetLanguage painted without laying out again: text resolved in Layout would be stale")
	}
	var lang string
	h.onLoop(func() { lang = h.app.Language() })
	if lang != "ko_KR" {
		t.Errorf("Language() = %q, want ko_KR", lang)
	}
}

func TestApp_AMessageNoCatalogHasShowsItsID(t *testing.T) {
	t.Parallel()
	l := &msgLabel{msg: Msg("app.nowhere")}
	h := startApp(t, l, 20, 1, WithLanguage("ja_JP"))
	shows(t, h, "app.nowhere") // the id itself
}

func TestApp_AnApplicationCatalogAddsALanguageAndKeepsTheKey(t *testing.T) {
	t.Parallel()
	s := i18n.Toolkit()
	// German, which golib does not ship, translating a toolkit id with the wrong letter.
	if err := s.Add(mustCatalog(t, "de", "tui.button.yes", "&Ja")); err != nil {
		t.Fatal(err)
	}
	l := &msgLabel{msg: Msg("tui.button.yes")}
	h := startApp(t, l, 20, 1, WithTranslations(s), WithLanguage("de_AT"))
	shows(t, h, "Ja(&y)") // de_AT reads de, and Yes stays on y
}

func TestApp_SetTranslationsRelaysOut(t *testing.T) {
	t.Parallel()
	l := &msgLabel{msg: Msg("app.greeting")}
	h := startApp(t, l, 20, 1)
	s := i18n.Toolkit()
	_ = s.Add(mustCatalog(t, "en", "app.greeting", "Hello"))
	h.app.SetTranslations(s)
	shows(t, h, "Hello") // from the replaced catalogs
}

func TestApp_NilTranslationsPanicBeforeAnythingIsQueued(t *testing.T) {
	t.Parallel()
	for name, call := range map[string]func(){
		"WithTranslations":    func() { WithTranslations(nil) },
		"App.SetTranslations": func() { NewApp(&msgLabel{}, WithBackend(NewTestBackend(1, 1))).SetTranslations(nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s(nil) did not panic", name)
				}
			}()
			call()
		}()
	}
}

func TestApp_TranslateIsWhatAWidgetShows(t *testing.T) {
	t.Parallel()
	l := &msgLabel{msg: Msg("tui.button.no")}
	h := startApp(t, l, 20, 1, WithLanguage("pt_BR"))
	shows(t, h, "&Não")
	var got string
	h.onLoop(func() { got = h.app.Translate(Msg("tui.button.no")) })
	if got != "&Não" {
		t.Errorf("App.Translate = %q, want the widget's &Não", got)
	}
	h.onLoop(func() { got = h.app.Translate(Msg("app.nowhere")) })
	if got != "app.nowhere" {
		t.Errorf("App.Translate of an unknown id = %q, want the id", got)
	}
}
