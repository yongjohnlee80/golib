package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

// The index lifecycle: closed operations over a declared index, for a consumer that builds and
// retires indexes while it runs (AutoDoc's per-model HNSW indexes). golib renders, quotes and
// allowlists every statement; nothing here takes SQL text from its caller.

// identRule is the rule every identifier the lifecycle renders follows: a lowercase PostgreSQL
// identifier of at most 63 bytes, which needs no case folding and quotes as itself.
var identRule = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// tokenRule is MarkSession's token: what the consumer records to know its own builder by.
var tokenRule = regexp.MustCompile(`^[a-z0-9:_-]{1,40}$`)

// lifecycleTag prefixes the application_name of every session MarkSession marks: only such a
// session is alive, cancelled or terminated through this API.
const lifecycleTag = "golib-index-lifecycle/"

// indexMethods is the allowlist of access methods an IndexSpec builds with.
var indexMethods = map[string]bool{"hnsw": true, "btree": true}

func checkIdent(what, s string) error {
	if !identRule.MatchString(s) {
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: %s %q is not a lowercase identifier of at most 63 bytes", what, s)
	}
	return nil
}

// checkTable checks a table, optionally schema-qualified, part by part.
func checkTable(table string) error {
	parts := strings.Split(table, ".")
	if len(parts) > 2 {
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: table %q has more than a schema and a name", table)
	}
	for _, p := range parts {
		if err := checkIdent("table", p); err != nil {
			return err
		}
	}
	return nil
}

// IndexElement is an index's element list, built only by HNSWElement or ColumnElement, so no
// caller's text reaches the DDL.
type IndexElement struct{ sql string }

// String is the element list as the DDL renders it.
func (e IndexElement) String() string { return e.sql }

// ColumnElement is a btree index's element list over one column: ("col").
func ColumnElement(col string) (IndexElement, error) {
	if err := checkIdent("column", col); err != nil {
		return IndexElement{}, err
	}
	return IndexElement{"(" + quoteIdent(col) + ")"}, nil
}

// HNSWIndexElement is HNSWElement as an IndexSpec's element: the column, a lowercase identifier,
// cast to t, with m's operator class.
func HNSWIndexElement(col string, m dao.Metric, t dao.VectorType) (IndexElement, error) {
	if err := checkIdent("column", col); err != nil {
		return IndexElement{}, err
	}
	el, err := HNSWElement(quoteIdent(col), m, t)
	if err != nil {
		return IndexElement{}, err
	}
	return IndexElement{el}, nil
}

// EqText is a partial index predicate's term: column = value, the value rendered as an
// escape-string literal (escapeLiteral), whatever standard_conforming_strings is.
type EqText struct{ Column, Value string }

// IndexSpec is one index, declared.
type IndexSpec struct {
	Table   string       // the table, optionally schema-qualified: lowercase identifiers
	Name    string       // the index's name: a lowercase identifier of at most 63 bytes
	Method  string       // "hnsw" or "btree"
	Element IndexElement // from HNSWIndexElement or ColumnElement
	WhereEq []EqText     // the partial predicate: each column = a text literal, ANDed
	// M and EfConstruction are an hnsw index's build parameters (pgvector's m, 2 to 100, and
	// ef_construction, 4 to 1000 and at least twice m), written WITH (…). Zero is pgvector's
	// default (16 and 64) and writes nothing, so a spec that sets neither builds as before.
	M, EfConstruction int
}

// pgvector's HNSW build parameter defaults and bounds.
const (
	hnswDefaultM = 16
	hnswMaxM     = 100
	hnswMinEfC   = 4
	hnswMaxEfC   = 1000
)

func (s IndexSpec) check() error {
	if err := checkTable(s.Table); err != nil {
		return err
	}
	if err := checkIdent("index name", s.Name); err != nil {
		return err
	}
	if !indexMethods[s.Method] {
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: index method %q is not hnsw or btree", s.Method)
	}
	if s.Element.sql == "" {
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: index %s has no element (HNSWIndexElement, ColumnElement)", s.Name)
	}
	for _, w := range s.WhereEq {
		if err := checkIdent("predicate column", w.Column); err != nil {
			return err
		}
		if _, err := escapeLiteral(w.Value); err != nil {
			return err
		}
	}
	return s.checkHNSW()
}

