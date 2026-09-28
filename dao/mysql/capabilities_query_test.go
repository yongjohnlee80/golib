package mysql

import (
	"testing"

	"github.com/yongjohnlee80/golib/dao/internal/daotest"
)

// Skips when TEST_MYSQL_DSN is unset.
func TestQueryCapabilitiesOnMySQL(t *testing.T) {
	daotest.QueryCapabilities(t, testConn(t), daotest.Engine{
		Table: func(name string) string {
			return "CREATE TABLE " + name + " (id BIGINT AUTO_INCREMENT PRIMARY KEY, uri VARCHAR(64) NOT NULL UNIQUE, name VARCHAR(64), n BIGINT NOT NULL, cap BIGINT NOT NULL)"
		},
		Writers: true,
	})
}
