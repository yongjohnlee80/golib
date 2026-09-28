// Package txexec is the seam through which golib's own migration runner
// (dao/deploy) runs statements inside a dao transaction. It imports nothing of
// golib, so dao can install the hook without an import cycle, and Go's
// internal-package rule keeps it away from every package outside dao/.
package txexec

// Join joins conn to tx and returns the transaction's executor for conn: the
// same one the transaction's own statements use. tx is a *dao.Transaction and
// conn a dao.DataConn; any other value is refused. dao installs it.
var Join func(tx, conn any) (any, error)
