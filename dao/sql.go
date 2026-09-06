package dao

import "strings"

// builder renders SQL text and collects its bind arguments against a [Dialect].
// It is the core's minimal, zero-dependency SQL builder. This file currently
// implements the batch-insert path; the SELECT/UPDATE/DELETE builders
// are added by.
type builder struct {
	dialect Dialect
	sb      strings.Builder
	args    []any
	n       int // next placeholder index (1-based for $n dialects)
}

// quoteTable quotes a table-position identifier: dialects implementing the
// optional [TableQuoter] capability get qualified (dot-split) quoting; all
// others keep the historical behavior — the whole string quoted as one
// identifier via QuoteIdent.
func quoteTable(d Dialect, table string) string {
	if tq, ok := d.(TableQuoter); ok {
		return tq.QuoteTable(table)
	}
	return d.QuoteIdent(table)
}

// ph appends a bind arg and returns its placeholder text (e.g. "$1" or "?").
func (b *builder) ph(v any) string {
	b.args = append(b.args, v)
	b.n++
	return b.dialect.Placeholder(b.n)
}

// where appends " WHERE p1 AND p2 ..." when preds is non-empty, rendering each
// predicate with the shared placeholder counter so numbering stays continuous.
func (b *builder) where(preds []Predicate) {
	if len(preds) == 0 {
		return
	}
	b.sb.WriteString(" WHERE ")
	for i, p := range preds {
		if i > 0 {
			b.sb.WriteString(" AND ")
		}
		s, a := p.ToSQL(b.dialect, &b.n)
		b.sb.WriteString(s)
		b.args = append(b.args, a...)
	}
}

// fromAndJoins writes "FROM <table> <join> <join> ...".
func (b *builder) fromAndJoins(table string, joins []joinClause) {
	b.sb.WriteString(" FROM ")
	b.sb.WriteString(quoteTable(b.dialect, table))
	for _, j := range joins {
		b.sb.WriteByte(' ')
		b.sb.WriteString(j.sql)
	}
}

// buildSelect renders SELECT <cols> FROM <table> <joins> [WHERE] [ORDER BY]
// [LIMIT] [OFFSET]. cols are already-resolved SQL expressions (Field.Column),
// emitted verbatim; the table is quoted.
func (b *builder) buildSelect(table string, cols []string, joins []joinClause,
	where []Predicate, order []orderClause, limit, offset *uint64) string {
	b.sb.WriteString("SELECT ")
	if len(cols) == 0 {
		b.sb.WriteByte('*')
	} else {
		b.sb.WriteString(strings.Join(cols, ", "))
	}
	b.fromAndJoins(table, joins)
	b.where(where)
	if len(order) > 0 {
		b.sb.WriteString(" ORDER BY ")
		for i, o := range order {
			if i > 0 {
				b.sb.WriteString(", ")
			}
			b.sb.WriteString(o.expr)
			if o.desc {
				b.sb.WriteString(" DESC")
			} else {
				b.sb.WriteString(" ASC")
			}
		}
	}
	if limit != nil {
		b.sb.WriteString(" LIMIT ")
		b.sb.WriteString(b.ph(int64(*limit))) // int64: portable across drivers (pgx won't encode uint64)
	}
	if offset != nil {
		b.sb.WriteString(" OFFSET ")
		b.sb.WriteString(b.ph(int64(*offset)))
	}
	return b.sb.String()
}

// buildCount renders SELECT COUNT(*) FROM <table> <joins> [WHERE].
func (b *builder) buildCount(table string, joins []joinClause, where []Predicate) string {
	b.sb.WriteString("SELECT COUNT(*)")
	b.fromAndJoins(table, joins)
	b.where(where)
	return b.sb.String()
}

// buildExists renders SELECT EXISTS(SELECT 1 FROM <table> <joins> [WHERE]).
func (b *builder) buildExists(table string, joins []joinClause, where []Predicate) string {
	b.sb.WriteString("SELECT EXISTS(SELECT 1")
	b.fromAndJoins(table, joins)
	b.where(where)
	b.sb.WriteByte(')')
	return b.sb.String()
}

// insertCore writes INSERT INTO <table> (<cols>) VALUES (<ph>...) with columns in
// the set's stable sorted order.
func (b *builder) insertCore(table string, set orderedSet) []string {
	keys := set.sortedKeys()
	b.sb.WriteString("INSERT INTO ")
	b.sb.WriteString(quoteTable(b.dialect, table))
	b.sb.WriteString(" (")
	for i, c := range keys {
		if i > 0 {
			b.sb.WriteString(", ")
		}
		b.sb.WriteString(b.dialect.QuoteIdent(c))
	}
	b.sb.WriteString(") VALUES (")
	for i, c := range keys {
		if i > 0 {
			b.sb.WriteString(", ")
		}
		b.sb.WriteString(b.ph(set.m[c]))
	}
	b.sb.WriteByte(')')
	return keys
}

