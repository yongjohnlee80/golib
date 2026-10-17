package keymap

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui/key"
)

func parse(t *testing.T, xml string) *Scheme {
	t.Helper()
	s, err := ParseScheme(strings.NewReader(xml), "keymap.xml")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var (
	down    = Command{ID: "tui.list.down", Scope: "tui.list", Keys: key.MustParse("Down", "J")}
	up      = Command{ID: "tui.list.up", Scope: "tui.list", Keys: key.MustParse("Up", "K")}
	top     = Command{ID: "tui.list.top", Scope: "tui.list", Keys: key.MustParse("G")}
	save    = Command{ID: "app.save", Scope: Global, Keys: key.MustParse("Ctrl+S")}
	next    = Command{ID: "tui.app.focusNext", Scope: App, Keys: key.MustParse("Tab")}
	listAll = []Command{down, up, top, save, next}
)

func TestASchemeChangesOnlyTheKeysItNames(t *testing.T) {
	m, err := Build(listAll, parse(t, `<!DOCTYPE KeyboardMappingScheme>
<mapping>
  <shortcut id="tui.list.down"><key value="Down"/><key value="Ctrl+N"/></shortcut>
  <shortcut id="tui.list.top"><key value=""/></shortcut>
</mapping>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.KeysOf(down); !slices.Equal(got, key.MustParse("Down", "Ctrl+N")) {
		t.Errorf("down: %v", got)
	}
	if got := m.KeysOf(top); len(got) != 0 {
		t.Errorf("an unbound command keeps keys: %v", got)
	}
	if got := m.KeysOf(up); !slices.Equal(got, up.Keys) {
		t.Errorf("an unnamed command lost its defaults: %v", got)
	}
	if src := m.SourceOf(down.ID); src.Kind != FromScheme || src.File != "keymap.xml" || src.Line != 3 {
		t.Errorf("down's source: %+v", src)
	}
	if src := m.SourceOf(up.ID); src.Kind != FromDefault {
		t.Errorf("up's source: %+v", src)
	}
}

func TestALaterSchemeWinsPerCommand(t *testing.T) {
	product := parse(t, `<mapping><shortcut id="tui.list.down"><key value="Ctrl+N"/></shortcut>
<shortcut id="tui.list.up"><key value="Ctrl+P"/></shortcut></mapping>`)
	user := parse(t, `<mapping><shortcut id="tui.list.down"><key value="N"/></shortcut></mapping>`)
	m, err := Build(listAll, product, user)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.KeysOf(down); !slices.Equal(got, key.MustParse("N")) {
		t.Errorf("the user's layer lost: %v", got)
	}
	if got := m.KeysOf(up); !slices.Equal(got, key.MustParse("Ctrl+P")) {
		t.Errorf("the product's layer for an id the user left was lost: %v", got)
	}
}

func TestANilMapOrAnUndeclaredCommandKeepsItsDefaults(t *testing.T) {
	var m *Map
	if got := m.KeysOf(down); !slices.Equal(got, down.Keys) {
		t.Errorf("nil map: %v", got)
	}
	built, err := Build([]Command{up})
	if err != nil {
		t.Fatal(err)
	}
	if got := built.KeysOf(down); !slices.Equal(got, down.Keys) {
		t.Errorf("undeclared: %v", got)
	}
	if !built.Has(down, key.MustParse("J")[0]) || m.Has(top, key.MustParse("J")[0]) {
		t.Error("Has disagrees with KeysOf")
	}
}

func TestBuildRefusesEveryProblemAtOnce(t *testing.T) {
	_, err := Build(append(slices.Clone(listAll), down), parse(t, `<mapping>
<shortcut id="tui.list.nothing"><key value="X"/></shortcut>
<shortcut id="tui.list.up"><key value="J"/></shortcut>
</mapping>`))
	var e *Error
	if !errors.As(err, &e) || !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("Build: %v, want *Error wrapping ErrInvalidArgument", err)
	}
	kinds := map[ProblemKind]Problem{}
	for _, p := range e.Problems {
		kinds[p.Kind] = p
	}
	if _, ok := kinds[Duplicate]; !ok {
		t.Error("no Duplicate for tui.list.down declared twice")
	}
	if p, ok := kinds[UnknownID]; !ok || p.ID != "tui.list.nothing" || p.Line != 2 {
		t.Errorf("UnknownID: %+v", p)
	}
	if p, ok := kinds[Conflict]; !ok || p.Line != 3 || p.Key != key.MustParse("J")[0] {
		t.Errorf("Conflict: %+v", p)
	}
	if !strings.Contains(err.Error(), "keymap.xml, line 3") {
		t.Errorf("the message names no position: %v", err)
	}
}

func TestAnExactAndAFallbackKeyInOneScopeAreNotAConflict(t *testing.T) {
	a := Command{ID: "a", Scope: "s", Keys: key.MustParse("Ctrl+S")}
	b := Command{ID: "b", Scope: "s", Keys: key.MustParse("Ctrl+Shift+S")}
	if _, err := Build([]Command{a, b}); err != nil {
		t.Fatalf("Ctrl+S and Ctrl+Shift+S: %v", err)
	}
	c := Command{ID: "c", Scope: "s", Keys: key.MustParse("Ctrl+S")}
	if _, err := Build([]Command{a, c}); err == nil {
		t.Fatal("two Ctrl+S in one scope were accepted")
	}
	d := Command{ID: "d", Scope: "other", Keys: key.MustParse("Ctrl+S")}
	if _, err := Build([]Command{a, d}); err != nil {
		t.Fatalf("one key in two inner scopes: %v", err)
	}
}

func TestAKeySharedWithAnOuterScopeIsAWarning(t *testing.T) {
	child := Command{ID: "child", Scope: "tui.list", Keys: key.MustParse("Ctrl+S")}
	outer := Command{ID: "outer", Scope: Global, Keys: key.MustParse("Ctrl+Shift+S")}
	tab := Command{ID: "tab", Scope: "tui.list", Keys: key.MustParse("Tab")}
	m, err := Build([]Command{child, outer, tab, next})
	if err != nil {
		t.Fatal(err)
	}
	w := m.Warnings()
	if len(w) != 2 {
		t.Fatalf("warnings: %v", w)
	}
	for _, p := range w {
		if p.Kind != Shadow || !p.Warning() {
			t.Errorf("%+v is not a Shadow warning", p)
		}
	}
	w[0].ID = "mutated"
	if m.Warnings()[0].ID == "mutated" {
		t.Error("Warnings returned the map's own slice")
	}
}

func TestGettersReturnCopies(t *testing.T) {
	m, err := Build(listAll)
	if err != nil {
		t.Fatal(err)
	}
	m.KeysOf(down)[0] = key.Seq{Code: 'x'}
	b := m.Bindings("tui.list")
	b[0].Keys[0] = key.Seq{Code: 'x'}
	c, _ := m.Command(down.ID)
	c.Keys[0] = key.Seq{Code: 'x'}
	m.Commands()[0].Keys[0] = key.Seq{Code: 'x'}
	if got := m.KeysOf(down); !slices.Equal(got, down.Keys) {
		t.Errorf("a getter's slice changed the map: %v", got)
	}
	listAll[0].Keys[0] = key.Seq{Code: 'y'}
	defer func() { listAll[0].Keys[0] = key.MustParse("Down")[0] }()
	if got := m.KeysOf(Command{ID: down.ID}); got[0] != key.MustParse("Down")[0] {
		t.Errorf("Build kept the caller's slice: %v", got)
	}
}

func TestBindingsAndScopes(t *testing.T) {
	m, err := Build(listAll)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, b := range m.Bindings("tui.list") {
		ids = append(ids, b.Command.ID)
		if b.Scope != "tui.list" {
			t.Errorf("%s: scope %s", b.Command.ID, b.Scope)
		}
	}
	if !slices.Equal(ids, []string{"tui.list.down", "tui.list.top", "tui.list.up"}) {
		t.Errorf("bindings in key order: %v", ids)
	}
	if got := m.Scopes(); !slices.Equal(got, []Scope{Global, "tui.app", "tui.list"}) {
		t.Errorf("scopes: %v", got)
	}
}

func TestParseSchemeRefuses(t *testing.T) {
	for _, c := range []struct {
		name, xml, want string
	}{
		{"empty file", ``, "no <mapping>"},
		{"not xml", `<mapping>`, "not well-formed"},
		{"wrong root", `<keymap/>`, "<keymap> where <mapping>"},
		{"two roots", `<mapping/><mapping/>`, "<mapping> where <mapping> is the root"},
		{"unknown element", `<mapping><shortcuts id="a"/></mapping>`, "only <shortcut> belongs"},
		{"unknown key element", `<mapping><shortcut id="a"><keys value="X"/></shortcut></mapping>`, "only <key> belongs"},
		{"misspelt id", `<mapping><shortcut name="a"><key value="X"/></shortcut></mapping>`, `attribute "name"`},
		{"misspelt value", "<mapping>\n<shortcut id=\"a\"><key valu=\"X\"/></shortcut></mapping>", `line 2: <key> has an attribute "valu"`},
		{"no id", `<mapping><shortcut><key value="X"/></shortcut></mapping>`, "has no id"},
		{"no value", `<mapping><shortcut id="a"><key/></shortcut></mapping>`, "has no value"},
		{"no key", `<mapping><shortcut id="a"></shortcut></mapping>`, "has no <key>"},
		{"twice", "<mapping>\n<shortcut id=\"a\"><key value=\"X\"/></shortcut>\n<shortcut id=\"a\"><key value=\"Y\"/></shortcut></mapping>", "line 3: shortcut \"a\" is written a second time (first at line 2)"},
		{"bind and unbind", `<mapping><shortcut id="a"><key value=""/><key value="X"/></shortcut></mapping>`, "both unbinds"},
		{"bad key", `<mapping><shortcut id="a"><key value="Ctrl+Foo"/></shortcut></mapping>`, "Foo"},
		{"text", `<mapping>hello</mapping>`, "text in <mapping>"},
		{"inside key", `<mapping><shortcut id="a"><key value="X">x</key></shortcut></mapping>`, "holds nothing"},
		{"root attribute", `<mapping version="1"/>`, `attribute "version"`},
	} {
		_, err := ParseScheme(strings.NewReader(c.xml), "keymap.xml")
		if err == nil {
			t.Errorf("%s: accepted", c.name)
			continue
		}
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("%s: %v is not ErrInvalidArgument", c.name, err)
		}
		if !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "keymap.xml") {
			t.Errorf("%s: %v, want it to name the file and say %q", c.name, err, c.want)
		}
	}
	_, err := ParseScheme(strings.NewReader(`<mapping><shortcut id="a"><key value="Ctrl+K, Ctrl+C"/></shortcut></mapping>`), "keymap.xml")
	if !errors.Is(err, key.ErrMultiChord) {
		t.Errorf("a multi-chord key: %v, want ErrMultiChord", err)
	}
}

func TestParseSchemeAcceptsQtCreatorsShape(t *testing.T) {
	s := parse(t, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE KeyboardMappingScheme>
<!-- a comment -->
<mapping>
 <shortcut id="QtCreator.New">
  <key value="Ctrl+N"/>
 </shortcut>
 <shortcut id="QtCreator.Unbound">
  <key value=""/>
 </shortcut>
</mapping>`)
	if got := s.IDs(); !slices.Equal(got, []string{"QtCreator.New", "QtCreator.Unbound"}) {
		t.Errorf("ids: %v", got)
	}
	if s.entries[0].line != 5 || !s.entries[1].unbind {
		t.Errorf("entries: %+v", s.entries)
	}
}
