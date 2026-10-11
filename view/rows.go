package view

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"

	"github.com/yongjohnlee80/golib/dao"
)

var (
	// ErrArgs is returned when an invocation's arguments do not match the view's declared args.
	ErrArgs = errors.New("view: arguments do not match the declared args")

	// ErrSource is returned when a view runs on a connection whose dialect is not its declared source.
	ErrSource = errors.New("view: connection dialect is not the view's source")

	// ErrRow is wrapped by every error about the shape of a query result: more or fewer than one
	// column, a NULL, or text that is not one JSON value.
	ErrRow = errors.New("view: process must return one JSON column")
)

// Invocation binds a [View] to a specific sequence of query arguments, preparing it for execution.
// It is an immutable value: binding does not mutate the underlying [View], and a single Invocation
// can be executed multiple times concurrently across distinct goroutines and database connections.
type Invocation struct {
	view *View
	args []any
}

// WithArgs binds values to the view's declared args list in positional order.
//
// Arguments are passed directly to the database driver as bound parameters ($1, $2, … or ?, ?),
// ensuring values never enter the SQL query text. For drivers supporting array types (such as
// PostgreSQL "id = ANY($1)"), a Go slice binds as a typed SQL array.
func (v *View) WithArgs(args ...any) Invocation {
	return Invocation{view: v, args: args}
}

// Query executes the view's query on conn with no arguments.
// It fails if the view declared one or more args, or if the connection dialect does not match
// the view's declared source.
func (v *View) Query(ctx context.Context, conn dao.DataConn) (*Rows, error) {
	return v.WithArgs().Query(ctx, conn)
}

// Query executes the bound view on conn.
//
// Pre-execution validations:
//   - Argument Count: The number of bound arguments must match len(v.Args()).
//   - Dialect Verification: conn.Dialect().Name() must match v.Source().
//   - Column Cardinality: The SQL query must return exactly one column (the JSON value).
//
// The returned [Rows] cursor must be closed by the caller when iteration completes.
func (inv Invocation) Query(ctx context.Context, conn dao.DataConn) (*Rows, error) {
	v := inv.view
	if len(inv.args) != len(v.args) {
		return nil, fmt.Errorf("%w: %s declares %d (%v), have %d", ErrArgs, v.name, len(v.args), v.args, len(inv.args))
	}
	if got := conn.Dialect().Name(); got != v.source {
		return nil, fmt.Errorf("%w: %s is written for %s, the connection is %s", ErrSource, v.name, v.source, got)
	}
	rows, err := conn.QueryContext(ctx, v.process, inv.args...)
	if err != nil {
		return nil, fmt.Errorf("view %s: %w", v.name, err)
	}
	cols, err := dao.Columns(rows)
	switch {
	case errors.Is(err, dao.ErrUnsupported):
		// The driver cannot name its result columns; a result of other than one column then fails
		// at the first row's Scan instead.
	case err != nil:
		_ = rows.Close()
		return nil, fmt.Errorf("view %s: result columns: %w", v.name, err)
	case len(cols) != 1:
		_ = rows.Close()
		return nil, fmt.Errorf("%w: %s returns %d columns %v", ErrRow, v.name, len(cols), cols)
	}
	return &Rows{view: v, rows: rows}, nil
}

// Rows provides a forward-only streaming iterator over the database query results, delivering
// one JSON byte slice per row.
//
// Memory Management:
// Results are read sequentially one row at a time directly from the database driver cursor.
// Large result sets spanning millions of rows are processed without buffering the entire dataset in memory.
//
// Rows is not safe for concurrent use by multiple goroutines.
type Rows struct {
	view *View
	rows dao.Rows
	n    int
	done bool
	err  error
}

// Next advances the cursor and returns the JSON payload for the next row, returning [io.EOF]
// after the final row has been processed.
//
// Ownership and Invariants:
// The returned []byte is cloned from the driver's internal buffer, giving the caller full
// ownership of the memory. If the database returns NULL or malformed JSON, Next terminates the
// cursor, closes the underlying connection, and returns an error wrapping [ErrRow].
func (r *Rows) Next() ([]byte, error) {
	if r.done {
		return nil, r.end()
	}
	if !r.rows.Next() {
		r.finish(r.rows.Err())
		return nil, r.end()
	}
	r.n++
	var x []byte
	if err := r.rows.Scan(&x); err != nil {
		r.finish(fmt.Errorf("view %s: row %d: %w", r.view.name, r.n, err))
		return nil, r.err
	}
	switch {
	case x == nil:
		r.finish(fmt.Errorf("%w: %s: row %d is NULL", ErrRow, r.view.name, r.n))
		return nil, r.err
	case !json.Valid(x):
		r.finish(fmt.Errorf("%w: %s: row %d is not JSON", ErrRow, r.view.name, r.n))
		return nil, r.err
	}
	// Drivers differ on whether a scanned []byte aliases their read buffer; a clone makes the
	// caller's ownership true for every one of them.
	return bytes.Clone(x), nil
}

// All returns an iterator over the remaining rows in the stream suitable for for-range loops.
//
// Iteration Invariants:
//   - Yields each row's JSON byte slice and any encountered read or unmarshaling error.
//   - Stops iteration automatically after the first non-nil error, yielding that error to the caller.
//   - Ensures the underlying [dao.Rows] cursor is closed when the loop terminates, whether
//     exhausted naturally, aborted via error, or exited early via break or return.
func (r *Rows) All() iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		defer r.Close()
		for {
			x, err := r.Next()
			if err == io.EOF {
				return
			}
			if !yield(x, err) || err != nil {
				return
			}
		}
	}
}

// Close releases the underlying database cursor and any associated network/connection resources.
// It is idempotent and safe to call multiple times or after the stream has already reached [io.EOF].
func (r *Rows) Close() error {
	if r.done {
		return nil
	}
	r.done = true
	return r.rows.Close()
}

func (r *Rows) finish(err error) {
	r.done = true
	if cerr := r.rows.Close(); err == nil {
		err = cerr
	}
	r.err = err
}

func (r *Rows) end() error {
	if r.err != nil {
		return r.err
	}
	return io.EOF
}
