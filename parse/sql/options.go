package sql

// Option configures a [SQL] built by [New].
type Option func(*SQL)

// New returns a SQL splitter configured by opts. With no options it handles the lexical syntax
// common to the major engines; each option adds one engine's extension.
//
//	pg := sql.New(sql.DollarQuotes(), sql.NestedBlockComments(), sql.EStringEscapes())
//	stmts, err := pg.Parse(src)
func New(opts ...Option) SQL {
	var s SQL
	for _, o := range opts {
		if o != nil {
			o(&s)
		}
	}
	return s
}

// Backticks treats `like this` as a quoted identifier, as MySQL does.
func Backticks() Option { return func(s *SQL) { s.Backticks = true } }

// DollarQuotes treats $$…$$ and $tag$…$tag$ as string literals, as PostgreSQL does for function
// bodies, so the semicolons inside a function body do not split it.
func DollarQuotes() Option { return func(s *SQL) { s.DollarQuotes = true } }

// NestedBlockComments lets /* … /* … */ … */ nest, as PostgreSQL does.
func NestedBlockComments() Option { return func(s *SQL) { s.NestedBlockComments = true } }

// EStringEscapes treats a string written E'…' as one in which a backslash escapes the next
// character, as PostgreSQL does. Only strings carrying the E prefix change.
func EStringEscapes() Option { return func(s *SQL) { s.EStringEscapes = true } }

// TriggerBodies reads CREATE [TEMP] TRIGGER … BEGIN … END as one statement, as SQLite does.
func TriggerBodies() Option { return func(s *SQL) { s.TriggerBodies = true } }

// HashComments reads # to the end of the line as a comment, as MySQL does.
func HashComments() Option { return func(s *SQL) { s.hashComments = true } }

// MySQLDashComments reads -- as a comment only when a space, a tab, a line break, a vertical tab or
// a form feed follows it, or nothing does, as MySQL does: balance--1 is a subtraction, and the
// text after it is part of the statement.
func MySQLDashComments() Option { return func(s *SQL) { s.mysqlDash = true } }

// BackslashEscapes lets a backslash escape the next character in every '…' and "…" string, as
// MySQL does by default, so 'it\'s' is one string. Backticked names are not affected.
func BackslashEscapes() Option { return func(s *SQL) { s.backslash = true } }

// ExecutableComments reads the body of a /*! … */ or /*!NNNNN … */ comment as statement text, as
// MySQL does, which runs it: a ; inside the body ends a statement. Only the opener, its version
// digits and the closing */ are passed over. A /*+ … */ optimizer hint stays a comment.
func ExecutableComments() Option { return func(s *SQL) { s.executable = true } }

// BracketIdentifiers reads [like this] as a quoted identifier, as SQLite does: the name runs to the
// FIRST ], with no escape inside it, so a quote, a comment opener or a ; between the brackets is a
// byte of the name. Read as a lone [, SELECT 1 AS [a'b]; DROP TABLE t; --' is one statement whose
// quote opens a string to the end of the text, where SQLite runs two. An unclosed [ is unterminated.
func BracketIdentifiers() Option { return func(s *SQL) { s.brackets = true } }

// TclParameters reads $name, @name, :name and #name as named parameters, as SQLite's tokenizer
// does. A name is letters, digits, _, $ and bytes past ASCII, with :: allowed inside it, so $$x is
// the parameter $$x, never a dollar quote. After at least one name character an optional (…) suffix
// runs to the FIRST ), and anything but whitespace inside it is a byte of the parameter: the quote in
// SELECT $a(x'y); DROP TABLE t; --' opens no string, and SQLite runs two statements there.
//
// A prefix with no name after it (# a comment, x::text, @>) and a suffix cut off by whitespace or
// the end of the text are tokens SQLite refuses, and so is the script: # is never a comment here. A
// word is read whole, so the $ in a$b is a byte of the word; a digit is a byte of its own, so the $
// in 1$a opens a parameter.
func TclParameters() Option { return func(s *SQL) { s.tclParams = true } }

// PostgreSQLSplitter splits as PostgreSQL reads a script: dollar-quoted bodies, nested block
// comments and E'…' escapes.
func PostgreSQLSplitter() SQL { return New(DollarQuotes(), NestedBlockComments(), EStringEscapes()) }

// MySQLSplitter splits as MySQL reads a script: backticked names, # comments, -- before a space,
// backslash escapes in strings, and live /*! … */ bodies.
func MySQLSplitter() SQL {
	return New(Backticks(), HashComments(), MySQLDashComments(), BackslashEscapes(), ExecutableComments())
}

// SQLiteSplitter splits as SQLite reads a script: a CREATE TRIGGER … BEGIN … END is one statement,
// `…` and […] are quoted names, and $name, @name, :name and #name are parameters.
func SQLiteSplitter() SQL {
	return New(TriggerBodies(), Backticks(), BracketIdentifiers(), TclParameters())
}