// checkHNSW refuses build parameters on another method, or outside pgvector's bounds: m 2 to 100,
// ef_construction 4 to 1000 and at least twice m (the default 16 when m is unset).
func (s IndexSpec) checkHNSW() error {
	if s.M == 0 && s.EfConstruction == 0 {
		return nil
	}
	switch m := cmpOr(s.M, hnswDefaultM); {
	case s.Method != "hnsw":
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: index %s: m and ef_construction are hnsw's, not %s's", s.Name, s.Method)
	case s.M != 0 && (s.M < 2 || s.M > hnswMaxM):
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: index %s: m %d, want 2 to %d", s.Name, s.M, hnswMaxM)
	case s.EfConstruction != 0 && (s.EfConstruction < hnswMinEfC || s.EfConstruction > hnswMaxEfC):
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: index %s: ef_construction %d, want %d to %d", s.Name, s.EfConstruction, hnswMinEfC, hnswMaxEfC)
	case s.EfConstruction != 0 && s.EfConstruction < 2*m:
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: index %s: ef_construction %d is less than twice m (%d)", s.Name, s.EfConstruction, m)
	}
	return nil
}

func cmpOr(v, def int) int {
	if v != 0 {
		return v
	}
	return def
}

// withSQL is the spec's WITH (…) for the parameters it sets, "" for none.
func (s IndexSpec) withSQL() string {
	var opts []string
	if s.M != 0 {
		opts = append(opts, "m = "+strconv.Itoa(s.M))
	}
	if s.EfConstruction != 0 {
		opts = append(opts, "ef_construction = "+strconv.Itoa(s.EfConstruction))
	}
	if len(opts) == 0 {
		return ""
	}
	return " WITH (" + strings.Join(opts, ", ") + ")"
}

// createSQL is the spec's CREATE INDEX on table (its own, or a reference copy's) as name.
func (s IndexSpec) createSQL(concurrently bool, table, name string) string {
	var b strings.Builder
	b.WriteString("CREATE INDEX ")
	if concurrently {
		b.WriteString("CONCURRENTLY ")
	}
	b.WriteString(quoteIdent(name) + " ON " + PostgresDialect{}.QuoteTable(table) + " USING " + s.Method + " " + s.Element.sql + s.withSQL())
	for i, w := range s.WhereEq {
		if i == 0 {
			b.WriteString(" WHERE ")
		} else {
			b.WriteString(" AND ")
		}
		lit, _ := escapeLiteral(w.Value) // checked
		b.WriteString(quoteIdent(w.Column) + " = " + lit)
	}
	return b.String()
}

// escapeLiteral is v as an escape-string literal, E'…', with every backslash and every quote
// doubled: in an escape-string literal both escapes hold whatever standard_conforming_strings is, so no
// value ends the literal early. A NUL, any other control character (U+0000 to U+001F, U+007F) or
// invalid UTF-8 is refused.
func escapeLiteral(v string) (string, error) {
	if !utf8.ValidString(v) {
		return "", errs.Wrap(errs.ErrInvalidArgument, "postgres: a predicate value is not valid UTF-8")
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return "", errs.Wrap(errs.ErrInvalidArgument, "postgres: a predicate value holds the control character %U", r)
		}
	}
	return "E'" + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `'`, `''`) + "'", nil
}

func quoteIdent(s string) string { return PostgresDialect{}.QuoteIdent(s) }

// BuildOptions are a concurrent build's or drop's session timeouts, set on the pinned session
// before the DDL. Zero is no timeout, set explicitly, so a pool's statement_timeout or
// lock_timeout never cuts a long build short.
type BuildOptions struct {
	StatementTimeout time.Duration
	LockTimeout      time.Duration
}

