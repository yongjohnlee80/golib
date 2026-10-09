package highlight

import (
	"path"
	"sort"
)

// Definition is a highlighter by name, with the files it is for —
// KSyntaxHighlighting's Definition: its name() and its extensions(), the
// file-name patterns ("*.qml") it claims.
type Definition struct {
	Name       string
	Extensions []string
	// Priority settles a file two definitions claim, as KSyntaxHighlighting's
	// priority does: the higher wins. A definition for a narrower pattern —
	// "*.sqlite.sql" beside "*.sql" — takes a higher one. Zero by default.
	Priority    int
	Highlighter Highlighter
	// Aliases are labels by which a fenced code block names this language.
	Aliases []string
	// DocumentAdapter excludes document wrappers from embedded code lookup.
	DocumentAdapter bool
	// SourceFactory creates independent highlighting/indentation/state ownership.
	// It takes precedence over Highlighter and does no I/O.
	SourceFactory func(*Catalog) Source
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
// base name — KSyntaxHighlighting's definitionForFileName. Where several claim
// it, the highest Priority wins, and among equals the one first by name, so
// the answer never depends on map order.
func (r *Repository) DefinitionForFileName(file string) (Definition, bool) {
	base := path.Base(file)
	var match Definition
	found := false
	for _, n := range r.Names() {
		d := r.defs[n]
		if found && d.Priority <= match.Priority {
			continue
		}
		for _, pat := range d.Extensions {
			if ok, _ := path.Match(pat, base); ok {
				match, found = d, true
				break
			}
		}
	}
	return match, found
}
