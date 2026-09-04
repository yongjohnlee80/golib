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
	optionalJoinEx map[JoinKey]Expr
	sortMap        map[K]string
	sortJoins      map[K]JoinKey
	search         []SearchOp
	conflictFields []C
	defaultValues  map[C]any
	errorMap       ErrorMap
	logger         logger.Logger
	debug          bool
	strictClears   bool
	newRow         func() R
	hooks          []Hook
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
	strictClears  bool
	newRow        func() R
	hooks         []Hook
}

// New builds an immutable [Schema] for one entity from a [DataConn] and options.
// It validates required options (Table, Fields, and a registered join for every
// field that declares one) and panics with a precise message at construction —
// not per query — on misconfiguration.
func New[R any, C ~string, K ~string, ID any](conn DataConn, opts ...Option[R, C, K, ID]) *Schema[R, C, K, ID] {
	cfg := &config[R, C, K, ID]{
		fields:         map[C]Field[R]{},
		optionalJoins:  map[JoinKey]string{},
		optionalJoinEx: map[JoinKey]Expr{},
		sortMap:        map[K]string{},
		sortJoins:      map[K]JoinKey{},
		defaultValues:  map[C]any{},
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

	// Resolve declared Exprs into a schema-owned clone BEFORE anything reads a
	// field: the id column, the search-op binding, the conflict columns and the
	// default values all consume Column/writeCol below (ADR-0016 §2.2).
	cfg.fields = resolveFields[R, C](conn.Dialect(), cfg.fields)

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
		strictClears:  cfg.strictClears,
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
	for k, e := range cfg.optionalJoinEx {
		s.optionalJoins[k] = joinClause{key: k, sql: e.render(conn.Dialect())}
	}
	for key, f := range cfg.fields {
		if f.Join != "" {
			if _, ok := s.optionalJoins[f.Join]; !ok {
				panic(fmt.Sprintf("dao.New: field %q references unregistered join %q", any(key), f.Join))
			}
		}
		if f.ClearValue != nil && !f.Clearable {
			panic(fmt.Sprintf("dao.New: field %q declares ClearValue without Clearable", any(key)))
		}
		// Write-column safety (ADR-0016 §2.6). writeCol derives the bare name as
		// the tail after the last dot, which is meaningless for an expression —
		// COALESCE(...) yields `"name", '')`. A writable field must therefore
		// resolve to a plain identifier. Runs after resolution, so T/C fields
		// (which carry a raw WriteColumn) pass; an explicit WriteColumn is the
		// author's call and is trusted.
		if !f.ReadOnly && f.WriteColumn == "" {
			if wc := f.writeCol(); !plainIdent(wc) {
				panic(fmt.Sprintf("dao.New: field %q is writable but its write column %q is not a plain identifier "+
					"(declare ReadOnly: true, or set WriteColumn)", any(key), wc))
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

	// hooks: registration order preserved; duplicate names are construction
	// errors (ADR-0009 §2.2)
	names := map[string]bool{}
	for _, h := range cfg.hooks {
		if h == nil {
			panic("dao.New: nil hook")
		}
		if n := hookName(h); n != "" {
			if names[n] {
				panic(fmt.Sprintf("dao.New: duplicate hook name %q", n))
			}
			names[n] = true
		}
	}
	s.hooks = cfg.hooks

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

// resolveFields returns a schema-owned copy of the declared fields with every
// [Expr] rendered against this connection's dialect (ADR-0016 §2.2).
//
// It clones deliberately. Fields aliases the map it is handed and New stores
// that same reference, so resolving in place would mutate a package-level var:
// a second New over the same declaration would then see both Column and Expr
// set — tripping the panic below — or silently inherit the first dialect.
// Cloning also stops a caller who mutates their map after New from mutating the
// live schema.
func resolveFields[R any, C ~string](d Dialect, in map[C]Field[R]) map[C]Field[R] {
	out := make(map[C]Field[R], len(in))
	for key, f := range in {
		if f.Expr.isSet() {
			if f.Column != "" {
				panic(fmt.Sprintf("dao.New: field %q sets both Column and Expr", any(key)))
			}
			f.Column = f.Expr.render(d)
			// An explicit WriteColumn always wins; otherwise T/C supply the raw
			// name so INSERT/UPDATE quote it exactly as they quote a
			// hand-written declaration's derived tail.
			if f.WriteColumn == "" {
				f.WriteColumn = f.Expr.write
			}
			// Resolution CONSUMES the Expr: Column and WriteColumn now carry
			// everything, so the clone drops the renderer closure. The schema
			// then retains no closures at all and its resolved fields are
			// content-identical to a hand-written declaration — which is what
			// makes the two forms indistinguishable at query time, not merely
			// equal in allocations. The caller's map keeps its Exprs; only this
			// copy is cleared.
			f.Expr = Expr{}
		}
		out[key] = f
	}
	return out
}

// acquire assembles the effective per-call state from QueryOptions: schema
// hooks first, then per-call hooks, minus skipped names, with the debug
// logger (when enabled) appended last so it logs the final SQL as executed
// (ADR-0009 §2.2, §2.5). With nothing registered it returns a nil slice —
// the zero-cost fast path.
func (s *Schema[R, C, K, ID]) acquire(opts []QueryOption) queryConfig {
	var qc queryConfig
	for _, o := range opts {
		if o != nil {
			o(&qc)
		}
	}
	perCall := qc.hooks
	qc.hooks = nil
	total := len(s.hooks) + len(perCall)
	if s.debug {
		total++
	}
	if total == 0 {
		return qc
	}
	effective := make([]Hook, 0, total)
	for _, h := range s.hooks {
		if !qc.skip[hookName(h)] {
			effective = append(effective, h)
		}
	}
	for _, h := range perCall {
		if h != nil && !qc.skip[hookName(h)] {
			effective = append(effective, h)
		}
	}
	if s.debug && !qc.skip["dao.log"] {
		table := s.table
		lg := s.logger
		effective = append(effective, logHook{log: func(sql string, args []any) {
			lg.Log(logger.SeverityDebug, map[string]any{"dao": table, "sql": sql, "args": args})
		}})
	}
	qc.hooks = effective
	return qc
}

// DAO returns a fresh, query-scoped DAO on the connection pool (autocommit).
// [Schema.On] with a nil transaction returns the same thing (ADR-0019), which is
// what lets a helper take an executor parameter and pass it straight through.
func (s *Schema[R, C, K, ID]) DAO(opts ...QueryOption) DAO[R, C, ID] {
	qc := s.acquire(opts)
	return &queryDAO[R, C, K, ID]{schema: s, conn: s.conn,
		hooks: qc.hooks, ctxv: qc.ctx, explicitCtx: qc.explicitCtx}
}

// On returns a fresh, query-scoped DAO bound to a transaction. Every statement on
// the returned DAO runs on the transaction (resolved via the connection name),
// with no per-statement rebind — the .Use(tx) footgun is gone (ADR-0005 §4).
//
// A nil tx is CONTRACT, not misuse (ADR-0019): it means "no transaction is
// held", and the returned DAO is exactly the one [Schema.DAO] would return —
// every statement, and the writer from [DAO.Batch], runs on the connection pool
// (autocommit). On never panics on a nil transaction and never begins one of its
// own.
//
// That guarantee is what lets a statement-issuing helper take its executor as a
// parameter and pass it straight through, serving a caller inside [RunTx] and a
// caller outside one with a single signature and no branch:
//
//	func (s *Store) rename(tx *dao.Transaction, id, name string) error {
//	    // tx from RunTx: runs on that transaction. tx nil: runs on the pool.
//	    return s.Artists.On(tx).With(ArtistID, id).Set(ArtistName, name).Update()
//	}
//
// Prefer that shape over an `if tx != nil` selector around On/DAO — the two
// branches it picks between are already the same call.
func (s *Schema[R, C, K, ID]) On(tx *Transaction, opts ...QueryOption) DAO[R, C, ID] {
	qc := s.acquire(opts)
	d := &queryDAO[R, C, K, ID]{schema: s, conn: s.conn, tx: tx,
		hooks: qc.hooks, ctxv: qc.ctx, explicitCtx: qc.explicitCtx}
	if tx != nil && !d.explicitCtx {
		d.ctxv = tx.ctx
	}
	return d
}

// OnCtx returns a query-scoped DAO bound to the transaction carried by ctx (via
// [WithTx]), or an unbound pool DAO when ctx carries none. It is convenience sugar
// over On; the explicit *Transaction remains the source of truth. A
// WithQueryContext option outranks ctx (ADR-0009 §2.3).
func (s *Schema[R, C, K, ID]) OnCtx(ctx context.Context, opts ...QueryOption) DAO[R, C, ID] {
	qc := s.acquire(opts)
	d := &queryDAO[R, C, K, ID]{schema: s, conn: s.conn, tx: txFromContext(ctx),
		hooks: qc.hooks, ctxv: qc.ctx, explicitCtx: qc.explicitCtx}
	if d.ctxv == nil {
		d.ctxv = ctx
	}
	return d
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
