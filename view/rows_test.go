package view

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

// fakeConn is a dao.DataConn whose queries return canned rows; it records the last query.
type fakeConn struct {
	dialect  string
	rows     []any // each row's single value: []byte, string, nil, or an error Scan returns
	cols     []string
	colsErr  error // returned by Columns instead of cols
	queryErr error
	iterErr  error // returned by Err once the rows run out
	closeErr error

	query  string
	args   []any
	opened *fakeRows
}

type fakeDialect struct {
	dao.GenericDialect
	name string
}

func (d fakeDialect) Name() string { return d.name }

func (c *fakeConn) QueryContext(_ context.Context, q string, args ...any) (dao.Rows, error) {
	c.query, c.args = q, args
	if c.queryErr != nil {
		return nil, c.queryErr
	}
	c.opened = &fakeRows{conn: c, at: -1}
	if c.cols != nil {
		return &fakeColumnRows{c.opened}, nil
	}
	return c.opened, nil
}

func (c *fakeConn) ExecContext(context.Context, string, ...any) (dao.Result, error) {
	return nil, dao.ErrUnsupported
}
func (c *fakeConn) Dialect() dao.Dialect                      { return fakeDialect{name: c.dialect} }
func (c *fakeConn) Begin(context.Context) (dao.TxConn, error) { return nil, dao.ErrUnsupported }
func (c *fakeConn) Name() string                              { return "fake" }
func (c *fakeConn) Close() error                              { return nil }

// fakeRows scans each value into the SAME buffer, as a driver that reuses its read buffer would,
// so a caller that kept the scanned slice would see it overwritten.
type fakeRows struct {
	conn   *fakeConn
	at     int
	buf    []byte
	closed int
}

func (r *fakeRows) Next() bool {
	if r.closed > 0 {
		return false
	}
	r.at++
	return r.at < len(r.conn.rows)
}

func (r *fakeRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("fake: one destination")
	}
	p := dest[0].(*[]byte)
	switch v := r.conn.rows[r.at].(type) {
	case error:
		return v
	case nil:
		*p = nil
	case string:
		r.buf = append(r.buf[:0], v...)
		*p = r.buf
	}
	return nil
}

func (r *fakeRows) Close() error { r.closed++; return r.conn.closeErr }

func (r *fakeRows) Err() error {
	if r.at >= len(r.conn.rows) {
		return r.conn.iterErr
	}
	return nil
}

type fakeColumnRows struct{ *fakeRows }

func (r *fakeColumnRows) Columns() ([]string, error) {
	if err := r.fakeRows.conn.colsErr; err != nil {
		return nil, err
	}
	return r.fakeRows.conn.cols, nil
}

const sqliteView = "---\nversion: 1\nname: v\nsource: sqlite\nargs: [a, b]\nprocess: SELECT x FROM t WHERE a = ? AND b = ?\nexport: \"{{.id}}.md\"\n---\n# {{.t}}\n"

