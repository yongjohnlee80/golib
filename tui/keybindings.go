package tui

import (
	"slices"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// KEY BINDINGS BY COMPONENT (ADR 1792255248 §4.8): what a help card asks — which keys reach this
// component, and what does each do? The runtime answers because it owns routing: the focused node,
// its ancestors up to the active trap's ceiling, then the App's own traversal keys. Each stop says
// what it binds through KeyBinder; the answer runs the dispatch decision itself over the finite set of
// event chords those keys accept, so it cannot disagree with what a key press does.

// KeyBinder is the optional capability of a component that acts on keys at its stop: the bindings it
// consults, as m binds them, in the order it consults them (the first holding a key wins it).
type KeyBinder interface {
	KeyBindings(m *keymap.Map) []keymap.Binding
}

// ErrConfined is KeyBindings' answer for a component outside the active modal trap: no key reaches
// it while the trap stands.
var ErrConfined = errs.Sentinel(errs.ErrPrecondition, "tui: the component is outside the active modal trap")

// KeyBindings is the bindings that would reach comp if it had focus now, in routing order, each with
// its Scope, Source, Reaches (the event chords it wins) and ShadowedBy (the commands that win the rest
// of what its keys accept, earlier in routing). Every binding is listed, a fully shadowed one with no
// Reaches. It does not predict text a component inserts, the key after a pending grammar's prefix, or
// a handler that declines a command conditionally (a List's context menu when it has none).
//
// A component outside the active trap gets ErrConfined; one not mounted, errs.ErrPrecondition. Loop
// goroutine only.
func (a *App) KeyBindings(comp Component) ([]keymap.Binding, error) {
	n := a.byComp[comp]
	if n == nil || !n.mounted {
		return nil, errs.Wrap(errs.ErrPrecondition, "tui: KeyBindings: the component is not mounted")
	}
	limit := a.confinement()
	if limit != nil && !withinScope(n, limit) {
		return nil, ErrConfined
	}
	m := a.cfg.keymap
	var stops [][]keymap.Binding
	for ; n != nil; n = n.parent {
		if kb, ok := n.comp.(KeyBinder); ok {
			if bs := kb.KeyBindings(m); len(bs) > 0 {
				stops = append(stops, bs)
			}
		}
		if n == limit {
			break
		}
	}
	stops = append(stops, BindingsOf(m, FocusNext, FocusPrev)) // globalKey runs inside a trap too
	return reach(stops), nil
}

// BindingsOf is cmds as m binds them, for a KeyBinder whose stop consults exactly these commands.
func BindingsOf(m *keymap.Map, cmds ...keymap.Command) []keymap.Binding {
	out := make([]keymap.Binding, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, keymap.Binding{Command: c, Keys: m.KeysOf(c), Scope: c.Scope, Source: m.SourceOf(c.ID)})
	}
	return out
}

// reach fills Reaches and ShadowedBy by the dispatch decision: for each event chord any binding
// accepts, the first stop holding one of its candidates wins it, the earlier candidate first, the
// earlier binding first within a stop.
func reach(stops [][]keymap.Binding) []keymap.Binding {
	type at struct{ stop, i int }
	var events []key.Seq
	for _, stop := range stops {
		for _, b := range stop {
			for _, k := range b.Keys {
				for _, e := range key.Accepts(k) {
					if !slices.Contains(events, e) {
						events = append(events, e)
					}
				}
			}
		}
	}
	won := map[at][]key.Seq{}
	winner := map[key.Seq]at{}
	for _, e := range events {
	find:
		for si, stop := range stops {
			for _, cand := range key.Candidates(e) {
				for bi, b := range stop {
					if slices.Contains(b.Keys, cand) {
						w := at{si, bi}
						won[w] = append(won[w], e)
						winner[e] = w
						break find
					}
				}
			}
		}
	}
	var out []keymap.Binding
	for si, stop := range stops {
		for bi, b := range stop {
			b.Reaches = won[at{si, bi}]
			for _, k := range b.Keys {
				for _, e := range key.Accepts(k) {
					w, ok := winner[e]
					if !ok || w == (at{si, bi}) {
						continue
					}
					if id := stops[w.stop][w.i].Command.ID; !slices.Contains(b.ShadowedBy, id) {
						b.ShadowedBy = append(b.ShadowedBy, id)
					}
				}
			}
			out = append(out, b)
		}
	}
	return out
}

// KeyScopeOf is the key-map scope keys reaching comp are read in: its own if it is KeyScoped, else the
// nearest enclosing KeyScoped component's; "" when none has one. Live: an Editor's follows its mode.
// Loop goroutine only.
func (a *App) KeyScopeOf(comp Component) keymap.Scope {
	for n := a.byComp[comp]; n != nil; n = n.parent {
		if ks, ok := n.comp.(KeyScoped); ok {
			if s := ks.KeyScope(); s != "" {
				return s
			}
		}
	}
	return ""
}
