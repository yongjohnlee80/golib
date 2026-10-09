package highlight

import "strings"

// Catalog is an immutable snapshot of a composed Repository. It shares factory
// functions, which must capture immutable configuration, never live source state.
type Catalog struct{ repository *Repository }

func copyDefinition(d Definition) Definition {
	d.Extensions = append([]string(nil), d.Extensions...)
	d.Aliases = append([]string(nil), d.Aliases...)
	return d
}

// Snapshot copies definition metadata after all registrations have been folded.
// Later Add calls and mutations of returned metadata cannot change the catalog.
func (r *Repository) Snapshot() *Catalog {
	repo := NewRepository()
	for _, d := range r.defs {
		repo.Add(copyDefinition(d))
	}
	return &Catalog{repository: repo}
}

// Definition finds an exact canonical name and returns independent metadata.
func (c *Catalog) Definition(name string) (Definition, bool) {
	if c == nil {
		return Definition{}, false
	}
	d, ok := c.repository.Definition(name)
	return copyDefinition(d), ok
}

// Names lists canonical names in deterministic order.
func (c *Catalog) Names() []string {
	if c == nil {
		return nil
	}
	return c.repository.Names()
}

// DefinitionForFileName chooses by descending priority, then canonical name.
func (c *Catalog) DefinitionForFileName(name string) (Definition, bool) {
	if c == nil {
		return Definition{}, false
	}
	d, ok := c.repository.DefinitionForFileName(name)
	return copyDefinition(d), ok
}

// DefinitionForLanguage resolves a case-insensitive canonical name or alias.
// Priority and name break ties just as they do for filename selection.
func (c *Catalog) DefinitionForLanguage(label string) (Definition, bool) {
	if c == nil {
		return Definition{}, false
	}
	label = strings.TrimSpace(label)
	var match Definition
	found := false
	for _, name := range c.repository.Names() {
		d, _ := c.repository.Definition(name)
		if found && d.Priority <= match.Priority {
			continue
		}
		matches := strings.EqualFold(label, d.Name)
		for _, alias := range d.Aliases {
			matches = matches || strings.EqualFold(label, alias)
		}
		if matches {
			match, found = d, true
		}
	}
	return copyDefinition(match), found
}
