package i18n

import (
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/errs"
)

// English is the language every lookup falls back to, and the one whose mnemonic letters
// golib/tui keeps in every other language.
const English = "en"

// Set layers catalogs and looks messages up across them. The zero value is an empty Set,
// ready to use.
//
// A Set is filled before it is used: [Set.Add] and [Set.LoadDir] must not run while
// another goroutine calls [Set.Lookup].
type Set struct {
	byLang map[string][]*Catalog
}

// Add layers c over the catalogs already added for its language, so c's messages replace
// the same ids from earlier ones. A catalog that names no language is refused: nothing
// would know which language it is.
func (s *Set) Add(c *Catalog) error {
	if c == nil || c.language == "" {
		return errs.Wrap(errs.ErrInvalidArgument, "i18n: a catalog must name its language")
	}
	if s.byLang == nil {
		s.byLang = map[string][]*Catalog{}
	}
	s.byLang[c.language] = append(s.byLang[c.language], c)
	return nil
}

// LoadDir adds every catalog in dir of fsys named <prefix>_<language>.xml, in name order:
// "editor_ko_KR.xml" is Korean, "editor_es.xml" Spanish. A catalog whose own language
// attribute disagrees with its file name is refused, since the two would load it as
// different languages; one that names none takes its file name's. Other files are ignored.
// Nothing is added unless every catalog parses.
func (s *Set) LoadDir(fsys fs.FS, dir, prefix string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return errs.WrapCause(errs.ErrInvalidArgument, err, "i18n: reading the catalogs in %q", dir)
	}
	var loaded []*Catalog
	for _, e := range entries {
		lang, ok := languageOf(e.Name(), prefix)
		if e.IsDir() || !ok {
			continue
		}
		file := path.Join(dir, e.Name())
		c, err := parseFile(fsys, file)
		if err != nil {
			return err
		}
		switch {
		case c.language == "":
			c.language = lang
		case c.language != lang:
			return errs.Wrap(errs.ErrInvalidArgument,
				"i18n: %s says its language is %q, but its name says %q", file, c.language, lang)
		}
		loaded = append(loaded, c)
	}
	for _, c := range loaded {
		if err := s.Add(c); err != nil {
			return err
		}
	}
	return nil
}

// Lookup is id's text in language: in that language ("es_MX"), then its base ("es"), then
// [English]. Within one language, the catalog added last wins. ok is false when no catalog
// has the id; golib/tui then shows the id itself.
func (s *Set) Lookup(language, id string) (text string, ok bool) {
	for _, lang := range chain(canonical(language)) {
		cats := s.byLang[lang]
		for i := len(cats) - 1; i >= 0; i-- {
			if t, ok := cats[i].texts[id]; ok {
				return t, true
			}
		}
	}
	return "", false
}

// Languages is every language some catalog in the Set names, sorted.
func (s *Set) Languages() []string {
	out := make([]string, 0, len(s.byLang))
	for lang := range s.byLang {
		out = append(out, lang)
	}
	sort.Strings(out)
	return out
}

// IDs is every message id the catalogs of exactly language hold, sorted: what that language
// translates, without its fallbacks. A check uses it to find ids English does not carry.
func (s *Set) IDs(language string) []string {
	seen := map[string]bool{}
	for _, c := range s.byLang[canonical(language)] {
		for id := range c.texts {
			seen[id] = true
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// chain is the languages a lookup tries, in order, without repeats.
func chain(lang string) []string {
	out := []string{lang}
	if base, _, cut := strings.Cut(lang, "_"); cut && base != "" {
		out = append(out, base)
	}
	if out[len(out)-1] != English && lang != English {
		out = append(out, English)
	}
	return out
}

// languageOf is the language a catalog file's name gives it, when the name is
// <prefix>_<language>.xml.
func languageOf(name, prefix string) (string, bool) {
	lang, ok := strings.CutPrefix(name, prefix+"_")
	if !ok {
		return "", false
	}
	lang, ok = strings.CutSuffix(lang, ".xml")
	if !ok || lang == "" {
		return "", false
	}
	return canonical(lang), true
}

func parseFile(fsys fs.FS, file string) (*Catalog, error) {
	f, err := fsys.Open(file)
	if err != nil {
		return nil, errs.WrapCause(errs.ErrInvalidArgument, err, "i18n: opening %s", file)
	}
	defer f.Close()
	return parse(f, file)
}
