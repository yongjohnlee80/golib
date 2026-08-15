package dao

import (
	"context"
	"fmt"
)

// queryDAO is the concrete, query-scoped implementation behind the [DAO]
// interface. It holds a pointer to the immutable [Schema] plus small mutable
// per-call state, and is not safe for concurrent use.
//
// It carries the sort-enum parameter K (which DAO[R,C,ID] does not) because Go
// generics are invariant: a *Schema[R,C,K,ID] cannot be stored as
// *Schema[R,C,any,ID]. Carrying K here is transparent to callers — Schema.DAO and
// Schema.On return the K-free DAO[R,C,ID] interface that queryDAO satisfies.
type queryDAO[R any, C ~string, K ~string, ID any] struct {
	schema *Schema[R, C, K, ID]
	conn   DataConn
	tx     *Transaction
	ctxv   context.Context

	hooks       []Hook // effective hook slice (nil = zero-cost fast path)
	explicitCtx bool   // ctxv came from WithQueryContext — sticky across Use(tx)

	q   queryState
	w   writeState
	err error
}

// compile-time: *queryDAO satisfies the K-free DAO interface.
var _ DAO[any, string, int] = (*queryDAO[any, string, string, int])(nil)

// execQuerier is the query+exec surface, satisfied by both [DataConn] and [TxConn].
type execQuerier interface {
	Querier
	Execer
}

func (d *queryDAO[R, C, K, ID]) fail(err error) {
	if d.err == nil {
		d.err = err
	}
}

func (d *queryDAO[R, C, K, ID]) ctx() context.Context {
	if d.ctxv != nil {
		return d.ctxv
	}
	return context.Background()
}

func (d *queryDAO[R, C, K, ID]) newBuilder() *builder { return &builder{dialect: d.schema.dialect} }

// handle resolves the statement executor: the transaction's TxConn when bound
// (issuing BEGIN on first touch), else the pool connection. A tx-bound DAO thus
// runs every statement on the transaction with no per-statement rebind (fixes the
// prior art's .Use(tx) footgun, ADR-0005 §4).
func (d *queryDAO[R, C, K, ID]) handle() (execQuerier, error) {
	if d.tx != nil {
		return d.tx.executorFor(d.schema.conn.Name())
	}
	return d.conn, nil
}

// begin opens the hook pipeline for one statement, firing BeforeBuild (with a
// Stager over this DAO's staged state) unless fireBuild is false (batch).
// Returns (nil, nil) when no hooks are registered — the zero-cost fast path.
func (d *queryDAO[R, C, K, ID]) begin(op Op, fireBuild bool) (*pipeline, error) {
	if len(d.hooks) == 0 {
		return nil, nil
	}
	p := &pipeline{hooks: d.hooks, ctx: d.ctx(),
		info: QueryInfo{Op: op, Table: d.schema.table, Conn: d.schema.conn.Name()}}
	if fireBuild {
		st := &stagerFor[R, C, K, ID]{d: d, op: op}
		if err := p.beforeBuild(st); err != nil {
			return nil, err
		}
		if st.err != nil {
			return nil, st.err
		}
		if d.err != nil { // e.g. a hook OrderBy with an unknown sort key
			return nil, d.err
		}
	}
	return p, nil
}

// stagerFor adapts one queryDAO's staged state to the type-erased [Stager]
// surface, enforcing the per-op semantics of ADR-0009 §2.1.
type stagerFor[R any, C ~string, K ~string, ID any] struct {
	d   *queryDAO[R, C, K, ID]
	op  Op
	err error
}

func (s *stagerFor[R, C, K, ID]) Where(p Predicate) {
	if !s.op.whereCapable() {
		if s.err == nil {
			s.err = fmt.Errorf("%w (op %s)", ErrHookWhereUnsupported, s.op)
		}
		return
	}
	s.d.q.where = append(s.d.q.where, p)
}

func (s *stagerFor[R, C, K, ID]) OrderBy(sorts ...Sort) {
	if s.op.IsWrite() {
		return
	}
	s.d.OrderBy(sorts...)
}

func (s *stagerFor[R, C, K, ID]) Limit(n uint64) {
	if s.op.IsWrite() || s.d.q.limit != nil {
		return
	}
	s.d.q.limit = &n
}

func (s *stagerFor[R, C, K, ID]) SetColumn(column string, value any) {
	switch s.op {
	case OpInsert, OpUpdate, OpUpsert:
		s.d.w.set.put(column, value)
	}
}

