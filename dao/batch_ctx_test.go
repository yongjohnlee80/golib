package dao

import (
	"context"
	"testing"
)

// ctxExecConn records the ctx value seen by the driver on Exec.
type ctxExecConn struct {
	fakeConn
	sawDriver bool
}

func (c *ctxExecConn) ExecContext(ctx context.Context, q string, args ...any) (Result, error) {
	if ctx.Value(ctxKey{}) == "yes" {
		c.sawDriver = true
	}
	return c.fakeConn.ExecContext(ctx, q, args...)
}

// Batch flushes must honor WithQueryContext like every other statement
// (ADR-0009 §2.3); before the dao-m1 fix the writer executed on
// context.Background regardless.
func TestBatch_FlushHonorsQueryContext(t *testing.T) {
	t.Parallel()
	conn := &ctxExecConn{fakeConn: fakeConn{d: returningDialect{}}}
	s := buildSchema(conn)

	ctx := context.WithValue(context.Background(), ctxKey{}, "yes")
	b := s.DAO(WithQueryContext(ctx)).Batch()
	b.Add(map[artistField]any{aName: "Alpha"})
	if err := b.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !conn.sawDriver {
		t.Error("explicit query context did not reach the batch driver exec")
	}
}