// appendReturning appends " RETURNING <idCol>" when the dialect supports it.
func (b *builder) appendReturning(idCol string, returning bool) {
	if !returning || idCol == "" {
		return
	}
	quoted := b.dialect.QuoteIdent(idCol)
	if r, ok := b.dialect.(Returner); ok {
		// The dialect owns the clause, so an engine whose RETURNING syntax
		// differs writes its own rather than being rendered for.
		b.sb.WriteString(r.ReturningClause(quoted))
		return
	}
	// The caller asked for RETURNING on a dialect that does not implement
	// Returner. Render the historical clause rather than silently dropping it:
	// omitting it here would turn a query that asks for an id into one that
	// returns no rows, and the scan would fail somewhere else entirely.
	b.sb.WriteString(" RETURNING ")
	b.sb.WriteString(quoted)
}

// buildInsert renders an INSERT, with RETURNING <idCol> when returning is true.
func (b *builder) buildInsert(table string, set orderedSet, idCol string, returning bool) string {
	b.insertCore(table, set)
	b.appendReturning(idCol, returning)
	return b.sb.String()
}

// buildUpsert renders an INSERT plus the dialect's conflict clause, with
// RETURNING when supported. updateCols are the staged columns minus the conflict
// columns.
func (b *builder) buildUpsert(table string, set orderedSet, idCol string, returning bool, conflict, updateCols []string) string {
	b.insertCore(table, set)
	// The conflict clause belongs to the engine that can upsert. A dialect
	// that cannot renders none, leaving a plain INSERT — the same statement
	// this produced before, for the same engines.
	suffix := ""
	if u, ok := b.dialect.(Upserter); ok {
		suffix = u.BuildUpsertSuffix(conflict, updateCols)
	}
	if suffix != "" {
		b.sb.WriteByte(' ')
		b.sb.WriteString(suffix)
	}
	b.appendReturning(idCol, returning)
	return b.sb.String()
}

// buildUpdate renders UPDATE <table> SET ... WHERE ... When the query carries
// joins (a filter on a joined table), the WHERE becomes an id-subselect because
// portable UPDATE cannot JOIN.
func (b *builder) buildUpdate(table, idCol string, set orderedSet, joins []joinClause, where []Predicate) string {
	qt := quoteTable(b.dialect, table)
	b.sb.WriteString("UPDATE ")
	b.sb.WriteString(qt)
	b.sb.WriteString(" SET ")
	for i, c := range set.sortedKeys() {
		if i > 0 {
			b.sb.WriteString(", ")
		}
		b.sb.WriteString(b.dialect.QuoteIdent(c))
		b.sb.WriteString(" = ")
		b.sb.WriteString(b.ph(set.m[c]))
	}
	b.whereOrSubselect(table, idCol, joins, where)
	return b.sb.String()
}

// buildDelete renders DELETE FROM <table> WHERE ... with the same join-as-subselect
// handling as buildUpdate.
func (b *builder) buildDelete(table, idCol string, joins []joinClause, where []Predicate) string {
	b.sb.WriteString("DELETE FROM ")
	b.sb.WriteString(quoteTable(b.dialect, table))
	b.whereOrSubselect(table, idCol, joins, where)
	return b.sb.String()
}

// whereOrSubselect writes a plain WHERE when there are no joins, or
// " WHERE <id> IN (SELECT <table>.<id> FROM <table> <joins> WHERE ...)" when a
// filter targets a joined table (portable UPDATE/DELETE cannot JOIN).
func (b *builder) whereOrSubselect(table, idCol string, joins []joinClause, where []Predicate) {
	if len(joins) == 0 {
		b.where(where)
		return
	}
	qt := quoteTable(b.dialect, table)
	qid := b.dialect.QuoteIdent(idCol)
	b.sb.WriteString(" WHERE ")
	b.sb.WriteString(qid)
	b.sb.WriteString(" IN (SELECT ")
	b.sb.WriteString(qt)
	b.sb.WriteByte('.')
	b.sb.WriteString(qid)
	b.fromAndJoins(table, joins)
	b.where(where)
	b.sb.WriteByte(')')
}

// buildBatchInsert renders a multi-row INSERT for the given table and columns,
// one VALUES tuple per row in matrix, with placeholders numbered per the dialect
// and arguments collected row-major into b.args. suffix, when non-empty, is
// appended (e.g. an ON CONFLICT clause). matrix rows must be aligned positionally
// with cols.
func (b *builder) buildBatchInsert(table string, cols []string, matrix [][]any, suffix string) string {
	b.sb.Reset()
	b.args = b.args[:0]

	b.sb.WriteString("INSERT INTO ")
	b.sb.WriteString(quoteTable(b.dialect, table))
	b.sb.WriteString(" (")
	for i, c := range cols {
		if i > 0 {
			b.sb.WriteString(", ")
		}
		b.sb.WriteString(b.dialect.QuoteIdent(c))
	}
	b.sb.WriteString(") VALUES ")

	n := 0
	for ri, row := range matrix {
		if ri > 0 {
			b.sb.WriteString(", ")
		}
		b.sb.WriteByte('(')
		for ci := range cols {
			if ci > 0 {
				b.sb.WriteString(", ")
			}
			n++
			b.sb.WriteString(b.dialect.Placeholder(n))
			if ci < len(row) {
				b.args = append(b.args, row[ci])
			} else {
				b.args = append(b.args, nil)
			}
		}
		b.sb.WriteByte(')')
	}

	if suffix != "" {
		b.sb.WriteByte(' ')
		b.sb.WriteString(suffix)
	}
	return b.sb.String()
}
