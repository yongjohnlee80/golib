package postgres

import (
	"testing"

	"github.com/yongjohnlee80/golib/dao/internal/daotest"
)

// Skips when TEST_PGURL is unset.
func TestHasPrefixOnPostgres(t *testing.T) {
	daotest.LiteralPrefix(t, testConn(t), func(name string) string {
		return "CREATE TABLE " + name + " (id BIGINT PRIMARY KEY, path TEXT NOT NULL)"
	})
}
