package js

// Option configures an [Expression] or [Statements] parser built by [NewExpression] or
// [NewStatements].
type Option func(*options)

type options struct {
	dialect  *ExprDialect
	maxDepth int
}

func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// WithDialect selects the language. Without it the parser reads [JavaScript].
func WithDialect(d *ExprDialect) Option { return func(o *options) { o.dialect = d } }

// MaxDepth bounds nesting. Without it, or with n <= 0, the parser applies
// [DefaultExprMaxDepth].
func MaxDepth(n int) Option { return func(o *options) { o.maxDepth = n } }

// NewExpression returns an expression parser configured by opts.
func NewExpression(opts ...Option) Expression {
	o := newOptions(opts)
	return Expression{Dialect: o.dialect, MaxDepth: o.maxDepth}
}

// NewStatements returns a statement parser configured by opts.
func NewStatements(opts ...Option) Statements {
	o := newOptions(opts)
	return Statements{Dialect: o.dialect, MaxDepth: o.maxDepth}
}
