package mysql

import "github.com/yongjohnlee80/golib/dao"

// MySQL cannot RETURNING, so it does NOT implement dao.Returner. It reports
// the generated id through the result of the INSERT instead, which is what
// dao.LastInsertIDReader names.
//
// BuildUpsertSuffix is already declared on this dialect (ON DUPLICATE KEY
// UPDATE), so dao.Upserter is satisfied without anything added here — and that
// divergence from the standard form is the reason the capability carries the
// operation rather than a boolean: a flag could not have expressed it.

// LastInsertID implements dao.LastInsertIDReader.
func (MysqlDialect) LastInsertID(res dao.Result) (int64, error) {
	return dao.ResultLastInsertID(res)
}
