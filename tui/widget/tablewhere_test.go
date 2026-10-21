package widget

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// dumpWhere writes a parsed filter as a prefix tree, fields as #positions: what the parser
// understood, precedence and all.
func dumpWhere(n whereNode) string {
	switch n := n.(type) {
	case *whereOr:
		return "(or " + dumpWhere(n.l) + " " + dumpWhere(n.r) + ")"
	case *whereAnd:
		return "(and " + dumpWhere(n.l) + " " + dumpWhere(n.r) + ")"
	case *whereNot:
		return "(not " + dumpWhere(n.x) + ")"
	case *whereNull:
		if n.not {
			return fmt.Sprintf("(notnull #%d)", n.col+1)
		}
		return fmt.Sprintf("(null #%d)", n.col+1)
	case *whereLike:
		var segs []string
		for _, s := range n.pat.segs {
			var b strings.Builder
			for _, r := range s {
				if r.any {
					b.WriteString("_")
				} else {
					b.WriteRune(r.r)
				}
			}
			segs = append(segs, b.String())
		}
		op := "like"
		if n.not {
			op = "notlike"
		}
		return fmt.Sprintf("(%s #%d %q)", op, n.col+1, strings.Join(segs, "|"))
	case *whereCompare:
		lit := ""
		switch n.lit.kind {
		case litNumber:
			lit = n.lit.num.RatString()
		case litString:
			lit = "'" + n.lit.s + "'"
		case litBool:
			lit = fmt.Sprint(n.lit.b)
		}
		return fmt.Sprintf("(%s #%d %s)", n.op, n.col+1, lit)
	}
	return fmt.Sprintf("?%T", n)
}

func parseDump(t *testing.T, text string, fields []TableField) string {
	t.Helper()
	p, err := ParseTableWhere(text, fields)
	if err != nil {
		t.Errorf("%q: %v", text, err)
		return ""
	}
	if p == nil {
		return "<nil>"
	}
	return dumpWhere(p.(*tableWhere).root)
}

// Each atom's form, names quoted and not, positions, and keywords in any case; a name matches
// exactly first, then case-insensitively when one field matches.
func TestParseTableWhere_ReadsEachAtom(t *testing.T) {
	fields := orderFields() // id Name "total sum" id City city `say "hi"` Age
	for text, want := range map[string]string{
		"":                           "<nil>",
		" \t\n":                      "<nil>",
		"Age = 18":                   "(= #8 18)",
		"age>=18":                    "(>= #8 18)",
		"Age <> 1":                   "(<> #8 1)",
		"Age != 1":                   "(!= #8 1)",
		"Age<-1.5e2":                 "(< #8 -150)",
		"Age <= .5":                  "(<= #8 1/2)",
		"Age > +7":                   "(> #8 7)",
		"Name < 'b'":                 "(< #2 'b')",
		"Name = 'it''s'":             "(= #2 'it's')",
		"Name = ''":                  "(= #2 '')",
		"Age IS NULL":                "(null #8)",
		"aGe Is NoT nUlL":            "(notnull #8)",
		"Name LIKE 'a%b_c'":          `(like #2 "a|b_c")`,
		"Name not like '\\%x'":       `(notlike #2 "%x")`,
		"#8 = TRUE":                  "(= #8 true)",
		"#1 = false":                 "(= #1 false)",
		`"total sum" > 0`:            "(> #3 0)",
		`"say ""hi""" = 'x'`:         "(= #7 'x')",
		"City = 'Seoul'":             "(= #5 'Seoul')", // exact, though city also matches case-insensitively
		"city = 'Seoul'":             "(= #6 'Seoul')",
		"name = 'x'":                 "(= #2 'x')", // one field matches case-insensitively
		"#4 = 1":                     "(= #4 1)",
		"Age = 12345678901234567890": "(= #8 12345678901234567890)",
		"NOT NOT Age = 1":            "(not (not (= #8 1)))",
	} {
		if got := parseDump(t, text, fields); got != want {
			t.Errorf("%q: %s, want %s", text, got, want)
		}
	}
}

