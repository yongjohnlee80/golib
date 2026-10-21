package widget

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// scrimNativeBackend draws the scrims natively: a GUI backend that dims what is beneath itself.
type scrimNativeBackend struct{ *tui.TestBackend }

func (scrimNativeBackend) PlaceNatives([]tui.NativePlacement) {}

func (scrimNativeBackend) NativeFor(c tui.Component) (any, tui.NativeScope, bool) {
	switch c.(type) {
	case *floatLayer, *Scrim:
		return "scrim", tui.ScopeChrome, true
	}
	return nil, 0, false
}

// screenText is the backend's screen, a line per row.
func screenText(tb *tui.TestBackend) string {
	var b strings.Builder
	for _, row := range tb.Snapshot() {
		for _, c := range row {
			b.WriteString(c.Content)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// A dimmed Float's scrim is the backend's to draw where it draws scrims natively, as a Modal's is:
// cells over it would hide the native content beneath (tui-widget-shell-D5).
func TestADimmedFloatLeavesTheScrimToANativeBackend(t *testing.T) {
	dialog := NewFloat(NewText("dialog"), WithModal(true), WithDimBackground(true))
	host := NewOverlayHost(NewText("base"))
	host.Attach(dialog)
	tb := tui.NewTestBackend(20, 5)
	app := tui.NewApp(host, tui.WithBackend(scrimNativeBackend{tb}), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	defer func() { cancel(); <-done }()
	app.Update(dialog.Show)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(screenText(tb), "dialog") {
		if time.Now().After(deadline) {
			t.Fatalf("the dialog never showed:\n%s", screenText(tb))
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s := screenText(tb); strings.Contains(s, "░") {
		t.Errorf("the scrim was painted in cells over a native backend:\n%s", s)
	}
}
