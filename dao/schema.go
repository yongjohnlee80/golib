package dao

import (
	"context"
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/logger"
)

// config is the mutable construction-time state an [Option] mutates. New turns a
// finished config into an immutable [Schema].
type config[R any, C ~string, K ~string, ID any] struct {
	table          string
	idField        C
	hasID          bool
	fields         map[C]Field[R]
	defaults       []C
	optionalJoins  map[JoinKey]string
	sortMap        map[K]string
	sortJoins      map[K]JoinKey
	search         []SearchOp
	conflictFields []C
	defaultValues  map[C]any
	errorMap       ErrorMap
	logger         logger.Logger
	debug          bool
	newRow         func() R
}

// Option configures a [Schema] during construction. It mutates and returns the
// config, applied variadically in order (mirrors golib's request.RequestOption
// idiom); a later option overrides an earlier one for the same setting.
type Option[R any, C ~string, K ~string, ID any] func(*config[R, C, K, ID]) *config[R, C, K, ID]

// Schema is the immutable, concurrency-safe configuration for one entity. Build
// one per entity (via [New]) and hold it for the process lifetime; acquire a
// cheap, single-use [DAO] from it per query via DAO or On.
type Schema[R any, C ~string, K ~string, ID any] struct {
	conn          DataConn
	dialect       Dialect
	table         string
	idColumn      string
	fields        map[C]Field[R]
	defaults      []C
	optionalJoins map[JoinKey]joinClause
	sortExpr      map[string]string  // stringified sort key -> ORDER BY expression
	sortJoin      map[string]JoinKey // stringified sort key -> triggered join
	search        map[string]SearchOp
	conflict      []string // resolved conflict columns
	defaultVals   map[string]any
	errorMap      ErrorMap
	logger        logger.Logger
	debug         bool
	newRow        func() R
}

// New builds an immutable [Schema] for one entity from a [DataConn] and options.
// It validates required options (Table, Fields, and a registered join for every
// field that declares one) and panics with a precise message at construction —
// not per query — on misconfiguration.
func New[R any, C ~string, K ~string, ID any](conn DataConn, opts ...Option[R, C, K, ID]) *Schema[R, C, K, ID] {
	cfg := &config[R, C, K, ID]{
		fields:        map[C]Field[R]{},
		optionalJoins: map[JoinKey]string{},
		sortMap:       map[K]string{},
		sortJoins:     map[K]JoinKey{},
		defaultValues: map[C]any{},
	}
	for _, o := range opts {
		if o != nil {
			cfg = o(cfg)
		}
	}

	if conn == nil {
		panic("dao.New: nil DataConn")
	}
	if cfg.table == "" {
		panic("dao.New: Table is required")
	}
	if len(cfg.fields) == 0 {
		panic("dao.New: Fields is required")
	}

	s := &Schema[R, C, K, ID]{
		conn:          conn,
		dialect:       conn.Dialect(),
		table:         cfg.table,
		fields:        cfg.fields,
		optionalJoins: map[JoinKey]joinClause{},
		sortExpr:      map[string]string{},
		sortJoin:      map[string]JoinKey{},
		search:        map[string]SearchOp{},
		errorMap:      cfg.errorMap,
		debug:         cfg.debug,
	}

	// id column
	if cfg.hasID {
		f, ok := cfg.fields[cfg.idField]
		if !ok {
			panic(fmt.Sprintf("dao.New: ID field %q is not in Fields", any(cfg.idField)))
		}
		s.idColumn = f.writeCol()
	}

	// defaults: explicit, else all fields in a stable order
	if len(cfg.defaults) > 0 {
		s.defaults = cfg.defaults
	} else {
		s.defaults = sortedFieldKeys(cfg.fields)
	}

	// optional joins + validate that every field-declared join is registered
	for k, sql := range cfg.optionalJoins {
		s.optionalJoins[k] = joinClause{key: k, sql: sql}
	}
	for key, f := range cfg.fields {
		if f.Join != "" {
			if _, ok := s.optionalJoins[f.Join]; !ok {
				panic(fmt.Sprintf("dao.New: field %q references unregistered join %q", any(key), f.Join))
			}
		}
	}

	// sort maps -> stringified runtime lookups
	for k, expr := range cfg.sortMap {
		s.sortExpr[fmt.Sprint(k)] = expr
	}
	for k, j := range cfg.sortJoins {
		s.sortJoin[fmt.Sprint(k)] = j
	}

	// search ops by token; bind field-keyed ops to their resolved column
	for _, op := range cfg.search {
		bound := op
		if fb, ok := op.(fieldSearchOp); ok {
			if f, ok2 := cfg.fields[C(fb.fieldKey())]; ok2 {
				bound = fb.withColumn(f.Column)
			}
		}
		s.search[op.Token()] = bound
	}

	// conflict fields -> columns
	for _, cf := range cfg.conflictFields {
		if f, ok := cfg.fields[cf]; ok {
			s.conflict = append(s.conflict, f.writeCol())
		} else {
			panic(fmt.Sprintf("dao.New: Conflict field %q is not in Fields", any(cf)))
		}
	}

	// default write values -> columns
	if len(cfg.defaultValues) > 0 {
		s.defaultVals = map[string]any{}
		for cf, v := range cfg.defaultValues {
			if f, ok := cfg.fields[cf]; ok {
				s.defaultVals[f.writeCol()] = v
			} else {
				panic(fmt.Sprintf("dao.New: DefaultValues field %q is not in Fields", any(cf)))
			}
		}
	}

	// logger + row allocator defaults
	s.logger = cfg.logger
	if s.logger == nil {
		s.logger = logger.Nop{}
	}
	s.newRow = cfg.newRow
	if s.newRow == nil {
		s.newRow = defaultNewRow[R]()
	}

	return s
}

