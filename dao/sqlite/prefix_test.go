package sqlite

import (
	"testing"

	"github.com/yongjohnlee80/golib/dao/internal/daotest"
)

// SQLite has no default LIKE escape, so this is where an ESCAPE clause that
// went missing shows.
func TestHasPrefixOnSQLite(t *testing.T) {
	daotest.LiteralPrefix(t, openMem(t), func(name string) string {
		return "CREATE TABLE " + name + " (id INTEGER PRIMARY KEY, path TEXT NOT NULL)"
	})
}
