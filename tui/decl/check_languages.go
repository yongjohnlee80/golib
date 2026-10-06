package decl

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/i18n"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// CheckLanguages judges a program's catalogs against its screens, in every language they
// hold, golib's and the program's own (the options [NewProgram] takes, its [Translations]
// among them). Each problem is reported once, labelled:
//
//   - a message a language translates that English does not carry: nothing falls back to it,
//     and its mnemonic has no English letter to keep;
//   - a translation marking another letter than English's: the App shows it with English's
//     letter appended, so the marker the translator wrote is not the key the user presses;
//   - two rows of one menu level answering to the same key, in the languages where they do:
//     the first row keeps the key and the second cannot be reached by it.
//
// Nothing runs: the layout is mounted and released, and no App is built.
func CheckLanguages(opts ...ProgramOption) error {
	c, err := configure(opts)
	if err != nil {
		return err
	}
	set := c.catalogs
	if set == nil {
		set = i18n.Toolkit()
	}
	errs := catalogProblems(set)

	spec, err := qml.QML{File: c.layoutFile}.Parse(c.layout)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	p, err := mount(c, spec)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	errs = append(errs, menuCollisions(p, set)...)
	return errors.Join(append(errs, p.tree.Destroy())...)
}

// catalogProblems is every id a language carries that English lacks, and every translation
// whose marker is not on English's letter.
func catalogProblems(set *i18n.Set) []error {
	english := map[string]bool{}
	for _, id := range set.IDs(i18n.English) {
		english[id] = true
	}
	var errs []error
	for _, lang := range set.Languages() {
		if lang == i18n.English {
			continue
		}
		for _, id := range set.IDs(lang) {
			if !english[id] {
				errs = append(errs, fmt.Errorf("%s: message %q has no English, so nothing falls back to it", lang, id))
				continue
			}
			text, _ := set.Lookup(lang, id)
			en, _ := set.Lookup(i18n.English, id)
			_, got, _ := tui.ParseMnemonic(text)
			_, want, _ := tui.ParseMnemonic(en)
			if got != want {
				errs = append(errs, fmt.Errorf("%s: message %q is %q, marking %s where English marks %s; "+
					"it is shown with English's letter", lang, id, text, keyName(got), keyName(want)))
			}
		}
	}
	return errs
}

// menuCollisions is every pair of rows, in one level of one menu bar, that answer to the same
// key: a collision found in several languages is reported once, naming them.
func menuCollisions(p *Program, set *i18n.Set) []error {
	langs := set.Languages()
	found := map[string][]string{} // a collision, by position: the languages it occurs in
	what := map[string]string{}    // and how to say it
	var order []string
	ids := make([]decl.NodeID, 0, len(p.adapter.nodes))
	for id := range p.adapter.nodes {
		ids = append(ids, id)
	}
	// In node order, which is document order, so the report reads the way the file does.
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		bar, ok := p.adapter.nodes[id].comp.(*menuBarNode)
		if !ok {
			continue
		}
		for _, lang := range langs {
			walkLevels("the menu bar", bar.rows, set, func(level string, rows []*menuNode) {
				seen := map[rune]int{}
				for i, r := range rows {
					if r.kind == "MenuSeparator" {
						continue
					}
					_, key := rowKey(r.model, set, lang)
					if key == 0 {
						continue
					}
					first, dup := seen[key]
					if !dup {
						seen[key] = i
						continue
					}
					// One collision is the same two rows whatever the language, so it is keyed by
					// position and named by its English labels, and reported once.
					at := fmt.Sprintf("%s|%d|%d|%c", level, first, i, key)
					if found[at] == nil {
						a, _ := rowKey(rows[first].model, set, i18n.English)
						b, _ := rowKey(r.model, set, i18n.English)
						order = append(order, at)
						what[at] = fmt.Sprintf("%s: %q and %q both answer to %s", level, a, b, keyName(key))
					}
					found[at] = append(found[at], lang)
				}
			})
		}
	}
	errs := make([]error, 0, len(order))
	for _, at := range order {
		errs = append(errs, fmt.Errorf("%s (in %s); the first keeps the key", what[at], strings.Join(found[at], ", ")))
	}
	return errs
}

// walkLevels calls fn for every level of a menu: the bar's own rows, then each submenu's.
// A level is named by its menu's English title.
func walkLevels(name string, rows []*menuNode, set *i18n.Set, fn func(level string, rows []*menuNode)) {
	fn(name, rows)
	for _, r := range rows {
		if len(r.children) > 0 {
			label, _ := rowKey(r.model, set, i18n.English)
			walkLevels(fmt.Sprintf("menu %q", label), r.children, set, fn)
		}
	}
}

// rowKey is a row's label and the key it answers to in lang, as the App shows it: a message
// keeps English's letter wherever English carries it.
func rowKey(m widget.MenuItemModel, set *i18n.Set, lang string) (string, rune) {
	if m.LabelMsg == (tui.Message{}) {
		return m.Label, m.Hotkey
	}
	text, ok := set.Lookup(lang, m.LabelMsg.ID)
	if !ok {
		text = m.LabelMsg.ID
	}
	label, key, _ := tui.ParseMnemonic(text)
	if en, ok := set.Lookup(i18n.English, m.LabelMsg.ID); ok {
		_, key, _ = tui.ParseMnemonic(en)
	}
	return label, key
}

// keyName writes a mnemonic key for a report.
func keyName(r rune) string {
	if r == 0 {
		return "no letter"
	}
	return fmt.Sprintf("%q", r)
}