// DAO returns a fresh, query-scoped DAO on the connection pool (autocommit).
func (s *Schema[R, C, K, ID]) DAO() DAO[R, C, ID] {
	return &queryDAO[R, C, K, ID]{schema: s, conn: s.conn}
}

// On returns a fresh, query-scoped DAO bound to a transaction. Every statement on
// the returned DAO runs on the transaction (resolved via the connection name),
// with no per-statement rebind — the .Use(tx) footgun is gone (ADR-0005 §4).
func (s *Schema[R, C, K, ID]) On(tx *Transaction) DAO[R, C, ID] {
	d := &queryDAO[R, C, K, ID]{schema: s, conn: s.conn, tx: tx}
	if tx != nil {
		d.ctxv = tx.ctx
	}
	return d
}

// OnCtx returns a query-scoped DAO bound to the transaction carried by ctx (via
// [WithTx]), or an unbound pool DAO when ctx carries none. It is convenience sugar
// over On; the explicit *Transaction remains the source of truth.
func (s *Schema[R, C, K, ID]) OnCtx(ctx context.Context) DAO[R, C, ID] {
	return &queryDAO[R, C, K, ID]{schema: s, conn: s.conn, tx: txFromContext(ctx), ctxv: ctx}
}

// resolve walks the requested field keys (or the default set if none) and derives
// — in one pass — the SQL projection, the de-duplicated joins to apply, and the
// scan plan. An unknown field key is a fail-fast error.
func (s *Schema[R, C, K, ID]) resolve(fields []C) (cols []string, joins []joinClause, plan []func(R) any, err error) {
	if len(fields) == 0 {
		fields = s.defaults
	}
	seen := map[JoinKey]bool{}
	for _, key := range fields {
		f, ok := s.fields[key]
		if !ok {
			return nil, nil, nil, fmt.Errorf("%w: %v", ErrUnknownField, any(key))
		}
		cols = append(cols, f.Column)
		plan = append(plan, f.Scan)
		if f.Join != "" && !seen[f.Join] {
			seen[f.Join] = true
			joins = append(joins, s.optionalJoins[f.Join])
		}
	}
	return cols, joins, plan, nil
}

// column returns the SQL column expression for a field key.
func (s *Schema[R, C, K, ID]) column(key C) (string, bool) {
	f, ok := s.fields[key]
	if !ok {
		return "", false
	}
	return f.Column, true
}

// translate funnels a driver error through the dialect's translation and the
// per-entity error map. A nil error stays nil.
func (s *Schema[R, C, K, ID]) translate(err error) error {
	if err == nil {
		return nil
	}
	de := s.dialect.TranslateError(err)
	var ce *ConstraintError
	if errors.As(de, &ce) {
		if mapped, ok := s.errorMap[ce.Constraint]; ok {
			return mapped
		}
	}
	return de
}

// sortedFieldKeys returns the field keys of m in stable string order.
func sortedFieldKeys[R any, C ~string](m map[C]Field[R]) []C {
	keys := make([]C, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
