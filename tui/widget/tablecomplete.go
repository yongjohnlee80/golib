package widget

import (
	"strconv"
	"strings"
)

// A BAR'S FIELD NAMES — completion of a field's name as the user types an ORDER BY or a WHERE
// bar's text, written so the bar reads it back.

// TableBar is the clause a TableFieldCompleter completes for. The two bars read a position
// differently, so a field two fields share is completed differently in each.
type TableBar int

const (
	TableOrderBar TableBar = iota // ORDER BY: a field two fields share is its 1-based number
	TableWhereBar                 // WHERE: a field two fields share is #n
)

// TableFieldName is a field's title as ParseTableOrder and ParseTableWhere both read it back: bare
// when it can be, else in double quotes ("" for a quote inside). A title two fields share reads
// back as an error either way, which names their positions.
func TableFieldName(title string) string {
	if orderQuoted(title) || whereQuoted(title) {
		return `"` + strings.ReplaceAll(title, `"`, `""`) + `"`
	}
	return title
}

// whereQuoted reports whether a WHERE bar would not read title bare as a name: empty, holding a
// byte that ends a bare word, beginning as a number or a position does, or a keyword.
func whereQuoted(title string) bool {
	if title == "" || title[0] == '#' || decimalStarts(title, 0) {
		return true
	}
	for i := 0; i < len(title); i++ {
		if whereStop(title[i]) {
			return true
		}
	}
	for _, kw := range whereKeywords {
		if strings.EqualFold(title, kw) {
			return true
		}
	}
	return false
}

// TableFieldCompleter completes a field's name in bar's text over fields, which it reads at each
// use, so it follows a new result: the fields whose title the word before the cursor begins,
// case-insensitively, each as its bar reads it back, in the fields' order. A title two fields
// share is the position that bar takes (ORDER BY 1, WHERE #1), so what Tab puts in parses; a word
// matches a field by its title or by what the bar writes for it, and one opened with a double
// quote matches past the quote. It offers nothing for an empty word, inside a 'string', or where
// a value is due: after a comparison's operator or LIKE.
func TableFieldCompleter(bar TableBar, fields func() []TableField) TextCompleter {
	return func(text string, cursor int) (int, []string) {
		start, word := wordBefore(text, cursor)
		if word == "" || inString(text[:start]) || valueDue(text[:start]) {
			return 0, nil
		}
		fs := fields()
		forms := fieldForms(bar, fs)
		typed := strings.TrimPrefix(word, `"`)
		var out []string
		for i, form := range forms {
			if hasPrefixFold(form, word) || hasPrefixFold(fs[i].Title, typed) {
				out = append(out, form)
			}
		}
		return start, out
	}
}

// fieldForms is each field as its bar takes it: a title two fields share as the bar's position,
// a title one field holds as TableFieldName writes it. The two never collide: a position is a
// bare number or #n, and a title that would read as one is quoted.
func fieldForms(bar TableBar, fields []TableField) []string {
	shared := make(map[string]int, len(fields))
	for _, f := range fields {
		shared[f.Title]++
	}
	forms := make([]string, len(fields))
	for i, f := range fields {
		if shared[f.Title] > 1 {
			forms[i] = strconv.Itoa(i + 1)
			if bar == TableWhereBar {
				forms[i] = "#" + forms[i]
			}
			continue
		}
		forms[i] = TableFieldName(f.Title)
	}
	return forms
}

// inString reports whether s ends inside a single-quoted string (” doubled inside it), outside
// any double-quoted name.
func inString(s string) bool {
	single, double := false, false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\'' && !double:
			single = !single
		case s[i] == '"' && !single:
			double = !double
		}
	}
	return single
}

// valueDue reports whether s, the text before a word, ends where a value is due: an operator, or
// the keyword LIKE.
func valueDue(s string) bool {
	s = strings.TrimRight(s, " \t\n\r")
	if s == "" {
		return false
	}
	if strings.IndexByte("=<>!", s[len(s)-1]) >= 0 {
		return true
	}
	j := len(s)
	for j > 0 && !completeStop(s[j-1]) {
		j--
	}
	return strings.EqualFold(s[j:], "LIKE")
}
