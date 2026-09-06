package dao

// joinClause is a rendered join (e.g. "LEFT JOIN label_group ON ..."), stored on
// a [Schema] by [JoinKey] and selected into a query only when a column, sort, or
// forced trigger fires. Every join is optional and demand-driven;
// "always join" is expressed by putting the key on a default-selected field or by
// forcing it with DAO.Join.
type joinClause struct {
	key JoinKey
	sql string
}
