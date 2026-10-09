package highlight_test

import (
	"github.com/yongjohnlee80/golib/highlight"
	"testing"
)

func TestCatalogSnapshotsCompositionAndCopiesLookupMetadata(t *testing.T) {
	t.Parallel()
	defs := highlight.NewRepository(highlight.Definition{Name: "Wide", Extensions: []string{"*.src"}, Aliases: []string{"source"}}, highlight.Definition{Name: "Narrow", Extensions: []string{"*.src"}, Aliases: []string{"source"}, Priority: 1})
	catalog := defs.Snapshot()
	defs.Add(highlight.Definition{Name: "Narrow"})
	d, ok := catalog.DefinitionForLanguage(" SOURCE ")
	if !ok || d.Name != "Narrow" {
		t.Fatal(d, ok)
	}
	d.Extensions[0] = "*.broken"
	d.Aliases[0] = "broken"
	again, ok := catalog.DefinitionForFileName("a.src")
	if !ok || again.Name != "Narrow" {
		t.Fatal(again, ok)
	}
	if _, ok = catalog.DefinitionForLanguage("source"); !ok {
		t.Fatal("lookup metadata mutated snapshot")
	}
}

func TestSourceFactoryReceivesTheSelectedCatalog(t *testing.T) {
	r := highlight.NewRepository()
	calls := 0
	r.Add(highlight.Definition{Name: "Custom", SourceFactory: func(c *highlight.Catalog) highlight.Source {
		calls++
		if _, ok := c.Definition("Other"); !ok {
			t.Fatal("factory got an unrelated catalog")
		}
		return highlight.Source{}
	}}, highlight.Definition{Name: "Other"})
	c := r.Snapshot()
	d, _ := c.Definition("Custom")
	d.NewSource(c)
	d.NewSource(c)
	if calls != 2 {
		t.Fatal(calls)
	}
}
