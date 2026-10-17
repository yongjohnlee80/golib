package tui

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// fired records which command a stop took, across the loop and the test goroutine.
type fired struct {
	mu  sync.Mutex
	ids []string
}

func (f *fired) add(id string) { f.mu.Lock(); f.ids = append(f.ids, id); f.mu.Unlock() }
func (f *fired) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.ids
	f.ids = nil
	return out
}

// bindingLeaf is a focusable stop acting on its commands.
type bindingLeaf struct {
	*focusProbe
	cmds []keymap.Command
	log  *fired
}

func (b *bindingLeaf) HandleEvent(ev Event) bool {
	if k, ok := ev.(KeyEvent); ok {
		if cmd, ok := b.ctx.KeyCommand(k, b.cmds...); ok {
			b.log.add(cmd.ID)
			return true
		}
	}
	return false
}

func (b *bindingLeaf) KeyBindings(m *keymap.Map) []keymap.Binding { return BindingsOf(m, b.cmds...) }

// bindingBox is an enclosing stop acting on its commands, around one child.
type bindingBox struct {
	*Flex
	cmds []keymap.Command
	log  *fired
	ctx  *Context
}

func (b *bindingBox) Init(ctx *Context) { b.ctx = ctx; b.Flex.Init(ctx) }

func (b *bindingBox) HandleEvent(ev Event) bool {
	if k, ok := ev.(KeyEvent); ok {
		if cmd, ok := b.ctx.KeyCommand(k, b.cmds...); ok {
			b.log.add(cmd.ID)
			return true
		}
	}
	return false
}

func (b *bindingBox) KeyBindings(m *keymap.Map) []keymap.Binding { return BindingsOf(m, b.cmds...) }

func reachOf(bs []keymap.Binding, id string) (keymap.Binding, bool) {
	for _, b := range bs {
		if b.Command.ID == id {
			return b, true
		}
	}
	return keymap.Binding{}, false
}

// The two directional arrangements of ADR 1792255248 §4.8, each advertised event driven through the
// mounted route: a query that suppressed by symmetric overlap would drop the outer Ctrl+S in the
// first, which a plain Ctrl+S still reaches.
func TestKeyBindingsAreDirectionalAndAgreeWithRouting(t *testing.T) {
	for _, c := range []struct {
		name                   string
		inner, outer           string
		innerReach, outerReach []string
	}{
		{"child Ctrl+Shift+S, outer Ctrl+S", "Ctrl+Shift+S", "Ctrl+S", []string{"Ctrl+Shift+S"}, []string{"Ctrl+S"}},
		{"child Ctrl+S, outer Ctrl+Shift+S", "Ctrl+S", "Ctrl+Shift+S", []string{"Ctrl+S", "Ctrl+Shift+S"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			log := &fired{}
			innerCmd := keymap.Command{ID: "inner", Scope: "t.inner", Keys: key.MustParse(c.inner)}
			outerCmd := keymap.Command{ID: "outer", Scope: keymap.Global, Keys: key.MustParse(c.outer)}
			leaf := &bindingLeaf{focusProbe: newFocusProbe("leaf", Size{W: 2, H: 1}), cmds: []keymap.Command{innerCmd}, log: log}
			box := &bindingBox{Flex: NewFlex(Vertical), cmds: []keymap.Command{outerCmd}, log: log}
			box.Add(leaf)
			h := startApp(t, box, 8, 4)
			h.inject(tabEv(false))
			waitFor(t, "leaf focused", func() bool { return focusedID(h) == leaf.nodeID() })

			var bs []keymap.Binding
			var err error
			h.onLoop(func() { bs, err = h.app.KeyBindings(leaf) })
			if err != nil {
				t.Fatal(err)
			}
			in, okIn := reachOf(bs, "inner")
			out, okOut := reachOf(bs, "outer")
			if !okIn || !okOut {
				t.Fatalf("a binding is missing: %+v", bs)
			}
			str := func(ss []key.Seq) []string {
				out := make([]string, len(ss))
				for i, s := range ss {
					out[i] = s.String()
				}
				slices.Sort(out)
				return out
			}
			if got := str(in.Reaches); !slices.Equal(got, c.innerReach) {
				t.Errorf("inner reaches %v, want %v", got, c.innerReach)
			}
			if got := str(out.Reaches); !slices.Equal(got, c.outerReach) {
				t.Errorf("outer reaches %v, want %v", got, c.outerReach)
			}
			if len(c.outerReach) == 0 && !slices.Equal(out.ShadowedBy, []string{"inner"}) {
				t.Errorf("a fully shadowed outer binding names its winner: %v", out.ShadowedBy)
			}
			// Drive each advertised event through the route: the binding the query names takes it.
			for _, b := range []keymap.Binding{in, out} {
				for _, e := range b.Reaches {
					h.inject(KeyEvent{Kind: KeyPress, Code: e.Code, Mods: e.Mods})
					var got []string
					waitFor(t, "the key lands", func() bool { got = append(got, log.take()...); return len(got) > 0 })
					if got[0] != b.Command.ID {
						t.Errorf("%s reached %v; the query said %s", e, got, b.Command.ID)
					}
				}
			}
		})
	}
}

// Under a modal trap the Window's stop is not reached, so a component outside the trap gets
// ErrConfined, and the App's traversal keys are still listed (globalKey runs inside a trap).
func TestKeyBindingsHonourTheTrap(t *testing.T) {
	log := &fired{}
	outsideCmd := keymap.Command{ID: "outside", Scope: "t.out", Keys: key.MustParse("X")}
	outside := &bindingLeaf{focusProbe: newFocusProbe("out", Size{W: 2, H: 1}), cmds: []keymap.Command{outsideCmd}, log: log}
	insideCmd := keymap.Command{ID: "inside", Scope: "t.in", Keys: key.MustParse("Y")}
	inside := &bindingLeaf{focusProbe: newFocusProbe("in", Size{W: 2, H: 1}), cmds: []keymap.Command{insideCmd}, log: log}
	trap := newScopeProbe(true)
	trap.Add(inside)
	globalCmd := keymap.Command{ID: "global", Scope: keymap.Global, Keys: key.MustParse("Z")}
	root := &bindingBox{Flex: NewFlex(Vertical), cmds: []keymap.Command{globalCmd}, log: log}
	root.Add(outside, trap)
	h := startApp(t, root, 8, 6)
	h.onLoop(func() { inside.ctx.RequestFocus() })
	waitFor(t, "inside focused", func() bool { return focusedID(h) == inside.nodeID() })

	var bs []keymap.Binding
	var err, outErr error
	h.onLoop(func() {
		bs, err = h.app.KeyBindings(inside)
		_, outErr = h.app.KeyBindings(outside)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(outErr, ErrConfined) {
		t.Errorf("outside the trap: %v, want ErrConfined", outErr)
	}
	if _, ok := reachOf(bs, "global"); ok {
		t.Error("the Window's binding is listed under a trap that stops it")
	}
	if _, ok := reachOf(bs, FocusNext.ID); !ok {
		t.Error("traversal is missing; globalKey runs inside a trap")
	}
}
