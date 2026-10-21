package tui

import (
	"testing"
	"time"
)

// A key handler that changes the language, the catalogs or the key map with an Apply call is
// answered with a frame: the change dirties layout, and the dirtying wakes the loop, though the key
// came in on the input lane, which renders nothing by itself. The test watches the probe's layout
// count without posting to the loop, since a posted Update would wake it and hide the defect.
func TestAnApplyFromAKeyHandlerLaysOutAgain(t *testing.T) {
	for name, apply := range map[string]func(*App){
		"ApplyLanguage":   func(a *App) { a.ApplyLanguage("ko_KR") },
		"ApplyTranslator": func(a *App) { a.ApplyTranslator(&fakeCatalog{}) },
		"ApplyKeymap":     func(a *App) { a.ApplyKeymap(nil) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := &probe{name: "root", pref: Size{W: 4, H: 2}}
			root.onEvent = func(p *probe, ev Event) bool {
				if k, ok := ev.(KeyEvent); ok && k.Code == 'a' {
					apply(p.ctx.App())
					return true
				}
				return false
			}
			h := startApp(t, root, 4, 2)
			waitFor(t, "the first frame", func() bool { return root.layouts.Load() > 0 })
			h.sync()
			before := root.layouts.Load()
			h.inject(keyEv('a'))
			deadline := time.Now().Add(time.Second)
			for root.layouts.Load() == before {
				if time.Now().After(deadline) {
					t.Fatalf("no layout pass within 1s of %s from a key handler: the loop was never woken", name)
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}