// collectJoins appends any forced joins (DAO.Join / sort-triggered) to base,
// de-duplicating by key.
func (d *queryDAO[R, C, K, ID]) collectJoins(base []joinClause) []joinClause {
	if len(d.q.forcedJoins) == 0 {
		return base
	}
	seen := make(map[JoinKey]bool, len(base))
	for _, j := range base {
		seen[j.key] = true
	}
	out := base
	for _, k := range d.q.forcedJoins {
		if seen[k] {
			continue
		}
		if jc, ok := d.schema.optionalJoins[k]; ok {
			seen[k] = true
			out = append(out, jc)
		}
	}
	return out
}

// stagedSet merges the write layers in fixed precedence — schema defaults,
// per-call staged values, then rules (ADR-0010 §2.3) — and reports the first
// surviving StrictClears violation (by column order, deterministically).
func (d *queryDAO[R, C, K, ID]) stagedSet() (orderedSet, error) {
	var set orderedSet
	for c, v := range d.schema.defaultVals {
		set.put(c, v)
	}
	for c, v := range d.w.set.m {
		set.put(c, v)
	}
	for _, col := range sortedRuleCols(d.w.rules) {
		r := d.w.rules[col]
		if r.err != nil {
			return orderedSet{}, r.err // final rule is a strict-clear violation
		}
		switch r.kind {
		case ruleWrite, ruleClear: // clear carries its resolved value
			set.put(col, r.value)
		case ruleSkip:
			set.del(col) // authoritative: removes staged/default values
		}
	}
	return set, nil
}

func (d *queryDAO[R, C, K, ID]) Name() string { return d.schema.table }

func (d *queryDAO[R, C, K, ID]) With(field C, values ...any) DAO[R, C, ID] {
	col, ok := d.schema.column(field)
	if !ok {
		d.fail(fmt.Errorf("%w: %v", ErrUnknownField, any(field)))
		return d
	}
	switch len(values) {
	case 0:
		// "filter by nothing" is intentionally ignored.
	case 1:
		d.q.where = append(d.q.where, Eq(col, values[0]))
	default:
		d.q.where = append(d.q.where, In(col, values))
	}
	return d
}

func (d *queryDAO[R, C, K, ID]) Excluding(field C, values ...any) DAO[R, C, ID] {
	if len(values) == 0 {
		return d
	}
	col, ok := d.schema.column(field)
	if !ok {
		d.fail(fmt.Errorf("%w: %v", ErrUnknownField, any(field)))
		return d
	}
	d.q.where = append(d.q.where, NotIn(col, values))
	return d
}

func (d *queryDAO[R, C, K, ID]) WithPredicate(p Predicate) DAO[R, C, ID] {
	d.q.where = append(d.q.where, p)
	return d
}

func (d *queryDAO[R, C, K, ID]) Search(query string) DAO[R, C, ID] {
	for _, term := range parseSearchQuery(query) {
		if op, ok := d.schema.search[term.token]; ok {
			d.q.where = append(d.q.where, op.Predicate(term.value))
		}
	}
	return d
}

func (d *queryDAO[R, C, K, ID]) Set(field C, value any) DAO[R, C, ID] {
	f, ok := d.schema.fields[field]
	if !ok {
		d.fail(fmt.Errorf("%w: %v", ErrUnknownField, any(field)))
		return d
	}
	if f.ReadOnly {
		d.fail(fmt.Errorf("%w: %v", ErrReadOnlyField, any(field)))
		return d
	}
	d.w.set.put(f.writeCol(), value)
	return d
}

func (d *queryDAO[R, C, K, ID]) SetMap(values map[C]any) DAO[R, C, ID] {
	for field, v := range values {
		d.Set(field, v)
	}
	return d
}

func (d *queryDAO[R, C, K, ID]) Clear(field C) DAO[R, C, ID] {
	if f, ok := d.schema.fields[field]; ok && f.ClearValue != nil {
		return d.Set(field, f.ClearValue)
	}
	return d.Set(field, nil)
}

func (d *queryDAO[R, C, K, ID]) SetRules(rules map[C]Rule) DAO[R, C, ID] {
	for field, r := range rules {
		f, ok := d.schema.fields[field]
		if !ok || f.ReadOnly {
			continue // wire-facing leniency: request-derived keys (ADR-0010 §2.4)
		}
		rr := resolvedRule{kind: r.kind, value: r.value}
		if r.kind == ruleClear {
			switch {
			case f.Clearable:
				rr.value = f.ClearValue // nil → SQL NULL, else NOT-NULL sentinel
			case d.schema.strictClears:
				rr = resolvedRule{kind: ruleSkip,
					err: fmt.Errorf("%w: %v", ErrNotClearable, any(field))}
			default:
				rr = resolvedRule{kind: ruleSkip} // downgrade (ADR-0010 §2.2)
			}
		}
		if d.w.rules == nil {
			d.w.rules = map[string]resolvedRule{}
		}
		d.w.rules[f.writeCol()] = rr
	}
	return d
}