// NOT binds tighter than AND, and AND than OR; parentheses group; chains associate to the left.
func TestParseTableWhere_Precedence(t *testing.T) {
	fields := orderFields()
	for text, want := range map[string]string{
		"Age = 1 OR Age = 2 AND Age = 3":      "(or (= #8 1) (and (= #8 2) (= #8 3)))",
		"Age = 1 AND Age = 2 OR Age = 3":      "(or (and (= #8 1) (= #8 2)) (= #8 3))",
		"NOT Age = 1 AND Age = 2":             "(and (not (= #8 1)) (= #8 2))",
		"NOT Age = 1 OR Age = 2":              "(or (not (= #8 1)) (= #8 2))",
		"(Age = 1 OR Age = 2) AND Age = 3":    "(and (or (= #8 1) (= #8 2)) (= #8 3))",
		"NOT (Age = 1 OR Age = 2)":            "(not (or (= #8 1) (= #8 2)))",
		"Age = 1 AND Age = 2 AND Age = 3":     "(and (and (= #8 1) (= #8 2)) (= #8 3))",
		"Age = 1 or Age = 2 or Age = 3":       "(or (or (= #8 1) (= #8 2)) (= #8 3))",
		"((Age = 1))":                         "(= #8 1)",
		"Age IS NULL OR NOT Name LIKE 'a%'":   `(or (null #8) (not (like #2 "a|")))`,
		"Age = 1 AND (Name = 'a' OR #1 = 2)":  "(and (= #8 1) (or (= #2 'a') (= #1 2)))",
		"NOT Age IS NOT NULL AND Name <> 'x'": "(and (not (notnull #8)) (<> #2 'x'))",
	} {
		if got := parseDump(t, text, fields); got != want {
			t.Errorf("%q: %s, want %s", text, got, want)
		}
	}
}

