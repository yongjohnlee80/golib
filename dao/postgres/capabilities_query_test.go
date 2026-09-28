package postgres

import (
	"testing"

	"github.com/yongjohnlee80/golib/dao/internal/daotest"
)

// Skips when TEST_PGURL is unset.
func TestQueryCapabilitiesOnPostgres(t *testing.T) {
	daotest.QueryCapabilities(t, testConn(t), daotest.Engine{
		Table: func(name string) string {
			return "CREATE TABLE " + name + " (id BIGSERIAL PRIMARY KEY, uri TEXT NOT NULL UNIQUE, name TEXT, n BIGINT NOT NULL, cap BIGINT NOT NULL)"
		},
		Writers: true,
	})
}