// lifecycleSession is p's session for the lifecycle: the pinned connection, outside any
// transaction, marked so its member is destroyed rather than recycled (the lifecycle leaves
// session state: timeouts, a temporary table, an application_name).
func lifecycleSession(p PinnedConn) (*pinnedConn, error) {
	pc, ok := p.(*pinnedConn)
	if !ok {
		return nil, fmt.Errorf("postgres: %w: the index lifecycle needs golib's pinned connection, not %T", dao.ErrUnsupported, p)
	}
	if pc.txOpenLoad() {
		return nil, fmt.Errorf("postgres: the index lifecycle runs outside a transaction, and one is open on the session (%w)", errs.ErrPrecondition)
	}
	pc.sessionSet.Store(true)
	return pc, nil
}

// setTimeouts sets the session's statement_timeout and lock_timeout from o, bound.
func setTimeouts(ctx context.Context, pc *pinnedConn, o BuildOptions) error {
	ms := func(d time.Duration) string { return strconv.FormatInt(d.Milliseconds(), 10) }
	_, err := pc.execPrivate(ctx, "SELECT set_config('statement_timeout', $1, false), set_config('lock_timeout', $2, false)",
		[]any{ms(o.StatementTimeout), ms(o.LockTimeout)})
	return err
}

// CreateIndexConcurrently builds s with CREATE INDEX CONCURRENTLY on p's session, outside any
// transaction, after setting o's timeouts. A build that fails or is cancelled leaves an invalid
// index of s.Name, which ReadIndexState reports and DropIndexConcurrently removes.
func CreateIndexConcurrently(ctx context.Context, p PinnedConn, s IndexSpec, o BuildOptions) error {
	if err := s.check(); err != nil {
		return err
	}
	pc, err := lifecycleSession(p)
	if err != nil {
		return err
	}
	if err := setTimeouts(ctx, pc, o); err != nil {
		return err
	}
	_, err = pc.execPrivate(ctx, s.createSQL(true, s.Table, s.Name), nil)
	return err
}

// qualified is schema.name quoted, or name alone when schema is "".
func qualified(schema, name string) string {
	if schema == "" {
		return quoteIdent(name)
	}
	return quoteIdent(schema) + "." + quoteIdent(name)
}

// DropIndexConcurrently drops schema.name (name alone, resolved by the search path, when schema
// is "") with DROP INDEX CONCURRENTLY IF EXISTS on p's session, outside any transaction, after
// setting o's timeouts. An index that isn't there is no error.
func DropIndexConcurrently(ctx context.Context, p PinnedConn, schema, name string, o BuildOptions) error {
	if schema != "" {
		if err := checkIdent("schema", schema); err != nil {
			return err
		}
	}
	if err := checkIdent("index name", name); err != nil {
		return err
	}
	pc, err := lifecycleSession(p)
	if err != nil {
		return err
	}
	if err := setTimeouts(ctx, pc, o); err != nil {
		return err
	}
	_, err = pc.execPrivate(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+qualified(schema, name), nil)
	return err
}

// RenameIndex renames schema.from (from alone, resolved by the search path, when schema is "") to
// to with ALTER INDEX … RENAME TO on p's session, outside any transaction, after setting o's
// timeouts: the last step of a replacement built under a second name, once the old index is
// dropped. The rename is a catalog change only; it holds its lock for that alone.
func RenameIndex(ctx context.Context, p PinnedConn, schema, from, to string, o BuildOptions) error {
	if schema != "" {
		if err := checkIdent("schema", schema); err != nil {
			return err
		}
	}
	if err := checkIdent("index name", from); err != nil {
		return err
	}
	if err := checkIdent("index name", to); err != nil {
		return err
	}
	pc, err := lifecycleSession(p)
	if err != nil {
		return err
	}
	if err := setTimeouts(ctx, pc, o); err != nil {
		return err
	}
	_, err = pc.execPrivate(ctx, "ALTER INDEX "+qualified(schema, from)+" RENAME TO "+quoteIdent(to), nil)
	return err
}

// IndexDefinition is an index's canonical definition, read from the catalog: its access method,
// the expression of its first element, that element's operator class, its predicate ("" for
// none), and its storage parameters (pg_class.reloptions, sorted and joined by commas, as
// "ef_construction=200,m=16"; "" for none). Two definitions are the same index when they are ==,
// so an index built with other parameters is not the one a spec declares.
type IndexDefinition struct{ Method, Expr, OpClass, Predicate, Options string }

