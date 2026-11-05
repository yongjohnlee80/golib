package mermaid

import (
	"fmt"
	"slices"
)

// Kind is a diagram type. Each kind's file declares its own value, explicitly (never iota), and
// registers it in init: no list of kinds is kept in one place, so a new kind edits no shared file,
// and two kinds can't take one value unnoticed (register panics).
type Kind uint8

// kindSpec is a registered kind: its name, the header keywords that start one, and its parser.
type kindSpec struct {
	kind     Kind
	name     string
	keywords []string
	parse    func(p *parser, rest string, restOff int) (Diagram, error)
}

var (
	registry  = map[Kind]kindSpec{}
	byKeyword = map[string]Kind{}
)

// register adds a kind. A Kind, a name or a keyword registered twice panics: every binary runs its
// package's init, so a clash fails each test run, never a release.
func register(k Kind, name string, keywords []string, parse func(p *parser, rest string, restOff int) (Diagram, error)) {
	if old, dup := registry[k]; dup {
		panic(fmt.Sprintf("mermaid: kind %d registered twice (%s, %s)", k, old.name, name))
	}
	for _, s := range registry {
		if s.name == name {
			panic(fmt.Sprintf("mermaid: kind name %q registered twice (%d, %d)", name, s.kind, k))
		}
	}
	for _, kw := range keywords {
		if other, dup := byKeyword[kw]; dup {
			panic(fmt.Sprintf("mermaid: keyword %q registered twice (%s, %s)", kw, registry[other].name, name))
		}
	}
	registry[k] = kindSpec{kind: k, name: name, keywords: slices.Clone(keywords), parse: parse}
	for _, kw := range keywords {
		byKeyword[kw] = k
	}
}

func (k Kind) String() string {
	if s, ok := registry[k]; ok {
		return s.name
	}
	return fmt.Sprintf("%d", int(k))
}

// KindInfo is a diagram kind Parse reads, and the header keywords that start one ("flowchart" and
// "graph" for Flowchart).
type KindInfo struct {
	Kind     Kind
	Keywords []string
}

// Kinds are the diagram kinds Parse reads, in Kind order, each with its header keywords: the
// registry Parse dispatches from. A diagram of any other kind is ErrUnsupported.
func Kinds() []KindInfo {
	out := make([]KindInfo, 0, len(registry))
	for _, s := range registry {
		out = append(out, KindInfo{Kind: s.kind, Keywords: slices.Clone(s.keywords)})
	}
	slices.SortFunc(out, func(a, b KindInfo) int { return int(a.Kind) - int(b.Kind) })
	return out
}
