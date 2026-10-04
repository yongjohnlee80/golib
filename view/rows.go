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

// Invocation is a view bound to its arguments, ready to run. It is a value: binding never changes the
// View, and one Invocation may run any number of times, concurrently.
type Invocation struct {
	view *View
	args []any
}

// WithArgs binds the view's declared args, in order, as the query's parameters. The values go to the
// driver as bound parameters and never into the query text, so they may come from anywhere. A Go
// slice binds as an array where the driver supports one, as postgres does for "id = ANY($1)", which
// lets one view serve a whole catalog by a group's id and a single entity by its own.
func (v *View) WithArgs(args ...any) Invocation {
	return Invocation{view: v, args: args}
}

// Query runs the view's process on conn and returns its rows. It refuses a conn whose dialect is not
// the view's source, and an argument count other than the declared one, before running anything.
func (v *View) Query(ctx context.Context, conn dao.DataConn) (*Rows, error) {
	return v.WithArgs().Query(ctx, conn)
}

// Query runs the bound view on conn. See [View.Query].
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

// Rows streams a query's result, one JSON value per row. The database cursor is read a row at a
// time, so a result of any size is never held in memory. Rows is not safe for concurrent use.
type Rows struct {
	view *View
	rows dao.Rows
	n    int
	done bool
	err  error
}

// Next returns the next row's JSON, or io.EOF after the last. The slice is the caller's to keep. A
// NULL or a value that is not JSON is an error wrapping [ErrRow]; any error ends the stream, and
// the cursor is released when it ends.
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

// All ranges over the remaining rows. It stops after the first error, which it yields, and closes the
// rows when the loop ends, even early.
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

// Close releases the cursor. It is safe to call more than once, and after the stream has ended.
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