// IndexState is what the catalog says of an index by name.
type IndexState struct {
	Exists, Valid bool
	Def           IndexDefinition
}

// indexStateSQL reads an index's validity and canonical tuple by its name, bound: the schema
// ($1, NULL for the search path) and the name ($2).
const indexStateSQL = `SELECT i.indisvalid, am.amname, pg_get_indexdef(i.indexrelid, 1, true), oc.opcname,
       coalesce(pg_get_expr(i.indpred, i.indrelid, true), ''),
       coalesce((SELECT string_agg(o, ',' ORDER BY o) FROM unnest(c.reloptions) o), '')
  FROM pg_index i
  JOIN pg_class c ON c.oid = i.indexrelid
  JOIN pg_am am ON am.oid = c.relam
  JOIN pg_opclass oc ON oc.oid = i.indclass[0]
 WHERE i.indexrelid = to_regclass(CASE WHEN $1::text IS NULL THEN format('%I', $2::text)
                                       ELSE format('%I.%I', $1::text, $2::text) END)`

// ReadIndexState reads schema.name's state from the catalog through q (a pool, a transaction, or
// a session): whether it exists, whether it is valid (pg_index.indisvalid), and its canonical
// definition. A missing index is Exists: false, not an error. schema "" resolves the name by the
// search path.
func ReadIndexState(ctx context.Context, q dao.Querier, schema, name string) (IndexState, error) {
	var sch any
	if schema != "" {
		if err := checkIdent("schema", schema); err != nil {
			return IndexState{}, err
		}
		sch = schema
	}
	if err := checkIdent("index name", name); err != nil {
		return IndexState{}, err
	}
	return readIndexState(ctx, q, sch, name)
}

func readIndexState(ctx context.Context, q dao.Querier, schema any, name string) (IndexState, error) {
	rows, err := q.QueryContext(ctx, indexStateSQL, schema, name)
	if err != nil {
		return IndexState{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return IndexState{}, rows.Err()
	}
	st := IndexState{Exists: true}
	if err := rows.Scan(&st.Valid, &st.Def.Method, &st.Def.Expr, &st.Def.OpClass, &st.Def.Predicate, &st.Def.Options); err != nil {
		return IndexState{}, err
	}
	return st, rows.Close()
}

// sessionQuerier is a lifecycle session's private query face, for the reference build's catalog
// read, which must run where its temporary table lives.
type sessionQuerier struct{ pc *pinnedConn }

func (s sessionQuerier) QueryContext(ctx context.Context, q string, args ...any) (dao.Rows, error) {
	return s.pc.queryPrivate(ctx, q, args)
}

// ReferenceDefinition is the canonical definition s has when built: on p's session, it creates
// an empty temporary table LIKE s.Table, builds s on it (not concurrently: the table is empty),
// reads that index's definition through the same catalog query as ReadIndexState, and drops the
// table. Comparing it with == against ReadIndexState's tells whether an index of s.Name is the
// one s declares. It leaves the session marked for destruction.
func ReferenceDefinition(ctx context.Context, p PinnedConn, s IndexSpec) (IndexDefinition, error) {
	if err := s.check(); err != nil {
		return IndexDefinition{}, err
	}
	pc, err := lifecycleSession(p)
	if err != nil {
		return IndexDefinition{}, err
	}
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return IndexDefinition{}, err
	}
	tmp := "golib_ref_" + hex.EncodeToString(rnd[:])
	if _, err := pc.execPrivate(ctx, "CREATE TEMPORARY TABLE "+quoteIdent(tmp)+" (LIKE "+PostgresDialect{}.QuoteTable(s.Table)+")", nil); err != nil {
		return IndexDefinition{}, err
	}
	defer func() {
		_, _ = pc.execPrivate(context.WithoutCancel(ctx), "DROP TABLE IF EXISTS pg_temp."+quoteIdent(tmp), nil)
	}()
	if _, err := pc.execPrivate(ctx, s.createSQL(false, "pg_temp."+tmp, tmp+"_i"), nil); err != nil {
		return IndexDefinition{}, err
	}
	st, err := readIndexState(ctx, sessionQuerier{pc}, "pg_temp", tmp+"_i")
	if err != nil {
		return IndexDefinition{}, err
	}
	if !st.Exists {
		return IndexDefinition{}, errors.New("postgres: the reference index is not in the catalog")
	}
	return st.Def, nil
}

