package sql_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/parsetest"
	"github.com/yongjohnlee80/golib/parse/sql"
)

// oneByteAtATime makes every chunk boundary land inside a construct, which is
// where a form that guessed shows itself.
type oneByteAtATime struct {
	b []byte
	i int
}

func (r *oneByteAtATime) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = r.b[r.i]
	r.i++
	return 1, nil
}

type lexed struct {
	kind parse.Kind
	text string
}

// lex drains a scan, taking each token's bytes as it arrives.
func lex(t *testing.T, forms []parse.Form, src string, stream bool) []lexed {
	t.Helper()
	lx := parse.New(parse.WithForms(forms...))

	var s *parse.Scan
	if stream {
		s = lx.Scan(context.Background(), &oneByteAtATime{b: []byte(src)}, parse.BorrowReader)
	} else {
		s = lx.ScanBytes(context.Background(), []byte(src))
	}
	defer s.Close()

	var out []lexed
	for tok, err := range s.Tokens() {
		if err != nil {
			t.Fatalf("lexing %q: %v", src, err)
		}
		if tok.Kind == parse.EOF {
			continue
		}
		v, aerr := s.Acquire(tok)
		if aerr != nil {
			t.Fatalf("Acquire(%v): %v", tok, aerr)
		}
		text, rerr := v.String()
		v.Close()
		if rerr != nil {
			t.Fatalf("reading %v: %v", tok, rerr)
		}
		out = append(out, lexed{tok.Kind, text})
	}
	return out
}

func render(toks []lexed) string {
	var b strings.Builder
	for i, tk := range toks {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(tk.kind.String())
		b.WriteByte('(')
		b.WriteString(tk.text)
		b.WriteByte(')')
	}
	return b.String()
}

func TestPostgreSQL_LexesItsOwnConstructs(t *testing.T) {
	for _, c := range []struct {
		name, src, want string
	}{
		{
			"quoted identifier is an identifier, not a string",
			`select "col" from t`,
			`Word(select) Space( ) Ident("col") Space( ) Word(from) Space( ) Word(t)`,
		},
		{
			"a doubled quote stays inside its literal",
			`'it''s'`,
			`String('it''s')`,
		},
		{
			"an ordinary literal does not treat backslash as an escape",
			`'a\' , 1`,
			`String('a\') Space( ) Punct(,) Space( ) Number(1)`,
		},
		{
			"an E string does",
			`E'a\'b'`,
			`String(E'a\'b')`,
		},
		{
			"dollar quoting, tagged and untagged",
			`$$body$$ $tag$x$tag$`,
			`String($$body$$) Space( ) String($tag$x$tag$)`,
		},
		{
			"a nested block comment closes at the right one",
			`/* a /* b */ c */ x`,
			`Comment(/* a /* b */ c */) Space( ) Word(x)`,
		},
		{
			"a line comment ends at the newline, which is the next token's",
			"-- c\nx",
			"Comment(-- c) Space(\n) Word(x)",
		},
		{
			"the longest operator wins",
			`a<=b<>c::d`,
			`Word(a) Operator(<=) Word(b) Operator(<>) Word(c) Operator(::) Word(d)`,
		},
		{
			"a lone slash is an operator, not an unterminated comment",
			`a/b`,
			`Word(a) Operator(/) Word(b)`,
		},
		{
			"a dollar parameter is not a dollar quote",
			`$1`,
			`Punct($) Number(1)`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := render(lex(t, sql.PostgreSQL(), c.src, false)); got != c.want {
				t.Errorf("\n src  %q\n got  %s\n want %s", c.src, got, c.want)
			}
		})
	}
}

func TestMySQL_LexesItsOwnConstructs(t *testing.T) {
	for _, c := range []struct {
		name, src, want string
	}{
		{
			"a backtick run is an identifier",
			"select `col` from t",
			"Word(select) Space( ) Ident(`col`) Space( ) Word(from) Space( ) Word(t)",
		},
		{
			"a double-quoted run is a STRING here, not an identifier",
			`"text"`,
			`String("text")`,
		},
		{
			"backslash escapes inside an ordinary literal",
			`'a\'b'`,
			`String('a\'b')`,
		},
		{
			"a hash comment",
			"# c\nx",
			"Comment(# c) Space(\n) Word(x)",
		},
		{
			"the null-safe equality beats <= and <",
			`a<=>b`,
			`Word(a) Operator(<=>) Word(b)`,
		},
		{
			"a block comment does not nest, so the first close ends it",
			`/* a /* b */ x`,
			`Comment(/* a /* b */) Space( ) Word(x)`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := render(lex(t, sql.MySQL(), c.src, false)); got != c.want {
				t.Errorf("\n src  %q\n got  %s\n want %s", c.src, got, c.want)
			}
		})
	}
}