func (d *queryDAO[R, C, K, ID]) OrderBy(sorts ...Sort) DAO[R, C, ID] {
	for _, srt := range sorts {
		expr, ok := d.schema.sortExpr[srt.Key]
		if !ok {
			d.fail(fmt.Errorf("%w: sort key %q", ErrUnknownField, srt.Key))
			continue
		}
		d.q.order = append(d.q.order, orderClause{expr: expr, desc: srt.Desc})
		if j, ok := d.schema.sortJoin[srt.Key]; ok && j != "" {
			d.q.forcedJoins = append(d.q.forcedJoins, j)
		}
	}
	return d
}

func (d *queryDAO[R, C, K, ID]) Limit(n uint64) DAO[R, C, ID]  { d.q.limit = &n; return d }
func (d *queryDAO[R, C, K, ID]) Offset(n uint64) DAO[R, C, ID] { d.q.offset = &n; return d }

func (d *queryDAO[R, C, K, ID]) Join(keys ...JoinKey) DAO[R, C, ID] {
	d.q.forcedJoins = append(d.q.forcedJoins, keys...)
	return d
}

func (d *queryDAO[R, C, K, ID]) Use(tx *Transaction) DAO[R, C, ID] {
	d.tx = tx
	// An explicit WithQueryContext is sticky: late tx binding must not demote
	// it behind the transaction's context (ADR-0009 §2.3).
	if tx != nil && !d.explicitCtx {
		d.ctxv = tx.ctx
	}
	return d
}

func (d *queryDAO[R, C, K, ID]) Get(cols ...C) (R, error) {
	var zero R
	if d.err != nil {
		return zero, d.err
	}
	pl, perr := d.begin(OpGet, true)
	if perr != nil {
		return zero, perr
	}
	sqlCols, joins, plan, err := d.schema.resolve(cols)
	if err != nil {
		return zero, err
	}
	one := uint64(1)
	b := d.newBuilder()
	q := b.buildSelect(d.schema.table, sqlCols, d.collectJoins(joins), d.q.where, d.q.order, &one, d.q.offset)
	args := b.args
	if err := pl.beforeExec(&q, &args); err != nil {
		return zero, err
	}
	h, herr := d.handle()
	if herr != nil {
		return zero, herr
	}
	rows, qerr := h.QueryContext(d.ctx(), q, args...)
	if qerr != nil {
		return zero, pl.finish(0, -1, d.schema.translate(qerr))
	}
	defer rows.Close()
	if !rows.Next() {
		if rerr := rows.Err(); rerr != nil {
			return zero, pl.finish(0, -1, d.schema.translate(rerr))
		}
		return zero, pl.finish(0, -1, ErrNoRows)
	}
	r, serr := scanRow(d.schema.newRow, plan, rows)
	if serr != nil {
		return zero, pl.finish(0, -1, d.schema.translate(serr))
	}
	return r, pl.finish(1, -1, nil)
}

func (d *queryDAO[R, C, K, ID]) Select(cols ...C) ([]R, error) {
	if d.err != nil {
		return nil, d.err
	}
	pl, perr := d.begin(OpSelect, true)
	if perr != nil {
		return nil, perr
	}
	sqlCols, joins, plan, err := d.schema.resolve(cols)
	if err != nil {
		return nil, err
	}
	b := d.newBuilder()
	q := b.buildSelect(d.schema.table, sqlCols, d.collectJoins(joins), d.q.where, d.q.order, d.q.limit, d.q.offset)
	args := b.args
	if err := pl.beforeExec(&q, &args); err != nil {
		return nil, err
	}
	h, herr := d.handle()
	if herr != nil {
		return nil, herr
	}
	rows, qerr := h.QueryContext(d.ctx(), q, args...)
	if qerr != nil {
		return nil, pl.finish(0, -1, d.schema.translate(qerr))
	}
	defer rows.Close()
	var out []R
	for rows.Next() {
		r, serr := scanRow(d.schema.newRow, plan, rows)
		if serr != nil {
			return nil, pl.finish(len(out), -1, d.schema.translate(serr))
		}
		out = append(out, r)
	}
	if ferr := pl.finish(len(out), -1, d.schema.translate(rows.Err())); ferr != nil {
		return nil, ferr
	}
	return out, nil
}

