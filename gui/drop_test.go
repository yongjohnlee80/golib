package gui

import (
	"testing"

	"github.com/yongjohnlee80/gio/app"
	"github.com/yongjohnlee80/golib/tui"
)

func TestDesktopDropDecodesAndQuotesPathsAsOnePaste(t *testing.T) {
	b := &Backend{q: newEventQueue()}
	b.desktopDrop(app.DropEvent{Type: "text/uri-list", Data: []byte("# files\r\nfile:///tmp/one.png\r\nfile://localhost/tmp/it%27s%20two.png\r\nfile:///tmp/%24%28touch%20x%29.png\r\n")})
	if len(b.q.queue) != 1 {
		t.Fatalf("events: %v", b.q.queue)
	}
	paste, ok := b.q.queue[0].(tui.PasteEvent)
	want := "/tmp/one.png '/tmp/it'\\''s two.png' '/tmp/$(touch x).png'"
	if !ok || paste.Text != want {
		t.Fatalf("event %v, want paste %q", b.q.queue[0], want)
	}
}

func TestDesktopDropRefusesAnEntireInvalidURIList(t *testing.T) {
	for _, invalid := range []string{
		"https://example.com/image.png", "file://remote/tmp/a.png", "file:///tmp/%00.png",
		"file:///tmp/%ff.png", "file:///tmp/a?query", "file:///tmp/a#fragment", "file:relative",
		"file:///tmp/%ZZ.png", "file://user@localhost/tmp/a.png",
		"file:///tmp/%1B.png", "file:///tmp/%0A.png", "file:///tmp/%07.png",
		"file:///tmp/%09.png", "file:///tmp/%0D.png", "file:///tmp/%1F.png",
		"file:///tmp/%7F.png", "file:///tmp/%C2%80.png", "file:///tmp/%C2%9F.png",
		"file:///tmp/a%1B%5B20%1B%5B201~1~%3B%20text",
	} {
		b := &Backend{q: newEventQueue()}
		b.desktopDrop(app.DropEvent{Type: "text/uri-list", Data: []byte("file:///tmp/valid.png\n" + invalid)})
		if len(b.q.queue) != 0 {
			t.Errorf("%q pasted a partial or invalid list: %v", invalid, b.q.queue)
		}
	}
}

func TestDesktopDropPlainTextAndUnsupportedTypes(t *testing.T) {
	for _, c := range []struct {
		kind, text, want string
		ok               bool
	}{
		{"text/plain;charset=utf-8", "a\r\nb\rc", "a\nb\nc", true},
		{"text/plain", "not a file:// URI", "not a file:// URI", true},
		{"image/png", "bytes", "", false},
		{"text/plain;charset=latin1", "text", "", false},
		{"text/plain", "a\x00b", "", false},
		{"text/uri-list", "# no paths\n", "", false},
	} {
		if got, ok := droppedText(c.kind, []byte(c.text)); got != c.want || ok != c.ok {
			t.Errorf("%q %q: %q %v, want %q %v", c.kind, c.text, got, ok, c.want, c.ok)
		}
	}
}
