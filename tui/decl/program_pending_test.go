package decl_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// eagerProvider delivers its first value synchronously, as a provider must,
// and two more before Subscribe returns — while the Program is still being
// built, so there is no App to deliver to yet.
type eagerProvider struct{}

func (eagerProvider) Subscribe(fn func(decl.Update)) ([]string, func() error, error) {
	for i, s := range []string{"one", "two", "three"} {
		fn(decl.Update{Version: uint64(i + 1), Values: map[string]qml.SpecValue{
			"Clock.now": {Kind: qml.SpecValueString, Raw: s}}})
	}
	return []string{"Clock.now"}, func() error { return nil }, nil
}

// Work scheduled before the App exists is not lost: it is queued while the
// Program is built and reaches the App when there is one — the screen shows
// the last delivery, which only the queue could have carried. (Deterministic:
// the deliveries happen inside Subscribe, during the build, instead of racing
// it from a provider's goroutine. Their ORDER is not what this shows — the
// engine drops a stale provider version whatever order it arrives in.)
func TestWorkScheduledBeforeTheAppExistsIsNotLost(t *testing.T) {
	s := decltest.Run(t, 20, 2,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport clock 1.0\nText { text: Clock.now }")),
		tuidecl.Singleton("clock", "1.0", "Clock"),
		tuidecl.Providers(eagerProvider{}))
	s.WaitForText(t, "three")
}