// The number form is where a run's predicate could not reach: it needs to know
// what came before (a dot already seen) and what comes after (digits behind an
// exponent marker).
func TestNumber_WhereALiteralStops(t *testing.T) {
	for _, c := range []struct {
		name, src, want string
	}{
		{"an integer", `12`, `Number(12)`},
		{"a fraction", `1.5`, `Number(1.5)`},
		{"a leading dot opens one", `.5`, `Number(.5)`},
		{"a lone dot does not", `.`, `Punct(.)`},
		{"an exponent", `1e5`, `Number(1e5)`},
		{"a signed exponent", `1.5E-3`, `Number(1.5E-3)`},
		{
			"an exponent with no digits is not one",
			`1e`,
			`Number(1) Word(e)`,
		},
		{
			"nor is one whose next byte is not a digit",
			`1ex`,
			`Number(1) Word(ex)`,
		},
		{
			"a second dot belongs to the next number, not this one",
			`1.2.3`,
			`Number(1.2) Number(.3)`,
		},
		{
			"a number ends where the identifier begins",
			`1abc`,
			`Number(1) Word(abc)`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := render(lex(t, sql.PostgreSQL(), c.src, false)); got != c.want {
				t.Errorf("\n src  %q\n got  %s\n want %s", c.src, got, c.want)
			}
		})
	}
}

// The tag rule is position-aware, which is the only way one predicate rejects
// $1$ — where $1 is a parameter — and accepts $a1$.
func TestPostgresTag_RejectsALeadingDigitAndAcceptsATrailingOne(t *testing.T) {
	if sql.PostgresTag(0, '1') {
		t.Error("a leading digit was accepted; $1$ would become a literal")
	}
	if !sql.PostgresTag(1, '1') {
		t.Error("a trailing digit was rejected; $a1$ is a legal tag")
	}
	if !sql.PostgresTag(0, 'a') || !sql.PostgresTag(0, '_') {
		t.Error("a letter or underscore must open a tag")
	}

	if got := render(lex(t, sql.PostgreSQL(), `$a1$x$a1$`, false)); got != `String($a1$x$a1$)` {
		t.Errorf("$a1$ = %s, want one String", got)
	}
}

const realistic = `-- a report
SELECT "user".id, count(*) AS n, 1.5e-3
FROM "user" /* the /* nested */ table */
WHERE name = 'O''Brien' AND note = E'line\n'
  AND body = $tag$ raw $tag$ AND x <= 12;
`

// A dialect read as bytes and one byte at a time yields identical tokens, and
// concatenating them reproduces the source exactly — the two properties the
// whole layer rests on, exercised through a real dialect rather than a fixture.
func TestDialects_ByteAndStreamAgreeAndReproduceTheSource(t *testing.T) {
	for _, d := range []struct {
		name  string
		forms []parse.Form
	}{
		{"postgresql", sql.PostgreSQL()},
		{"mysql", sql.MySQL()},
	} {
		t.Run(d.name, func(t *testing.T) {
			src := realistic
			if d.name == "mysql" {
				src = strings.ReplaceAll(src, `$tag$ raw $tag$`, "`raw`")
			}

			asBytes := lex(t, d.forms, src, false)
			asStream := lex(t, d.forms, src, true)

			if len(asBytes) != len(asStream) {
				t.Fatalf("token counts differ: bytes %d, stream %d", len(asBytes), len(asStream))
			}
			for i := range asBytes {
				if asBytes[i] != asStream[i] {
					t.Errorf("token %d differs: bytes %+v, stream %+v", i, asBytes[i], asStream[i])
				}
			}

			var rebuilt strings.Builder
			for _, tk := range asBytes {
				rebuilt.WriteString(tk.text)
			}
			if rebuilt.String() != src {
				t.Errorf("concatenation != source:\n got %q\nwant %q", rebuilt.String(), src)
			}
		})
	}
}

