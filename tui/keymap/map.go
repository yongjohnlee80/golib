package keymap

import (
	"slices"
	"sort"

	"github.com/yongjohnlee80/golib/tui/key"
)

// Map is the commands with the schemes layered on them. It is immutable once built: every getter
// returns a copy, so the App swaps a Map whole and nothing a caller holds can change an installed one.
type Map struct {
	commands map[string]Command
	order    []string                     // command ids in declaration order
	keys     map[string][]key.Seq         // effective keys by id
	source   map[string]Source            // where each id's keys came from
	scopes   map[Scope]map[key.Seq]string // effective key → id, per scope
	warnings []Problem
}

// Build declares commands and layers schemes on them, in order: a later scheme's entry replaces an
// earlier one's keys for that id, and an id no scheme names keeps its defaults.
//
// It refuses with *Error, every Problem at once: an id declared twice; a scheme id no command declares;
// two commands of one scope with an equal key once layered. Warnings, kept on the Map: an inner key
// that overlaps (key.Overlaps) a Global or App key, which the inner stop wins while focus is there.
//
// Two keys of one scope that only overlap (Ctrl+S and Ctrl+Shift+S) are not a conflict: lookup tries
// the exact chord first, so each event has one winner.
func Build(commands []Command, schemes ...*Scheme) (*Map, error) {
	m := &Map{
		commands: make(map[string]Command, len(commands)),
		keys:     make(map[string][]key.Seq, len(commands)),
		source:   make(map[string]Source, len(commands)),
		scopes:   map[Scope]map[key.Seq]string{},
	}
	var problems []Problem
	for _, c := range commands {
		if _, dup := m.commands[c.ID]; dup {
			problems = append(problems, Problem{ID: c.ID, Kind: Duplicate})
			continue
		}
		c.Keys = slices.Clone(c.Keys)
		m.commands[c.ID] = c
		m.order = append(m.order, c.ID)
		m.keys[c.ID] = c.Keys
		m.source[c.ID] = Source{Kind: FromDefault}
	}
	for _, s := range schemes {
		for _, e := range s.entries {
			if _, ok := m.commands[e.id]; !ok {
				problems = append(problems, Problem{File: s.file, Line: e.line, ID: e.id, Kind: UnknownID})
				continue
			}
			m.keys[e.id] = slices.Clone(e.keys)
			m.source[e.id] = Source{Kind: FromScheme, File: s.file, Line: e.line}
		}
	}
	for _, id := range m.order {
		c := m.commands[id]
		byKey := m.scopes[c.Scope]
		if byKey == nil {
			byKey = map[key.Seq]string{}
			m.scopes[c.Scope] = byKey
		}
		for _, k := range m.keys[id] {
			if other, taken := byKey[k]; taken && other != id {
				src := m.source[id]
				if src.Kind != FromScheme {
					src = m.source[other]
				}
				problems = append(problems, Problem{File: src.File, Line: src.Line, ID: other, With: id, Kind: Conflict, Key: k})
				continue
			}
			byKey[k] = id
		}
	}
	if len(problems) > 0 {
		return nil, &Error{Problems: problems}
	}
	m.warnings = m.shadows()
	return m, nil
}

// shadows is every inner key that overlaps a Global or App key.
func (m *Map) shadows() []Problem {
	var out []Problem
	for _, inner := range m.order {
		ic := m.commands[inner]
		if ic.Scope.outer() {
			continue
		}
		for _, k := range m.keys[inner] {
			for _, outer := range m.order {
				oc := m.commands[outer]
				if !oc.Scope.outer() {
					continue
				}
				for _, ok := range m.keys[outer] {
					if key.Overlaps(k, ok) {
						src := m.source[inner]
						out = append(out, Problem{File: src.File, Line: src.Line, ID: inner, With: outer, Kind: Shadow, Key: k})
					}
				}
			}
		}
	}
	return out
}

// KeysOf is c's effective keys, a copy. A nil Map, or one that does not declare c.ID, gives c.Keys: a
// host with no map keeps every default.
func (m *Map) KeysOf(c Command) []key.Seq {
	if m != nil {
		if keys, ok := m.keys[c.ID]; ok {
			return slices.Clone(keys)
		}
	}
	return slices.Clone(c.Keys)
}

// Has reports whether c's effective keys hold s, without copying them.
func (m *Map) Has(c Command, s key.Seq) bool {
	if m != nil {
		if keys, ok := m.keys[c.ID]; ok {
			return slices.Contains(keys, s)
		}
	}
	return slices.Contains(c.Keys, s)
}

// Command is the declared command with id, its default keys a copy.
func (m *Map) Command(id string) (Command, bool) {
	if m == nil {
		return Command{}, false
	}
	c, ok := m.commands[id]
	c.Keys = slices.Clone(c.Keys)
	return c, ok
}

// SourceOf is where id's effective keys came from.
func (m *Map) SourceOf(id string) Source {
	if m == nil {
		return Source{}
	}
	return m.source[id]
}

// Bindings is the scope's declared bindings, sorted by their first key, then id. Reaches and ShadowedBy
// are left empty: they depend on routing, which a query fills.
func (m *Map) Bindings(s Scope) []Binding {
	if m == nil {
		return nil
	}
	var out []Binding
	for _, id := range m.order {
		c := m.commands[id]
		if c.Scope != s {
			continue
		}
		c.Keys = slices.Clone(c.Keys)
		out = append(out, Binding{Command: c, Keys: slices.Clone(m.keys[id]), Scope: s, Source: m.source[id]})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case len(a.Keys) == 0 || len(b.Keys) == 0:
			return len(a.Keys) > len(b.Keys)
		case a.Keys[0] != b.Keys[0]:
			return a.Keys[0].String() < b.Keys[0].String()
		}
		return a.Command.ID < b.Command.ID
	})
	return out
}

// Scopes is every scope a command is declared in, sorted.
func (m *Map) Scopes() []Scope {
	if m == nil {
		return nil
	}
	out := make([]Scope, 0, len(m.scopes))
	for s := range m.scopes {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// Commands is every declared command, in declaration order, their default keys copies.
func (m *Map) Commands() []Command {
	if m == nil {
		return nil
	}
	out := make([]Command, len(m.order))
	for i, id := range m.order {
		c := m.commands[id]
		c.Keys = slices.Clone(c.Keys)
		out[i] = c
	}
	return out
}

// Warnings is Build's warnings, a copy.
func (m *Map) Warnings() []Problem {
	if m == nil {
		return nil
	}
	return slices.Clone(m.warnings)
}
