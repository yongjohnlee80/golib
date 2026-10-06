package i18n

import (
	"embed"
	"sync"

	"github.com/yongjohnlee80/golib/errs"
)

//go:embed catalogs/tui_*.xml
var toolkitFiles embed.FS

// toolkitCatalogs parses golib's own catalogs once: they are compiled in, so a failure is a
// broken build, not a condition a program can act on.
var toolkitCatalogs = sync.OnceValue(func() []*Catalog {
	var s Set
	if err := s.LoadDir(toolkitFiles, "catalogs", "tui"); err != nil {
		panic(errs.Fatal{Op: "i18n: Toolkit", Rule: "golib's own catalogs must parse", Detail: err.Error()})
	}
	var all []*Catalog
	for _, cats := range s.byLang {
		all = append(all, cats...)
	}
	return all
})

// Toolkit returns a new Set holding golib/tui's own catalogs: the messages its widgets
// show (button labels, file dialog titles, hints and placeholders, all named "tui.…") in
// English, Korean, Japanese, Simplified Chinese, Brazilian Portuguese and Spanish.
//
// Each call returns a separate Set, so what one application adds to it reaches no other.
// The catalogs inside are shared, and never change.
func Toolkit() *Set {
	s := &Set{}
	for _, c := range toolkitCatalogs() {
		_ = s.Add(c) // every toolkit catalog names its language, so Add cannot refuse it
	}
	return s
}