func (d *queryDAO[R, C, K, ID]) Iterate(cols ...C) (Iterator[R], error) {
	if d.err != nil {
		return nil, d.err
	}
	pl, perr := d.begin(OpIterate, true)
	if perr != nil {
		return nil, perr
	}
	sqlCols, joins, plan, err := d.schema.resolve(cols)
	if err != nil {
		return nil, err
	}
	b := d.newBuilder()
	q := b.buildSelect(d.schema.table, sqlCols, d.collectJoins(joins), d.q.where, d.q.order, d.q.limit, d.q.offset)
	args := b.args
	if err := pl.beforeExec(&q, &args); err != nil {
		return nil, err
	}
	h, herr := d.handle()
	if herr != nil {
		return nil, herr
	}
	rows, qerr := h.QueryContext(d.ctx(), q, args...)
	if qerr != nil {
		return nil, pl.finish(-1, -1, d.schema.translate(qerr))
	}
	// Execution-only AfterExec (ADR-0009 §2.6): consumption/scan errors surface
	// through the iterator untransformed by hooks.
	if ferr := pl.finish(-1, -1, nil); ferr != nil {
		rows.Close()
		return nil, ferr
	}
	return &rowsIterator[R]{rows: rows, plan: plan, newR: d.schema.newRow, translate: d.schema.translate}, nil
}

func (d *queryDAO[R, C, K, ID]) Exists() (bool, error) {
	if d.err != nil {
		return false, d.err
	}
	pl, perr := d.begin(OpExists, true)
	if perr != nil {
		return false, perr
	}
	b := d.newBuilder()
	q := b.buildExists(d.schema.table, d.collectJoins(nil), d.q.where)
	args := b.args
	if err := pl.beforeExec(&q, &args); err != nil {
		return false, err
	}
	h, herr := d.handle()
	if herr != nil {
		return false, herr
	}
	rows, err := h.QueryContext(d.ctx(), q, args...)
	if err != nil {
		return false, pl.finish(0, -1, d.schema.translate(err))
	}
	var exists bool
	if serr := scanScalar(rows, &exists); serr != nil {
		return false, pl.finish(0, -1, d.schema.translate(serr))
	}
	return exists, pl.finish(1, -1, nil)
}

func (d *queryDAO[R, C, K, ID]) Count() (uint64, error) {
	if d.err != nil {
		return 0, d.err
	}
	pl, perr := d.begin(OpCount, true)
	if perr != nil {
		return 0, perr
	}
	b := d.newBuilder()
	q := b.buildCount(d.schema.table, d.collectJoins(nil), d.q.where)
	args := b.args
	if err := pl.beforeExec(&q, &args); err != nil {
		return 0, err
	}
	h, herr := d.handle()
	if herr != nil {
		return 0, herr
	}
	rows, err := h.QueryContext(d.ctx(), q, args...)
	if err != nil {
		return 0, pl.finish(0, -1, d.schema.translate(err))
	}
	var n uint64
	if serr := scanScalar(rows, &n); serr != nil {
		return 0, pl.finish(0, -1, d.schema.translate(serr))
	}
	return n, pl.finish(1, -1, nil)
}

func (d *queryDAO[R, C, K, ID]) Insert() (ID, error) {
	var zero ID
	if d.err != nil {
		return zero, d.err
	}
	pl, perr := d.begin(OpInsert, true)
	if perr != nil {
		return zero, perr
	}
	set, serr := d.stagedSet()
	if serr != nil {
		return zero, serr
	}
	if set.empty() {
		return zero, ErrNothingToInsert
	}
	returning := d.schema.dialect.SupportsReturning() && d.schema.idColumn != ""
	b := d.newBuilder()
	q := b.buildInsert(d.schema.table, set, d.schema.idColumn, returning)
	args := b.args
	if err := pl.beforeExec(&q, &args); err != nil {
		return zero, err
	}
	h, herr := d.handle()
	if herr != nil {
		return zero, herr
	}
	if returning {
		rows, err := h.QueryContext(d.ctx(), q, args...)
		if err != nil {
			return zero, pl.finish(0, -1, d.schema.translate(err))
		}
		var id ID
		if serr := scanScalar(rows, &id); serr != nil {
			return zero, pl.finish(0, -1, d.schema.translate(serr))
		}
		return id, pl.finish(1, -1, nil)
	}
	res, err := h.ExecContext(d.ctx(), q, args...)
	if err != nil {
		return zero, pl.finish(0, -1, d.schema.translate(err))
	}
	if ferr := pl.finish(0, affectedOf(res), nil); ferr != nil {
		return zero, ferr
	}
	if d.schema.dialect.SupportsLastInsertID() {
		return lastInsertID[ID](res, nil)
	}
	// No RETURNING and no LastInsertID: a documented no-generated-id insert
	// (ADR-0008 §2.6). The DML ran; the caller supplies ids client-side (e.g. a
	// UUID) for append-only stores and must not treat the zero id as meaningful.
	return zero, nil
}

