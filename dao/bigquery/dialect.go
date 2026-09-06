package bigquery

import (
	"strings"

	"github.com/yongjohnlee80/golib/dao"
)

// BigQueryDialect implements dao.Dialect for Google BigQuery — a read-mostly,
// no-transaction OLAP store (golib-dao). It embeds dao.GenericDialect
// and overrides the BigQuery specifics: backtick-quoted identifiers, "?"
// positional placeholders, and the no-transaction / no-upsert / no-RETURNING
// capability profile.
//
// What works through this dialect: Select/Get/Count/Exists/Iterate (reads) and
// Insert/Update/Delete via standard DML. What returns dao.ErrUnsupported:
// transactions (Begin/RunTx/On(tx)), Upsert (and batch conflict handling), and
// the explicit COPY fast-path.
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

// BigQuery implements NONE of dao's optional capabilities, and that absence is
// the whole of its profile. It is an append-mostly analytics store, so:
//
//   - No dao.Returner. BigQuery has no INSERT ... RETURNING.
//   - No dao.LastInsertIDReader. There is no server-generated insert id.
//     With neither, an insert runs the DML and reports a zero id and a nil
//     error; callers supply ids themselves.
//   - No dao.Upserter. There is no INSERT-suffix upsert, only MERGE, so an
//     upsert or batch conflict handling is refused with dao.ErrUnsupported
//     rather than degraded into a plain insert.
//   - No dao.Copier. Large ingests should use a load job; that fast path is
//     not wired to dao's bulk-copy seam.
//   - No dao.TwoPhaser. There are no prepared transactions.
//
// Interactive transactions are likewise absent — BigQuery has only limited
// in-job scripting — and the connection reports that from Begin when it is
// first touched, wrapping dao.ErrUnsupported.
//
// None of this is declared. There is nothing here to declare it WITH, which is
// the point: a dialect states what it can do, and says nothing about what it
// cannot.
// REFERENCE: dao/capabilities.go

// TranslateError passes the error through unchanged: BigQuery has no unique /
// foreign-key constraint SQLSTATEs to map to dao.ConstraintError sentinels.
func (BigQueryDialect) TranslateError(err error) error { return err }
