package decl_test

import (
	"errors"
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// RELOAD WITH SOURCES — a host adding a part at runtime reloads a document that reads sources it
// never declared, declaring them as it reloads. A refused reload declares none; a remount after a
// reload that failed part-way declares them again, with the values they have.
//
// The Flex stacks its texts, so each is waited for on its own line.

func sourcesDoc(extra string) string {
	return windowDoc(" Flex { Text { id: old; text: App.old }" + extra + " }")
}

var withFresh = sourcesDoc(` Text { text: " | " } Text { id: fresh; text: App.fresh }`)

func runSources(t *testing.T) *decltest.Screen {
	t.Helper()
	s := decltest.Run(t, 40, 8, tuidecl.LayoutSource("main.qml", []byte(sourcesDoc(""))),
		tuidecl.Singleton("demo", "1.0", "App"), tuidecl.Sources(map[string]any{"App.old": "before"}))
	s.WaitForText(t, "before")
	return s
}

// Reload refuses a document reading a source never declared; ReloadWithSources declares and binds
// it, and both sources move afterwards.
func TestAReloadDeclaresTheSourcesItReads(t *testing.T) {
	s := runSources(t)
	var err error
	onScreenLoop(t, s, func() { _, err = s.Program.Reload([]byte(withFresh)) })
	if err == nil || !strings.Contains(err.Error(), `has no member "fresh"`) {
		t.Fatalf("a plain reload of an undeclared source: %v", err)
	}
	onScreenLoop(t, s, func() {
		_, err = s.Program.ReloadWithSources([]byte(withFresh), map[string]any{"App.fresh": "newcomer"})
	})
	if err != nil {
		t.Fatalf("ReloadWithSources: %v", err)
	}
	s.WaitForText(t, "newcomer")
	s.WaitForText(t, "before")
	onScreenLoop(t, s, func() { err = errors.Join(s.Program.Set("App.fresh", "moved"), s.Program.Set("App.old", "after")) })
	if err != nil {
		t.Fatalf("the sources after the reload: %v", err)
	}
	s.WaitForText(t, "moved")
	s.WaitForText(t, "after")
}

// A refused ReloadWithSources (its document reads one more source, never declared) declares none of
// its sources and leaves the screen; a name already declared is refused.
func TestARefusedReloadDeclaresNoSource(t *testing.T) {
	s := runSources(t)
	missing := sourcesDoc(` Text { id: third; text: App.third } Text { id: miss; text: App.missing }`)
	var err error
	onScreenLoop(t, s, func() { _, err = s.Program.ReloadWithSources([]byte(missing), map[string]any{"App.third": "x"}) })
	if err == nil {
		t.Fatal("a reload reading a source still undeclared was accepted")
	}
	onScreenLoop(t, s, func() { err = s.Program.Set("App.third", "y") })
	if err == nil {
		t.Fatal("a refused reload left App.third declared")
	}
	if sc := s.String(); !strings.Contains(sc, "before") || strings.Contains(sc, " x") {
		t.Fatalf("the refused reload changed the screen:\n%s", sc)
	}
	onScreenLoop(t, s, func() {
		_, err = s.Program.ReloadWithSources([]byte(withFresh), map[string]any{"App.old": "again", "App.fresh": "f"})
	})
	if err == nil {
		t.Fatal("a source declared twice was accepted")
	}
	onScreenLoop(t, s, func() { err = s.Program.Set("App.fresh", "y") })
	if err == nil {
		t.Fatal("a refused duplicate left App.fresh declared")
	}
}

// A reload that fails part-way (a colour refused while applying) latches the tree. ReloadWithSources
// refuses a failed tree, and the next Reload remounts it: a source ReloadWithSources declared is
// declared again, with its current value.
func TestARemountDeclaresTheReloadsSourcesAgain(t *testing.T) {
	s := runSources(t)
	// the colour is changed on a node that exists, so it is refused while applying, part-way
	tinted := func(colour string) []byte {
		return []byte(sourcesDoc(` Text { text: " | " } Text { id: fresh; text: App.fresh } Text { id: tint; text: "x"; palette.window: "` + colour + `" }`))
	}
	var err error
	onScreenLoop(t, s, func() {
		_, err = s.Program.ReloadWithSources(tinted("blue"), map[string]any{"App.fresh": "newcomer"})
		if err == nil {
			err = s.Program.Set("App.fresh", "moved")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	s.WaitForText(t, "moved")
	onScreenLoop(t, s, func() { _, err = s.Program.Reload(tinted("bleu")) })
	if err == nil {
		t.Fatal("the bad colour was accepted")
	}
	failed := false
	onScreenLoop(t, s, func() { failed = s.Program.Tree().Failed() })
	if !failed {
		t.Fatal("the colour was refused before the tree was touched; this cell needs a part-way failure")
	}
	good := sourcesDoc(` Text { text: " | " } Text { id: fresh; text: App.fresh } Text { text: " ok" }`)
	onScreenLoop(t, s, func() { _, err = s.Program.ReloadWithSources([]byte(good), map[string]any{"App.later": "l"}) })
	if err == nil || !strings.Contains(err.Error(), "a failed tree is remounted by Reload") {
		t.Fatalf("ReloadWithSources on a failed tree: %v, want it refused for Reload's remount", err)
	}
	onScreenLoop(t, s, func() { _, err = s.Program.Reload([]byte(good)) })
	if err != nil {
		t.Fatalf("the remount: %v", err)
	}
	s.WaitForText(t, " ok")
	s.WaitForText(t, "moved")
	onScreenLoop(t, s, func() { failed = s.Program.Tree().Failed() })
	if failed {
		t.Fatal("the remount left the tree failed")
	}
}

// ReloadWithSources refuses what it cannot read, an incomplete document or a value no source can
// hold, before it declares anything; a program built with no sources takes them too.
func TestAReloadWithSourcesRefusesWhatItCannotRead(t *testing.T) {
	s := runSources(t)
	var err error
	onScreenLoop(t, s, func() { _, err = s.Program.ReloadWithSources([]byte("Window {"), map[string]any{"App.fresh": "f"}) })
	if err == nil {
		t.Fatal("an incomplete document was reloaded")
	}
	onScreenLoop(t, s, func() {
		_, err = s.Program.ReloadWithSources([]byte(withFresh), map[string]any{"App.fresh": struct{}{}})
	})
	if err == nil || !strings.Contains(err.Error(), "App.fresh") {
		t.Fatalf("a value no source can hold: %v, want it refused, naming the source", err)
	}
	onScreenLoop(t, s, func() { err = s.Program.Set("App.fresh", "y") })
	if err == nil {
		t.Fatal("a refused reload left App.fresh declared")
	}

	bare := decltest.Run(t, 40, 8, tuidecl.LayoutSource("main.qml", []byte(windowDoc(" Flex { Text { text: \"plain\" } }"))),
		tuidecl.Singleton("demo", "1.0", "App"))
	bare.WaitForText(t, "plain")
	onScreenLoop(t, bare, func() {
		_, err = bare.Program.ReloadWithSources([]byte(windowDoc(" Flex { Text { text: App.fresh } }")), map[string]any{"App.fresh": "declared late"})
	})
	if err != nil {
		t.Fatalf("a program with no sources: %v", err)
	}
	bare.WaitForText(t, "declared late")
}

// A ReloadWithSources whose reconcile fails part-way (a colour refused while applying, on a node that
// exists) latches the tree, as Reload's does, and declares none of its sources: App.fresh is not in
// the registry, setting an existing source is safe, and the next Reload remounts without it.
func TestAReloadWithSourcesFailingPartWayDeclaresNone(t *testing.T) {
	s := runSources(t)
	tint := func(colour, extra string) []byte {
		return []byte(sourcesDoc(extra + ` Text { id: tint; text: "x"; palette.window: "` + colour + `" }`))
	}
	var err error
	onScreenLoop(t, s, func() { _, err = s.Program.Reload(tint("blue", "")) })
	if err != nil {
		t.Fatal(err)
	}
	onScreenLoop(t, s, func() {
		_, err = s.Program.ReloadWithSources(tint("bleu", ` Text { id: fresh; text: App.fresh }`), map[string]any{"App.fresh": "newcomer"})
	})
	if err == nil {
		t.Fatal("the bad colour was accepted")
	}
	failed, declared := false, true
	onScreenLoop(t, s, func() {
		failed = s.Program.Tree().Failed()
		_, declared = s.Program.Tree().Lookup("App.fresh")
	})
	if !failed {
		t.Fatal("the colour was refused before the tree was touched; this cell needs a part-way failure")
	}
	if declared {
		t.Fatal("a reload that failed part-way left App.fresh declared")
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.fresh", "y"); err == nil {
			t.Error("App.fresh could be set after the failed reload")
		}
		_ = s.Program.Set("App.old", "after") // refused or taken, but safe on a latched tree
	})
	onScreenLoop(t, s, func() { _, err = s.Program.Reload(tint("blue", ` Text { text: " ok" }`)) })
	if err != nil {
		t.Fatalf("the remount: %v", err)
	}
	s.WaitForText(t, " ok")
	onScreenLoop(t, s, func() {
		failed = s.Program.Tree().Failed()
		_, declared = s.Program.Tree().Lookup("App.fresh")
	})
	if failed || declared {
		t.Fatalf("after the remount: failed %v, App.fresh declared %v (want both false)", failed, declared)
	}
}