// Every form of both dialects obeys the protocol at every split.
func TestDialects_ConformAtEverySplit(t *testing.T) {
	corpus := []string{
		"", "a", "ab1", "  ", "\n", "1", "1.5", ".5", "1e5", "1e", "1.2.3",
		"'x'", "'it''s'", "'unterminated", `"col"`, "`col`", "E'a\\'b'",
		"$$b$$", "$t$x$t$", "$1", "/*c*/", "/* a /* b */ c */", "--c\nx", "#c\nx",
		"<=", "<=>", "<>", "::", "->>", "/", "-", ";", "(", ".", "x",
	}
	for _, d := range []struct {
		name  string
		forms []parse.Form
	}{
		{"postgresql", sql.PostgreSQL()},
		{"mysql", sql.MySQL()},
	} {
		for i, f := range d.forms {
			t.Run(d.name+"/"+f.Kind().String()+"/"+itoa(i), func(t *testing.T) {
				parsetest.Form(t, f, corpus)
			})
		}
	}
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// The dialect names live HERE and nowhere below. The core's own files must not
// contain them: a core that cannot name a dialect cannot be closed to new ones,
// which is a stronger guarantee than a table of options and the reason this
// package exists at all.
//
// Scoped to the files this design added. The package also carries an older
// statement splitter that Johno asked to keep for now, and retiring it is its own
// piece of work.
func TestTheCoreNamesNoDialect(t *testing.T) {
	core := []string{
		"kind.go", "form.go", "forms.go", "runforms.go",
		"token.go", "source.go", "scan.go",
	}
	named := regexp.MustCompile(`(?i)\b(sql|postgres|postgresql|mysql|sqlite|oracle)\b`)

	for _, name := range core {
		path := filepath.Join("..", name)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		for n, line := range strings.Split(string(b), "\n") {
			if named.MatchString(line) {
				t.Errorf("%s:%d names a dialect: %s", name, n+1, strings.TrimSpace(line))
			}
		}
	}
}

// A form the core has never heard of is added as a VALUE, with nothing below it
// edited: a # comment, a [bracket] identifier and a ~tag~ body, none of which any
// dialect above declares.
func TestAFormTheLexerHasNeverSeenIsJustAValue(t *testing.T) {
	forms := append([]parse.Form{
		parse.LineComment("#"),
		parse.QuoteForm("[", "]", parse.QuoteOpts{Kind: parse.Ident}),
		parse.DelimitedForm('~', '~', parse.DelimitedOpts{TagByte: sql.PostgresTag, AllowEmpty: true}),
	}, sql.PostgreSQL()...)

	got := render(lex(t, forms, "# c\n[col] ~t~body~t~", false))
	want := "Comment(# c) Space(\n) Ident([col]) Space( ) String(~t~body~t~)"
	if got != want {
		t.Errorf("\n got  %s\n want %s", got, want)
	}
}

func TestValidate_AcceptsARealStatementAndNamesWhatIsLeftOpen(t *testing.T) {
	lx := parse.New(parse.WithForms(sql.PostgreSQL()...))
	if err := lx.Validate(context.Background(), strings.NewReader(realistic)); err != nil {
		t.Errorf("Validate of a well-formed statement = %v, want nil", err)
	}

	err := lx.Validate(context.Background(), strings.NewReader(`select 'oops`))
	var unterm *parse.UnterminatedError
	if !errors.As(err, &unterm) {
		t.Fatalf("Validate of an unterminated literal = %v, want *UnterminatedError", err)
	}
	if unterm.Kind != parse.String {
		t.Errorf("UnterminatedError.Kind = %v, want String", unterm.Kind)
	}

	// And an identifier left open says so as an identifier.
	err = lx.Validate(context.Background(), strings.NewReader(`select "oops`))
	if !errors.As(err, &unterm) {
		t.Fatalf("Validate of an unterminated identifier = %v, want *UnterminatedError", err)
	}
	if unterm.Kind != parse.Ident {
		t.Errorf("UnterminatedError.Kind = %v, want Ident — it reports what was left open", unterm.Kind)
	}
}

// PostgreSQL has a grammar for operator names, not a list, so a preset that
// enumerated the built-ins would split the ones it had not thought of and would
// never see a user-defined name at all.
func TestPostgresOperator_IsAGrammarNotAList(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"an @ earns its trailing sign", `a @- b`, `Word(a) Space( ) Operator(@-) Space( ) Word(b)`},
		{"question-pipe is one name", `a ?| b`, `Word(a) Space( ) Operator(?|) Space( ) Word(b)`},
		{"question-ampersand is one name", `a ?& b`, `Word(a) Space( ) Operator(?&) Space( ) Word(b)`},
		{"hash-minus is one name", `a #- b`, `Word(a) Space( ) Operator(#-) Space( ) Word(b)`},
		{"a backtick is an operator byte here", "a ` b", "Word(a) Space( ) Operator(`) Space( ) Word(b)"},
		{"a name nobody built in", `a @#&| b`, `Word(a) Space( ) Operator(@#&|) Space( ) Word(b)`},

		// The trailing-sign rule: without a special byte the sign is not part of
		// the name, which is what keeps arithmetic arithmetic.
		{"a sum of a negative is not an operator", `1+-2`, `Number(1) Operator(+) Operator(-) Number(2)`},
		// PostgreSQL's `--` is unconditional, unlike MySQL's — so this is a
		// comment, and the operator name simply may not run into it.
		{"a comment wins over a name that would contain it", `1--2`, `Number(1) Comment(--2)`},
		{"a special byte still cannot absorb a comment opener", `1@--2`, `Number(1) Operator(@) Comment(--2)`},
		{"but a special byte earns it", `1@-2`, `Number(1) Operator(@-) Number(2)`},

		// A name may not contain a comment opener.
		{"a name stops before a line comment", "1+-- c\n2", "Number(1) Operator(+) Comment(-- c) Space(\n) Number(2)"},
		{"and before a block comment", `1+/*c*/2`, `Number(1) Operator(+) Comment(/*c*/) Number(2)`},

		{"the familiar ones still work", `a<=b<>c::d`, `Word(a) Operator(<=) Word(b) Operator(<>) Word(c) Operator(::) Word(d)`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := render(lex(t, sql.PostgreSQL(), c.src, false)); got != c.want {
				t.Errorf("\n src  %q\n got  %s\n want %s", c.src, got, c.want)
			}
			// and identically one byte at a time, where the lookahead is real
			if got := render(lex(t, sql.PostgreSQL(), c.src, true)); got != c.want {
				t.Errorf("streamed:\n src  %q\n got  %s\n want %s", c.src, got, c.want)
			}
		})
	}
}

