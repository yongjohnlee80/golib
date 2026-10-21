package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// A DataTable's Theme is the App's as it is now, not as the table last painted: right after SetTheme
// takes effect, before a frame is drawn with it, the table already reports the new theme.
func TestADataTableThemeIsTheAppsNotItsLastPaint(t *testing.T) {
	h, _, dt := dataTable(t, numbered(3), 30, 6)
	next := style.NewTheme(style.Default())
	h.app.SetTheme(&next)
	var got *style.Theme
	h.onLoop(func() { got = dt.Theme() }) // queued after the theme change, ahead of its frame
	if got != &next {
		t.Errorf("Theme() right after SetTheme is %p, want the App's new theme %p", got, &next)
	}
}
