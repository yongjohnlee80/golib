package bigquery

import (
	"strings"

	"github.com/yongjohnlee80/golib/dao"
)

// BigQueryDialect implements dao.Dialect for Google BigQuery — a read-mostly,
// no-transaction OLAP store (golib-dao ADR-0008). It embeds dao.GenericDialect
// and overrides the BigQuery specifics: backtick-quoted identifiers, "?"
// positional placeholders, and the no-transaction / no-upsert / no-RETURNING
// capability profile.
//
// What works through this dialect: Select/Get/Count/Exists/Iterate (reads) and
// Insert/Update/Delete via standard DML. What returns dao.ErrUnsupported:
// transactions (Begin/RunTx/On(tx)), Upsert (and batch conflict handling), and
// the explicit COPY fast-path. See ADR-0008 §3.
type BigQueryDialect struct {
	dao.GenericDialect
}

// Name returns "bigquery".
func (BigQueryDialect) Name() string { return dao.DialectBigQuery }

// Placeholder renders "?" — BigQuery uses positional query parameters. The driver
// maps each positional arg to a bigquery.QueryParameter in order.
func (BigQueryDialect) Placeholder(int) string { return "?" }

// QuoteIdent wraps an identifier (column, or "dataset.table" path) in backticks,
// stripping any embedded backtick — BigQuery's identifier quoting. With a default
// dataset configured on the connection, a bare table name resolves against it.
func (BigQueryDialect) QuoteIdent(ident string) string {
	return "`" + strings.ReplaceAll(ident, "`", "") + "`"
}

// MaxBindParams returns a conservative ceiling for positional query parameters,
// driving batch chunking. BigQuery is not built for high-frequency small DML;
// large ingests should prefer a load job (a future COPY-equivalent fast-path).
func (BigQueryDialect) MaxBindParams() int { return 10000 }

// SupportsReturning reports false: BigQuery has no INSERT ... RETURNING.

// SupportsTransactions reports false: BigQuery has no interactive pooled
// transactions (only limited in-job scripting), so Begin/RunTx/On(tx) return
// dao.ErrUnsupported on first touch (ADR-0008 §2.3).

// SupportsUpsert reports false: BigQuery has no ON CONFLICT INSERT suffix (only
// MERGE), so DAO.Upsert and batch conflict handling return dao.ErrUnsupported
// (ADR-0008 §2.4).

// SupportsLastInsertID reports false: BigQuery has no server-generated insert id.
// With no RETURNING and no LastInsertID, DAO.Insert runs the DML and returns the
// zero id with a nil error — callers supply ids client-side (ADR-0008 §2.6).

// BuildUpsertSuffix returns "" defensively: upsert is capability-gated off before
// this is reached, and BigQuery has no INSERT-suffix upsert to render.

// TranslateError passes the error through unchanged: BigQuery has no unique /
// foreign-key constraint SQLSTATEs to map to dao.ConstraintError sentinels.
func (BigQueryDialect) TranslateError(err error) error { return err }
