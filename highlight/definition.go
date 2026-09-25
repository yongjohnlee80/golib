package highlight

import (
	"path"
	"sort"
)

// Definition is a highlighter by name, with the files it is for —
// KSyntaxHighlighting's Definition: its name() and its extensions(), the
// file-name patterns ("*.qml") it claims.
type Definition struct {
	Name        string
	Extensions  []string
	Highlighter Highlighter
}

// Repository is a set of definitions, by name — KSyntaxHighlighting's
// Repository, from which an editor asks for a definition by name, or by the
// name of the file it is showing.
type Repository struct{ defs map[string]Definition }

// NewRepository holds the given definitions; a later one replaces an earlier
// one of the same name.
func NewRepository(defs ...Definition) *Repository {
	r := &Repository{defs: map[string]Definition{}}
	r.Add(defs...)
	return r
}

// Add adds definitions, replacing any of the same name.
func (r *Repository) Add(defs ...Definition) {
	for _, d := range defs {
		r.defs[d.Name] = d
	}
}

// Definition is the definition of that name.
func (r *Repository) Definition(name string) (Definition, bool) {
	d, ok := r.defs[name]
	return d, ok
}

// Names are the definitions' names, sorted.
func (r *Repository) Names() []string {
	out := make([]string, 0, len(r.defs))
	for n := range r.defs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// DefinitionForFileName is the definition whose extensions match the file's
// base name — KSyntaxHighlighting's definitionForFileName. Where two claim it,
// the one first by name wins, so the answer never depends on map order.
func (r *Repository) DefinitionForFileName(file string) (Definition, bool) {
	base := path.Base(file)
	for _, n := range r.Names() {
		d := r.defs[n]
		for _, pat := range d.Extensions {
			if ok, _ := path.Match(pat, base); ok {
				return d, true
			}
		}
	}
	return Definition{}, false
}
