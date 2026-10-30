package postgres

import (
	"strings"

	"github.com/yongjohnlee80/golib/dao"
)

var _ dao.ArrayKeyer = PostgresDialect{}

// KeysInSQL implements dao.ArrayKeyer: (c1, c2) IN (SELECT * FROM unnest($1::bigint[],
// $2::integer[])), one bound array per column, however many keys.
func (PostgresDialect) KeysInSQL(cols []string, elems []dao.ArrayElem, placeholders []string) string {
	arrays := make([]string, len(placeholders))
	for i, ph := range placeholders {
		arrays[i] = ph + "::" + arrayType(elems[i])
	}
	return "(" + strings.Join(cols, ", ") + ") IN (SELECT * FROM unnest(" + strings.Join(arrays, ", ") + "))"
}

// arrayType is PostgreSQL's array type for e.
func arrayType(e dao.ArrayElem) string {
	switch e {
	case dao.ArrayInt32:
		return "integer[]"
	case dao.ArrayText:
		return "text[]"
	}
	return "bigint[]"
}
