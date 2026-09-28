package postgres

import "testing"

// What dao/deploy asks of Postgres, without a server: DDL rolls back with
// the transaction, and the lock is a transaction-level advisory lock, which
// commit and rollback release by themselves.
func TestPostgresDeployCapabilities(t *testing.T) {
	d := PostgresDialect{}
	if !d.TransactionalDDL() {
		t.Error("TransactionalDDL = false; Postgres rolls DDL back with the transaction")
	}
	if got := d.DeployLock(); got != "SELECT pg_advisory_xact_lock(7234985120937465)" {
		t.Errorf("DeployLock = %q: a changed key would let an old and a new runner apply at once", got)
	}
}
