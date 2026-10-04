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