// affectedOf extracts RowsAffected best-effort (-1 when unavailable).
func affectedOf(res Result) int64 {
	if res == nil {
		return -1
	}
	if n, err := res.RowsAffected(); err == nil {
		return n
	}
	return -1
}

func (d *queryDAO[R, C, K, ID]) Update() error {
	if d.err != nil {
		return d.err
	}
	pl, perr := d.begin(OpUpdate, true)
	if perr != nil {
		return perr
	}
	if len(d.q.where) == 0 {
		return ErrNoConditions
	}
	set, serr := d.stagedSet()
	if serr != nil {
		return serr
	}
	if set.empty() {
		return nil
	}
	b := d.newBuilder()
	q := b.buildUpdate(d.schema.table, d.schema.idColumn, set, d.collectJoins(nil), d.q.where)
	args := b.args
	if err := pl.beforeExec(&q, &args); err != nil {
		return err
	}
	h, herr := d.handle()
	if herr != nil {
		return herr
	}
	res, err := h.ExecContext(d.ctx(), q, args...)
	return pl.finish(0, affectedOf(res), d.schema.translate(err))
}

func (d *queryDAO[R, C, K, ID]) Upsert() error {
	if d.err != nil {
		return d.err
	}
	if !d.schema.dialect.SupportsUpsert() {
		return fmt.Errorf("%w: upsert", ErrUnsupported)
	}
	pl, perr := d.begin(OpUpsert, true)
	if perr != nil {
		return perr
	}
	set, serr := d.stagedSet()
	if serr != nil {
		return serr
	}
	if set.empty() {
		return ErrNothingToInsert
	}
	if len(d.schema.conflict) == 0 {
		return fmt.Errorf("dao: Upsert requires a conflict target (use dao.Conflict)")
	}
	update := subtract(set.sortedKeys(), d.schema.conflict)
	b := d.newBuilder()
	q := b.buildUpsert(d.schema.table, set, d.schema.idColumn, false, d.schema.conflict, update)
	args := b.args
	if err := pl.beforeExec(&q, &args); err != nil {
		return err
	}
	h, herr := d.handle()
	if herr != nil {
		return herr
	}
	res, err := h.ExecContext(d.ctx(), q, args...)
	return pl.finish(0, affectedOf(res), d.schema.translate(err))
}

func (d *queryDAO[R, C, K, ID]) Delete() error {
	if d.err != nil {
		return d.err
	}
	pl, perr := d.begin(OpDelete, true)
	if perr != nil {
		return perr
	}
	if len(d.q.where) == 0 {
		return ErrNoConditions
	}
	b := d.newBuilder()
	q := b.buildDelete(d.schema.table, d.schema.idColumn, d.collectJoins(nil), d.q.where)
	args := b.args
	if err := pl.beforeExec(&q, &args); err != nil {
		return err
	}
	h, herr := d.handle()
	if herr != nil {
		return herr
	}
	res, err := h.ExecContext(d.ctx(), q, args...)
	return pl.finish(0, affectedOf(res), d.schema.translate(err))
}

func (d *queryDAO[R, C, K, ID]) Batch() BatchWriter[R, C] {
	var exec Execer = d.conn
	var initErr error
	if d.tx != nil {
		tc, err := d.tx.executorFor(d.schema.conn.Name())
		if err != nil {
			initErr = err
		} else {
			exec = tc
		}
	}
	b := newBatchWriter[R, C](exec, d.schema.dialect, d.schema.table)
	b.ctx = d.ctx() // Flush honors WithQueryContext / the tx context (ADR-0009 §2.3)
	b.initErr = initErr
	b.translate = d.schema.translate
	b.pipe = func(op Op) *pipeline {
		p, _ := d.begin(op, false) // batch fires no BeforeBuild (ADR-0009 §2.6)
		return p
	}
	b.colName = func(c C) string {
		if f, ok := d.schema.fields[c]; ok {
			return f.writeCol()
		}
		return string(c)
	}
	// AddRow extractor: pull each writable field's value from the model via its
	// Field.Value func. Read-only fields and fields without a Value func are
	// skipped (e.g. a DB-generated id).
	b.extract = func(r R) map[C]any {
		m := make(map[C]any)
		for key, f := range d.schema.fields {
			if f.ReadOnly || f.Value == nil {
				continue
			}
			m[key] = f.Value(r)
		}
		return m
	}
	return b
}
