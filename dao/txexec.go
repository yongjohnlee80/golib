package dao

import (
	"github.com/yongjohnlee80/golib/dao/internal/txexec"
	"github.com/yongjohnlee80/golib/errs"
)

// The migration runner's seam (dao/deploy): it reaches a transaction's
// executor through txexec, never through an exported method, so products
// cannot run statements of their own inside a dao transaction.
func init() {
	txexec.Join = func(tx, conn any) (any, error) {
		t, okT := tx.(*Transaction)
		c, okC := conn.(DataConn)
		if !okT || !okC || t == nil || c == nil {
			return nil, errs.Wrap(errs.ErrInvalidArgument, "dao: txexec.Join takes a *dao.Transaction and a dao.DataConn, not %T and %T", tx, conn)
		}
		return t.join(c)
	}
}
