package js

import "github.com/yongjohnlee80/golib/parse"

// Driver parses JavaScript over a scanner the CALLER owns.
//
// It exists because an embedding format hands the cursor over and takes it
// back: QML puts JavaScript after `onClicked:` and in every property value, so
// it needs the JS grammar to start where its own cursor is and to leave the
// cursor where the JS ended.
//
// The DEPTH BUDGET is shared for the same reason. What has to stay bounded is
// the recursion, and the recursion alternates between the two grammars — a
// budget per grammar would bound nodes and leave the JavaScript inside them
// unbounded, in the same parse of the same file. A driver is therefore given
// what the document has already SPENT, not a fresh counter.
//
// Nothing is carried back when a parse succeeds: enter and leave are balanced,
// so a driver that returns has left the counter where it found it.
type Driver struct {
	x *exprParser
}

// NewDriver builds a driver over sc, bounded by maxDepth and starting from the
// depth the caller has already spent.
//
// A nil dialect means [JavaScript]. maxDepth of zero or less means
// [DefaultExprMaxDepth].
func NewDriver(sc *parse.Scanner, d *ExprDialect, maxDepth, spent int) *Driver {
	if d == nil {
		d = &JavaScript
	}
	if maxDepth <= 0 {
		maxDepth = DefaultExprMaxDepth
	}
	return &Driver{x: &exprParser{sc: sc, d: d, max: maxDepth, depth: spent}}
}

// Expression parses one expression and stops. Whatever follows it is the
// caller's to read.
func (dr *Driver) Expression() (Expr, error) { return dr.x.expression() }

// Statement parses one statement and stops.
//
// A BLOCK is returned as the block, not as its contents: an embedding format
// decides for itself whether `{ … }` after a colon is a body or a single
// statement, and flattening it here would take that decision away from the one
// place that knows the answer.
func (dr *Driver) Statement() (Stmt, error) {
	return (&stmtParser{sc: dr.x.sc, x: dr.x}).statement()
}

// Depth reports the recursion depth the driver has reached, for a caller that
// wants to charge its own budget for what the embedded grammar spent.
func (dr *Driver) Depth() int { return dr.x.depth }
