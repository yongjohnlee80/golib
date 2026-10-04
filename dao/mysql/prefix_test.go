package mysql

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/dao/internal/daotest"
)

func prefixTable(name string) string {
	return "CREATE TABLE " + name + " (id BIGINT PRIMARY KEY, path VARCHAR(255) NOT NULL)"
}

// The default sql_mode reads a backslash in a string literal as an escape;
// HasPrefix's '!' must not care. Skips when TEST_MYSQL_DSN is unset.
func TestHasPrefixOnMySQL(t *testing.T) {
	daotest.LiteralPrefix(t, testConn(t), prefixTable)
}

// The same cell with NO_BACKSLASH_ESCAPES set on every session of the pool,
// through the DSN, and the same answers.
func TestHasPrefixOnMySQLNoBackslashEscapes(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set; skipping mysql integration test")
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := Open(ctx, dsn+sep+"sql_mode=%27NO_BACKSLASH_ESCAPES%27")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	requireMode(t, conn, "NO_BACKSLASH_ESCAPES")
	daotest.LiteralPrefix(t, conn, prefixTable)
}

// requireMode fails unless the session's sql_mode holds mode, so the cell
// cannot pass while running in the default mode.
func requireMode(t *testing.T, conn dao.DataConn, mode string) {
	t.Helper()
	rows, err := conn.QueryContext(context.Background(), "SELECT @@SESSION.sql_mode")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got string
	if rows.Next() {
		if err := rows.Scan(&got); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(got, mode) {
		t.Fatalf("session sql_mode is %q, want it to hold %s", got, mode)
	}
}