// MySQL's `--` needs whitespace or a control byte after it. Without that rule
// `balance--1` loses the rest of the line to a comment that is not there.
func TestMySQLDashComment_NeedsItsFollowingGap(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"no gap is subtraction, not a comment", `balance--1`, `Word(balance) Operator(-) Operator(-) Number(1)`},
		{"a space makes it a comment", "balance-- 1", "Word(balance) Comment(-- 1)"},
		{"so does a tab", "a--\tc", "Word(a) Comment(--\tc)"},
		{"so does a newline, which stays the next token's", "a--\nb", "Word(a) Comment(--) Space(\n) Word(b)"},
		{"at end of input there is no gap, so it is two operators", `a--`, `Word(a) Operator(-) Operator(-)`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := render(lex(t, sql.MySQL(), c.src, false)); got != c.want {
				t.Errorf("\n src  %q\n got  %s\n want %s", c.src, got, c.want)
			}
			if got := render(lex(t, sql.MySQL(), c.src, true)); got != c.want {
				t.Errorf("streamed:\n src  %q\n got  %s\n want %s", c.src, got, c.want)
			}
		})
	}
}

// An executable construct must not arrive as trivia, and refusing to match it is
// not the answer either: the closer still has to be checked. The opener is its
// own token, the body lexes as ordinary tokens, and the `*/` is required.
func TestMySQLExecutable_VisibleBodyAndAValidatedCloser(t *testing.T) {
	lx := parse.New(parse.WithForms(sql.MySQL()...))
	validate := func(src string) error {
		return lx.Validate(context.Background(), strings.NewReader(src))
	}

	// The dangerous words are ordinary tokens, not hidden inside trivia.
	for _, src := range []string{`/*!50000 DROP TABLE t */`, `/*+ MAX_EXECUTION_TIME(1000) */`} {
		got := render(lex(t, sql.MySQL(), src, false))
		if strings.Contains(got, "Comment(") {
			t.Errorf("%q arrived as trivia: %s", src, got)
		}
		if streamed := render(lex(t, sql.MySQL(), src, true)); streamed != got {
			t.Errorf("%q lexes differently streamed:\n got %s\nwant %s", src, streamed, got)
		}
	}
	if got := render(lex(t, sql.MySQL(), `/*!50000 DROP TABLE t */`, false)); !strings.Contains(got, "Word(DROP)") ||
		!strings.Contains(got, "Word(TABLE)") {
		t.Errorf("the executable body is not visible: %s", got)
	}
	if got := render(lex(t, sql.MySQL(), `/*+ MAX_EXECUTION_TIME(1000) */`, false)); !strings.Contains(got, "Word(MAX_EXECUTION_TIME)") {
		t.Errorf("the optimizer hint is not visible: %s", got)
	}

	// CLOSERS ARE VALIDATED — for the ordinary form and the executable one, and
	// for the bare opener that used to slip through as two operators.
	for _, c := range []struct {
		src  string
		want bool // want an unterminated report
	}{
		{`/*`, true},
		{`/* unterminated`, true},
		{`/*!50000 DROP TABLE t`, true},
		{`/*+ hint`, true},
		{`/* closed */`, false},
		{`/*! closed */`, false},
		{`/*+ closed */`, false},
	} {
		err := validate(c.src)
		var unterm *parse.UnterminatedError
		if got := errors.As(err, &unterm); got != c.want {
			t.Errorf("Validate(%q) = %v; want unterminated report: %v", c.src, err, c.want)
		}
	}

	// An ordinary block comment is still trivia, and still does not nest.
	if g := render(lex(t, sql.MySQL(), `/* a /* b */ x`, false)); g != `Comment(/* a /* b */) Space( ) Word(x)` {
		t.Errorf("ordinary block comment = %s", g)
	}
}
