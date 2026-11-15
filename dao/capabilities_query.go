package dao

import "fmt"

// Increment is a staged value that adds to a column instead of replacing it.
// Build one with [Incr].
type Increment struct{ n any }

// Incr is the value to stage for "field = field + n" in an Update:
//
//	jobs.On(tx).With(JobPath, p).Set(JobAttempts, dao.Incr(1)).Update()
//	// UPDATE "index_job" SET "attempts" = "attempts" + $1 WHERE "path" = $2
//
// The engine adds n to the value the row holds when the statement runs, so
// two concurrent increments both land, where a read followed by a write would
// lose one. n is bound. Insert, Upsert and a batch refuse a staged Incr with
// errs.ErrInvalidArgument, because a new row has no value to add to.
//
// It is a value rather than a DAO method, so every DAO accepts it through Set.
func Incr(n any) Increment { return Increment{n: n} }

// SelectiveUpserter is an optional DAO capability: Upsert, updating only some
// of the staged fields when the row already exists.
type SelectiveUpserter[C ~string] interface {
	UpsertOnly(fields ...C) error
}

// UpsertOnly is d's Upsert, updating only fields on a conflict. Each field
// must also be staged, since the update writes the staged value; every other
// staged value is written by the insert alone:
//
//	// a touch of a queued path moves its seq and keeps when it was enqueued
//	d := jobs.On(tx).Set(JobPath, p).Set(JobSeq, seq).Set(JobEnqueued, now)
//	err := dao.UpsertOnly(d, JobSeq)
//
// With no fields a conflict updates nothing and the existing row is kept. It
// returns [ErrUnsupported] when d does not implement [SelectiveUpserter], and
// Upsert's own errors otherwise.
func UpsertOnly[R any, C ~string, ID any](d DAO[R, C, ID], fields ...C) error {
	u, ok := d.(SelectiveUpserter[C])
	if !ok {
		return fmt.Errorf("%w: a selective upsert (%T does not do one)", ErrUnsupported, d)
	}
	return u.UpsertOnly(fields...)
}

// DistinctSelector is an optional DAO capability: Select, returning each
// distinct row once (SELECT DISTINCT).
type DistinctSelector[R any, C ~string] interface {
	SelectDistinct(cols ...C) ([]R, error)
}

// SelectDistinct is d's Select with each distinct row once, typically over one
// column:
//
//	docs, err := dao.SelectDistinct(chunks.On(tx).With(ChunkTextHash, hashes...), ChunkDoc)
//
// It returns [ErrUnsupported] when d does not implement [DistinctSelector].
func SelectDistinct[R any, C ~string, ID any](d DAO[R, C, ID], cols ...C) ([]R, error) {
	s, ok := d.(DistinctSelector[R, C])
	if !ok {
		return nil, fmt.Errorf("%w: a distinct select (%T does not do one)", ErrUnsupported, d)
	}
	return s.SelectDistinct(cols...)
}

// DistinctCounter is an optional DAO capability: how many distinct values a
// field takes across the matching rows.
type DistinctCounter[C ~string] interface {
	CountDistinct(field C) (uint64, error)
}

// CountDistinct counts the distinct non-NULL values of field across the rows d
// matches (COUNT(DISTINCT field)), ignoring Limit and Offset. It returns
// [ErrUnsupported] when d does not implement [DistinctCounter].
func CountDistinct[R any, C ~string, ID any](d DAO[R, C, ID], field C) (uint64, error) {
	c, ok := d.(DistinctCounter[C])
	if !ok {
		return 0, fmt.Errorf("%w: a distinct count (%T does not do one)", ErrUnsupported, d)
	}
	return c.CountDistinct(field)
}

// FieldBinder is an optional DAO capability: the values a Param field (a
// [Field] whose SQL binds values, such as a [SnippetQuery]) binds when a query
// selects it.
type FieldBinder[R any, C ~string, ID any] interface {
	Bind(field C, args ...any) DAO[R, C, ID]
}

// Bind gives field, a Param field, its values for d's query, as [AscBy] gives
// a bound sort key its own:
//
//	d := chunks.DAO().WithPredicate(dao.Match(ChunkFTS, q))
//	rows, err := dao.Bind(d, ChunkSnippet, q).Select(ChunkPath, ChunkSnippet)
//
// Selecting the field without them, or with another count, is an error naming
// it, and so is binding a field that is not a Param field. A d that does not
// implement [FieldBinder] is returned as it is: it has no Param fields to
// select.
func Bind[R any, C ~string, ID any](d DAO[R, C, ID], field C, args ...any) DAO[R, C, ID] {
	b, ok := d.(FieldBinder[R, C, ID])
	if !ok {
		return d
	}
	return b.Bind(field, args...)
}

// The query DAO has every one of them.
var (
	_ SelectiveUpserter[string]     = (*queryDAO[any, string, string, int])(nil)
	_ DistinctSelector[any, string] = (*queryDAO[any, string, string, int])(nil)
	_ DistinctCounter[string]       = (*queryDAO[any, string, string, int])(nil)
	_ FieldBinder[any, string, int] = (*queryDAO[any, string, string, int])(nil)
)