// Each error says what is wrong and the byte where.
func TestParseTableWhere_ErrorsCarryTheOffsetOfTheirToken(t *testing.T) {
	fields := orderFields()
	for _, c := range []struct {
		text   string
		offset int
		says   string
	}{
		{"nope = 1", 0, `no field "nope"`},
		{"Age = 1 AND id = 2", 12, `"id" names fields #1 and #4: write its position`},
		{`"ID" = 1`, 0, `"ID" names fields #1 and #4`},
		{"#9 = 1", 0, "no field #9: the table has 8"},
		{"Age = 1 OR #0 = 1", 11, "no field #0"},
		{"#x = 1", 0, `"#x" is not a position`},
		{"Age = NULL", 6, "= NULL is never true: write IS NULL"},
		{"Age <> null", 7, "<> NULL is never true: write IS NOT NULL"},
		{"Age != NULL", 7, "write IS NOT NULL"},
		{"Age < NULL", 6, "write IS NULL or IS NOT NULL"},
		{"Age = ", 6, "a value is missing after ="},
		{"Age 5", 4, `expected an operator, IS or LIKE before "5"`},
		{"Age", 3, "Age needs an operator, IS or LIKE"},
		{"(Age = 1", 0, "the parenthesis is not closed"},
		{"Age = 1 AND (Age = 2", 12, "the parenthesis is not closed"},
		{"Age = 1)", 7, "this ) closes nothing"},
		{"(Age = 1 Name = 'x')", 9, `expected AND, OR or ) before "Name"`},
		{"Age = 1 Name = 'x'", 8, `expected AND or OR before "Name"`},
		{"Age = 'abc", 6, "the string is not closed"},
		{`"Age = 1`, 0, "the quoted name is not closed"},
		{`Age = "Name"`, 6, `"\"Name\"" is a field's name: write a string in single quotes`},
		{"Age = Name", 6, `expected a number, a 'string', TRUE or FALSE, not "Name"`},
		{"Age = 1 AND", 11, "a condition is missing"},
		{"AND Age = 1", 0, "expected a field, not AND"},
		{"Age = 1 AND or = 2", 12, "expected a field, not OR: quote a field of that name"},
		{"= 1", 0, `expected a field before "="`},
		{"Age IS 1", 7, `expected NULL after IS, not "1"`},
		{"Age IS NOT", 10, "expected NULL after IS"},
		{"Name NOT 'a'", 9, "expected LIKE after NOT"},
		{"Name LIKE 1", 10, "expected a 'pattern' in single quotes after LIKE"},
		{`Name LIKE 'a\'`, 10, `the pattern ends in \`},
		{"Age ! 1", 4, "! is not an operator: write != or NOT"},
		{"Age = 1x", 6, `"1x" is not a number`},
		{"Age = 1e99999", 6, "1e99999 is out of range"},
		{"Age = 1, Name = 2", 7, `unexpected ','`},
		{"Age = 1; DROP", 7, `unexpected ';'`},
	} {
		_, err := ParseTableWhere(c.text, fields)
		var we *TableBarError
		if !errors.As(err, &we) {
			t.Errorf("%q: err %v, want a *TableBarError", c.text, err)
			continue
		}
		if we.Offset != c.offset || !strings.Contains(we.Error(), c.says) {
			t.Errorf("%q: offset %d %q, want offset %d saying %q", c.text, we.Offset, we.Error(), c.offset, c.says)
		}
	}
	if _, err := ParseTableWhere("a = 1", nil); err == nil {
		t.Error("a name over no fields parsed")
	}
}

// whereRows is a kinded source of (id, v) rows, v's kind given per row; plainRows the same text
// without kinds.
func whereRows(rows ...[3]string) *kindedSource { return kinded(rows...) }

func plainRows(vs ...string) *rowsSource {
	s := &rowsSource{fields: []TableField{{Title: "id"}, {Title: "v"}}}
	for i, v := range vs {
		s.rows = append(s.rows, []string{fmt.Sprint(i), v})
	}
	return s
}

// kept is the ids of the rows text keeps, in source order.
func kept(t *testing.T, src TableSource, text string) []string {
	t.Helper()
	p, err := ParseTableWhere(text, src.Fields())
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	var out []string
	for r := range src.Len() {
		if p.Eval(src, r) == TableTrue {
			out = append(out, src.Cell(r, 0))
		}
	}
	return out
}

// truth is what text answers for source row r.
func truth(t *testing.T, src TableSource, text string, r int) TableTruth {
	t.Helper()
	p, err := ParseTableWhere(text, src.Fields())
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return p.Eval(src, r)
}

// NULL is unknown to every comparison, and AND, OR and NOT follow SQL's three-valued logic.
func TestTableWhere_ThreeValuedLogic(t *testing.T) {
	src := whereRows([3]string{"a", "", "z"}, [3]string{"b", "0", "n"}, [3]string{"c", "5", "n"}, [3]string{"d", "", "z"})
	for text, want := range map[string][]string{
		"v IS NULL OR v > 1":       {"a", "c", "d"},
		"NOT (v > 1)":              {"b"},
		"v > 1":                    {"c"},
		"v IS NOT NULL":            {"b", "c"},
		"NOT v IS NULL":            {"b", "c"},
		"NOT (v > 1 OR v < 1)":     nil,
		"v > 1 OR id = 'a'":        {"a", "c"},
		"NOT (v > 1 AND id = 'x')": {"a", "b", "c", "d"}, // unknown AND false is false; NOT false keeps
	} {
		if got := kept(t, src, text); !slices.Equal(got, want) {
			t.Errorf("%q kept %v, want %v", text, got, want)
		}
	}
	// row a: v is NULL
	for text, want := range map[string]TableTruth{
		"v > 1":                    TableUnknown,
		"NOT v > 1":                TableUnknown,
		"v > 1 AND id = 'x'":       TableFalse,
		"id = 'x' AND v > 1":       TableFalse,
		"v > 1 AND id = 'a'":       TableUnknown,
		"v > 1 OR id = 'a'":        TableTrue,
		"id = 'a' OR v > 1":        TableTrue,
		"v > 1 OR id = 'x'":        TableUnknown,
		"NOT (v > 1 OR id = 'x')":  TableUnknown,
		"NOT (v > 1 AND id = 'x')": TableTrue,
		"v IS NULL":                TableTrue,
		"v IS NOT NULL":            TableFalse,
	} {
		if got := truth(t, src, text, 0); got != want {
			t.Errorf("%q on a NULL: %v, want %v", text, got, want)
		}
	}
}

// Numbers compare as exact decimals, never through float64.
func TestTableWhere_NumbersAreExactDecimals(t *testing.T) {
	src := whereRows([3]string{"x", "12345678901234567890.123", "n"}, [3]string{"y", " -0.5 ", "n"})
	for text, want := range map[string][]string{
		"v > 12345678901234567890.122":  {"x"},
		"v > 12345678901234567890.123":  nil,
		"v = 12345678901234567890.1230": {"x"},
		"v < 12345678901234567890.124":  {"x", "y"},
		"v = -5e-1":                     {"y"},
		"v >= -.5":                      {"x", "y"},
	} {
		if got := kept(t, src, text); !slices.Equal(got, want) {
			t.Errorf("%q kept %v, want %v", text, got, want)
		}
	}
}

// A NULL cell matches IS NULL and nothing else, NOT included; booleans compare as booleans.
func TestTableWhere_NullsAndBooleans(t *testing.T) {
	src := whereRows([3]string{"z", "", "z"}, [3]string{"t", "true", "b"}, [3]string{"f", "false", "b"})
	for _, text := range []string{
		"v = 1", "v <> 1", "v = ''", "v <> ''", "v = TRUE", "v <> TRUE", "v LIKE '%'", "v NOT LIKE 'x'",
		"NOT v = 1", "NOT v <> ''", "NOT v = TRUE", "NOT v LIKE '%'", "v IS NOT NULL",
	} {
		if got := kept(t, src, text); slices.Contains(got, "z") {
			t.Errorf("%q kept the NULL", text)
		}
	}
	for text, want := range map[string][]string{
		"v IS NULL":     {"z"},
		"v = TRUE":      {"t"},
		"v = FALSE":     {"f"},
		"v <> TRUE":     {"f"},
		"v > FALSE":     {"t"},
		"NOT v = TRUE":  {"f"},
		"v = 'true'":    {"t"},
		"v IS NOT NULL": {"t", "f"},
	} {
		if got := kept(t, src, text); !slices.Equal(got, want) {
			t.Errorf("%q kept %v, want %v", text, got, want)
		}
	}
}

// In one field of mixed kinds, a pairing that does not compare is unknown: the row is not kept,
// and NOT does not keep it either. Every kind compares with a string as text, and LIKE takes every
// non-NULL cell's text.
func TestTableWhere_MismatchedKindsAreUnknown(t *testing.T) {
	src := whereRows(
		[3]string{"b", "true", "b"}, [3]string{"bf", "false", "b"},
		[3]string{"n", "1", "n"}, [3]string{"neg", "-1", "n"},
		[3]string{"j", "1", "j"}, [3]string{"abc", "abc", "t"}, [3]string{"seven", "7", "t"},
		[3]string{"ttrue", "true", "t"}, [3]string{"nan", "NaN", "n"}, [3]string{"inf", "Infinity", "n"},
		[3]string{"null", "", "z"}, [3]string{"odd", "1", "?"})
	for text, want := range map[string][]string{
		"v > 0":        {"n", "seven", "odd"},
		"NOT (v > 0)":  {"neg"},
		"v = 1":        {"n", "odd"},
		"NOT v = 1":    {"neg", "seven"},
		"v = TRUE":     {"b"},
		"NOT v = TRUE": {"bf"},
		"v <> TRUE":    {"bf"},
		"v = '1'":      {"n", "j", "odd"},
		"v = 'true'":   {"b", "ttrue"},
		"v = 'NaN'":    {"nan"},
		"v < 'b'":      {"n", "neg", "j", "abc", "seven", "nan", "inf", "odd"},
		"v LIKE '%'":   {"b", "bf", "n", "neg", "j", "abc", "seven", "ttrue", "nan", "inf", "odd"},
		"v IS NULL":    {"null"},
	} {
		if got := kept(t, src, text); !slices.Equal(got, want) {
			t.Errorf("%q kept %v, want %v", text, got, want)
		}
	}
	// each mismatch, alone
	for _, c := range []struct {
		row  int
		text string
	}{
		{0, "v = 1"}, {0, "v > 0"}, {2, "v = TRUE"}, {4, "v = 1"}, {4, "v > 0"}, {5, "v > 0"},
		{5, "v = 0"}, {7, "v = TRUE"}, {8, "v > 0"}, {9, "v > 0"}, {4, "v = TRUE"},
	} {
		for _, text := range []string{c.text, "NOT " + c.text, "NOT (" + c.text + ")"} {
			if got := truth(t, src, text, c.row); got != TableUnknown {
				t.Errorf("%q on %s: %v, want unknown", text, src.rows[c.row][0], got)
			}
		}
	}
}

// Without kinds every cell is text: one whose text is a decimal compares with a number, any other
// is unknown to it, no cell is NULL, and TRUE and FALSE compare with none.
func TestTableWhere_WithoutKinds(t *testing.T) {
	src := plainRows("10", "9", "abc", " 3 ", "NULL", "", "1/2", "0x10", "true", "1e5")
	for text, want := range map[string][]string{
		"v > 5":         {"0", "1", "9"},
		"NOT (v > 5)":   {"3"},
		"v IS NULL":     nil,
		"v IS NOT NULL": {"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"},
		"v = 'NULL'":    {"4"},
		"v = TRUE":      nil,
		"NOT v = TRUE":  nil,
		"v = ''":        {"5"},
		"v = 100000":    {"9"},
		"v LIKE '1%'":   {"0", "6", "9"},
	} {
		if got := kept(t, src, text); !slices.Equal(got, want) {
			t.Errorf("%q kept %v, want %v", text, got, want)
		}
	}
}

// LIKE: % any run, _ one character (not one byte), \ the next character itself, case-sensitive,
// the whole text matched.
func TestTableWhere_Like(t *testing.T) {
	for _, c := range []struct {
		pattern, text string
		want          bool
	}{
		{"a%", "apple", true}, {"a%", "Apple", false}, {"A%", "apple", false}, {"%LE", "apple", false},
		{"%le", "apple", true}, {"%pp%", "apple", true}, {"%pp", "apple", false}, {"a_ple", "apple", true},
		{"a_le", "apple", false}, {"_____", "apple", true}, {"____", "apple", false}, {"", "", true},
		{"", "a", false}, {"%", "", true}, {"%%", "abc", true}, {"é_", "éa", true}, {"_", "é", true},
		{"__", "é", false}, {`100\%`, "100%", true}, {`100\%`, "1000", false}, {`a\_c`, "abc", false},
		{`a\_c`, "a_c", true}, {`a\\b`, `a\b`, true}, {"a%b%c", "abc", true}, {"a%b%c", "acb", false},
		{"a%b%c", "aXbYbZc", true}, {"%a%a%", "a", false}, {"%a%a%", "aa", true}, {"ab%ba", "aba", false},
		{"ab%ba", "abba", true},
	} {
		pat, msg := compileLike(c.pattern)
		if msg != "" {
			t.Errorf("%q: %s", c.pattern, msg)
			continue
		}
		if got := pat.match(c.text); got != c.want {
			t.Errorf("%q LIKE %q = %v, want %v", c.text, c.pattern, got, c.want)
		}
	}
}

// A pattern that makes a backtracking matcher try every way to place its %s is matched in one
// pass: each segment is placed once.
func TestTableWhere_LikeIsLinear(t *testing.T) {
	long := strings.Repeat("a", 200000)
	for _, pattern := range []string{
		strings.Repeat("%a", 40) + "%b",
		strings.Repeat("%a", 40) + "%b%",
		strings.Repeat("%a_", 20) + "%b%",
	} {
		pat, _ := compileLike(pattern)
		start := time.Now()
		if pat.match(long) {
			t.Errorf("%q matched a text with no b", pattern[:12])
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("%q took %v over %d characters", pattern[:12], d, len(long))
		}
	}
}

func TestTableTruth_String(t *testing.T) {
	if fmt.Sprint(TableFalse, TableUnknown, TableTrue) != "false unknown true" {
		t.Error(fmt.Sprint(TableFalse, TableUnknown, TableTrue))
	}
}
