-- Integration-test schema for golib/dao + dao/postgres, applied against the
-- `golib` database. sql-migrate compatible: run with
--   sql-migrate up   -config=dbconfig.yml -env=golib
-- or let the two-process harness apply it programmatically (see
-- postgres_twoproc_integration_test.go, which parses these +migrate sections).
--
-- All objects are namespaced `golib_dao_*` so a real `golib` database is never
-- otherwise touched; the harness drops them again on teardown.

-- +migrate Up

-- The primary table the scaffold uses: a unique name drives duplicate/upsert
-- and concurrency tests; serial id exercises RETURNING.
CREATE TABLE golib_dao_widget (
    id   serial PRIMARY KEY,
    name text UNIQUE NOT NULL,
    qty  int  NOT NULL DEFAULT 0
);

-- A child table related to widget, for on-demand joins and cross-table /
-- cross-instance transactions. UNIQUE (widget_id, label) lets a transaction be
-- forced to roll back by inserting a duplicate label within one widget.
CREATE TABLE golib_dao_part (
    id        serial PRIMARY KEY,
    widget_id int  NOT NULL REFERENCES golib_dao_widget (id) ON DELETE CASCADE,
    label     text NOT NULL,
    UNIQUE (widget_id, label)
);

-- +migrate Down

DROP TABLE IF EXISTS golib_dao_part;
DROP TABLE IF EXISTS golib_dao_widget;
