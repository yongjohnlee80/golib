package decl_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// repeater_test.go: the engine's side of a Repeater — following its model.

// nameModel is a model of names, counting its subscribers.
type nameModel struct {
	names []string
	subs  map[int]func(decl.Change)
	next  int
}

func (m *nameModel) RowCount(*decl.Index) int { return len(m.names) }
func (m *nameModel) Data(ix decl.Index, _ string) qml.SpecValue {
	return qml.SpecValue{Kind: qml.SpecValueString, Raw: m.names[ix.Row]}
}
func (m *nameModel) Key(ix decl.Index) string { return m.names[ix.Row] }
func (m *nameModel) Subscribe(fn func(decl.Change)) func() {
	if m.subs == nil {
		m.subs = map[int]func(decl.Change){}
	}
	id := m.next
	m.next++
	m.subs[id] = fn
	return func() { delete(m.subs, id) }
}
func (m *nameModel) add(name string) {
	m.names = append(m.names, name)
	for _, fn := range m.subs {
		fn(decl.Change{Kind: decl.Inserted, First: len(m.names) - 1, Last: len(m.names) - 1})
	}
}

// A Destroy refused mid-emission changes nothing: the tree still follows its
// model, and a row added afterwards is still built.
func TestARefusedDestroyKeepsFollowingTheModel(t *testing.T) {
	r := newRecorder()
	tr := decl.New(r)
	m := &nameModel{names: []string{"ann"}}
	if err := tr.DeclareSource("rows", qml.SpecValue{Kind: qml.SpecValueObject, Obj: m}); err != nil {
		t.Fatal(err)
	}
	var refused error
	r.handlers["kill"] = func() error { refused = tr.Destroy(); return nil }
	if err := tr.Mount(wiredSpec(t, tr, r, `B { onGo: kill()
		Repeater { model: rows; A { label: model.name } } }`)); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if err := tr.Emit(tr.Root(), "go"); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !errors.Is(refused, decl.ErrPhase) {
		t.Fatalf("Destroy during emission = %v, want ErrPhase", refused)
	}
	if len(m.subs) != 1 {
		t.Fatalf("after the refused Destroy the model has %d subscribers, want 1", len(m.subs))
	}
	m.add("bob")
	if trace := strings.Join(r.trace, "\n"); !strings.Contains(trace, "label=string(bob)") {
		t.Errorf("the row added after the refused Destroy was not built:\n%s", trace)
	}
}