func TestQuery_RunsTheProcessWithBoundArgs(t *testing.T) {
	t.Parallel()
	v := mustNew(t, sqliteView)
	conn := &fakeConn{dialect: "sqlite", rows: []any{`{"id":1,"t":"a"}`, `{"id":2,"t":"b"}`}}
	rows, err := v.WithArgs("x", []string{"y"}).Query(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if conn.query != v.Process() || len(conn.args) != 2 || conn.args[0] != "x" {
		t.Errorf("ran %q with %v", conn.query, conn.args)
	}
	first, err := rows.Next()
	if err != nil {
		t.Fatal(err)
	}
	second, err := rows.Next()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != `{"id":1,"t":"a"}` || string(second) != `{"id":2,"t":"b"}` {
		t.Errorf("rows = %s, %s: a row the caller kept was overwritten", first, second)
	}
	if _, err := rows.Next(); err != io.EOF {
		t.Errorf("after the last row: %v, want io.EOF", err)
	}
	if _, err := rows.Next(); err != io.EOF {
		t.Errorf("again after the last row: %v, want io.EOF", err)
	}
	if conn.opened.closed != 1 {
		t.Errorf("cursor closed %d times by the end of the stream, want 1", conn.opened.closed)
	}
	if err := rows.Close(); err != nil || conn.opened.closed != 1 {
		t.Errorf("Close after the end: %v, cursor closed %d times", err, conn.opened.closed)
	}
}

func TestQuery_ZeroArgs(t *testing.T) {
	t.Parallel()
	v := mustNew(t, "---\nversion: 1\nname: v\nsource: sqlite\nprocess: SELECT 1\n---\n")
	conn := &fakeConn{dialect: "sqlite"}
	rows, err := v.Query(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rows.Next(); err != io.EOF {
		t.Errorf("empty result: %v", err)
	}
}

func TestQuery_RefusesBeforeRunning(t *testing.T) {
	t.Parallel()
	v := mustNew(t, sqliteView)
	for name, tc := range map[string]struct {
		conn *fakeConn
		args []any
		want error
	}{
		"too few args":   {&fakeConn{dialect: "sqlite"}, []any{1}, ErrArgs},
		"too many args":  {&fakeConn{dialect: "sqlite"}, []any{1, 2, 3}, ErrArgs},
		"other dialect":  {&fakeConn{dialect: "postgres"}, []any{1, 2}, ErrSource},
		"two columns":    {&fakeConn{dialect: "sqlite", cols: []string{"a", "b"}}, []any{1, 2}, ErrRow},
		"zero columns":   {&fakeConn{dialect: "sqlite", cols: []string{}}, []any{1, 2}, ErrRow},
		"database error": {&fakeConn{dialect: "sqlite", queryErr: errors.New("boom")}, []any{1, 2}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := v.WithArgs(tc.args...).Query(context.Background(), tc.conn)
			if err == nil {
				t.Fatal("ran")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
			if errors.Is(tc.want, ErrArgs) || errors.Is(tc.want, ErrSource) {
				if tc.conn.query != "" {
					t.Error("the query ran before the refusal")
				}
			}
			if errors.Is(tc.want, ErrRow) && tc.conn.opened.closed != 1 {
				t.Errorf("the refused result's cursor closed %d times, want 1", tc.conn.opened.closed)
			}
		})
	}
}

func TestQuery_OneColumnIsAccepted(t *testing.T) {
	t.Parallel()
	v := mustNew(t, sqliteView)
	conn := &fakeConn{dialect: "sqlite", cols: []string{"doc"}, rows: []any{`{}`}}
	rows, err := v.WithArgs(1, 2).Query(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if x, err := rows.Next(); err != nil || string(x) != "{}" {
		t.Errorf("row = %s, %v", x, err)
	}
}

func TestRows_EndOnABadRow(t *testing.T) {
	t.Parallel()
	scanErr := errors.New("scan failed")
	for name, tc := range map[string]struct {
		row  any
		want error
		msg  string
	}{
		"NULL":         {nil, ErrRow, "row 2 is NULL"},
		"not JSON":     {`{"a":`, ErrRow, "row 2 is not JSON"},
		"scan error":   {scanErr, scanErr, "row 2"},
		"empty string": {"", ErrRow, "row 2 is not JSON"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := mustNew(t, sqliteView)
			conn := &fakeConn{dialect: "sqlite", rows: []any{`{}`, tc.row, `{}`}}
			rows, err := v.WithArgs(1, 2).Query(context.Background(), conn)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := rows.Next(); err != nil {
				t.Fatal(err)
			}
			_, err = rows.Next()
			if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("err = %v, want %v naming %q", err, tc.want, tc.msg)
			}
			if _, again := rows.Next(); again != err {
				t.Errorf("after the error: %v, want the same error", again)
			}
			if conn.opened.closed != 1 {
				t.Errorf("cursor closed %d times, want 1", conn.opened.closed)
			}
		})
	}
}

func TestRows_ReportTheCursorsErrors(t *testing.T) {
	t.Parallel()
	v := mustNew(t, sqliteView)
	iterErr := errors.New("connection lost")
	conn := &fakeConn{dialect: "sqlite", rows: []any{`{}`}, iterErr: iterErr}
	rows, _ := v.WithArgs(1, 2).Query(context.Background(), conn)
	rows.Next()
	if _, err := rows.Next(); !errors.Is(err, iterErr) {
		t.Errorf("err = %v, want the cursor's", err)
	}

	closeErr := errors.New("close failed")
	conn = &fakeConn{dialect: "sqlite", closeErr: closeErr}
	rows, _ = v.WithArgs(1, 2).Query(context.Background(), conn)
	if _, err := rows.Next(); !errors.Is(err, closeErr) {
		t.Errorf("err = %v, want the close error at the end", err)
	}
}

func TestRows_All(t *testing.T) {
	t.Parallel()
	v := mustNew(t, sqliteView)
	conn := &fakeConn{dialect: "sqlite", rows: []any{`1`, `2`, `3`}}
	rows, _ := v.WithArgs(1, 2).Query(context.Background(), conn)
	var got []string
	for x, err := range rows.All() {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(x))
	}
	if strings.Join(got, ",") != "1,2,3" {
		t.Errorf("got %v", got)
	}

	conn = &fakeConn{dialect: "sqlite", rows: []any{`1`, `2`, `3`}}
	rows, _ = v.WithArgs(1, 2).Query(context.Background(), conn)
	for range rows.All() {
		break
	}
	if conn.opened.closed != 1 {
		t.Errorf("breaking out of All closed the cursor %d times, want 1", conn.opened.closed)
	}

	conn = &fakeConn{dialect: "sqlite", rows: []any{`1`, nil, `3`}}
	rows, _ = v.WithArgs(1, 2).Query(context.Background(), conn)
	var errs, n int
	for _, err := range rows.All() {
		n++
		if err != nil {
			errs++
		}
	}
	if n != 2 || errs != 1 {
		t.Errorf("yielded %d values, %d errors; want 2 and 1, the error last", n, errs)
	}
}

// A driver that has column metadata but fails to report it is an error, and the cursor is
// released; only a driver without the capability skips the one-column check.
func TestQuery_ColumnMetadataErrors(t *testing.T) {
	t.Parallel()
	v := mustNew(t, sqliteView)
	conn := &fakeConn{dialect: "sqlite", cols: []string{"doc"}, colsErr: errors.New("metadata lost"), rows: []any{`{}`}}
	if _, err := v.WithArgs(1, 2).Query(context.Background(), conn); err == nil || !strings.Contains(err.Error(), "metadata lost") {
		t.Errorf("err = %v, want the metadata error", err)
	}
	if conn.opened.closed != 1 {
		t.Errorf("cursor closed %d times, want 1", conn.opened.closed)
	}

	conn = &fakeConn{dialect: "sqlite", cols: []string{"doc"}, colsErr: dao.ErrUnsupported, rows: []any{`{}`}}
	rows, err := v.WithArgs(1, 2).Query(context.Background(), conn)
	if err != nil {
		t.Fatalf("a driver without column names: %v", err)
	}
	if x, err := rows.Next(); err != nil || string(x) != "{}" {
		t.Errorf("row = %s, %v", x, err)
	}
}