// Backend is a session MarkSession marked: its process id, its start, and the consumer's token.
// Only a backend with that pid, that backend_start and that token's application_name is alive,
// cancelled or terminated through this API, so a reused pid, or a session the lifecycle didn't
// mark, is never signalled.
type Backend struct {
	PID   int32
	Start time.Time
	Token string
}

func (b Backend) check() error {
	if b.PID <= 0 {
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: backend pid %d", b.PID)
	}
	if !tokenRule.MatchString(b.Token) {
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: backend token %q is not 1 to 40 of [a-z0-9:_-]", b.Token)
	}
	return nil
}

// MarkSession marks p's session as the lifecycle's: its application_name becomes
// "golib-index-lifecycle/<token>", and it answers the session's backend, which the consumer
// records (persists) to find it again. It leaves the session marked for destruction.
func MarkSession(ctx context.Context, p PinnedConn, token string) (Backend, error) {
	if !tokenRule.MatchString(token) {
		return Backend{}, errs.Wrap(errs.ErrInvalidArgument, "postgres: backend token %q is not 1 to 40 of [a-z0-9:_-]", token)
	}
	pc, err := lifecycleSession(p)
	if err != nil {
		return Backend{}, err
	}
	if _, err := pc.execPrivate(ctx, "SELECT set_config('application_name', $1, false)", []any{lifecycleTag + token}); err != nil {
		return Backend{}, err
	}
	rows, err := pc.queryPrivate(ctx, "SELECT pid, backend_start FROM pg_stat_activity WHERE pid = pg_backend_pid()", nil)
	if err != nil {
		return Backend{}, err
	}
	defer rows.Close()
	b := Backend{Token: token}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Backend{}, err
		}
		return Backend{}, errors.New("postgres: the session's backend is not in pg_stat_activity")
	}
	if err := rows.Scan(&b.PID, &b.Start); err != nil {
		return Backend{}, err
	}
	return b, rows.Close()
}

// backendSQL is the one statement BackendAlive, CancelBackend and TerminateBackend run: fn over
// the pg_stat_activity rows matching the backend's pid, start and application_name, all bound.
func backendSQL(fn string) string {
	return "SELECT coalesce(bool_or(" + fn + "), false) FROM pg_stat_activity WHERE pid = $1 AND backend_start = $2 AND application_name = $3"
}

func backendCall(ctx context.Context, q dao.Querier, b Backend, fn string) (bool, error) {
	if err := b.check(); err != nil {
		return false, err
	}
	rows, err := q.QueryContext(ctx, backendSQL(fn), b.PID, b.Start, lifecycleTag+b.Token)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var ok bool
	if rows.Next() {
		if err := rows.Scan(&ok); err != nil {
			return false, err
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return ok, rows.Close()
}

// BackendAlive reports whether b's session still runs. Ask it outside a long transaction:
// PostgreSQL may answer pg_stat_activity from a snapshot taken at the transaction's first read
// of it.
func BackendAlive(ctx context.Context, q dao.Querier, b Backend) (bool, error) {
	return backendCall(ctx, q, b, "true")
}

// CancelBackend cancels b's running statement (pg_cancel_backend) when b's session still runs,
// and reports whether a signal was sent.
func CancelBackend(ctx context.Context, q dao.Querier, b Backend) (bool, error) {
	return backendCall(ctx, q, b, "pg_cancel_backend(pid)")
}

// TerminateBackend ends b's session (pg_terminate_backend) when it still runs, and reports
// whether a signal was sent.
func TerminateBackend(ctx context.Context, q dao.Querier, b Backend) (bool, error) {
	return backendCall(ctx, q, b, "pg_terminate_backend(pid)")
}
