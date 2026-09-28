package sqlite

import (
	"testing"

	"github.com/yongjohnlee80/golib/dao/internal/daotest"
)

func TestQueryCapabilitiesOnSQLite(t *testing.T) {
	daotest.QueryCapabilities(t, openMem(t), daotest.Engine{
		Table: func(name string) string {
			return "CREATE TABLE " + name + " (id INTEGER PRIMARY KEY, uri TEXT NOT NULL UNIQUE, name TEXT, n INTEGER NOT NULL, cap INTEGER NOT NULL)"
		},
	})
}
