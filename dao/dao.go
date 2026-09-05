package dao

// DAO is the query-scoped, generic data-access contract for a single entity.
//
// A DAO value is cheap and single-use: obtain one from a [Schema] (ADR-0006) via
// schema.DAO() or schema.On(tx), chain intent methods, then call a terminal verb.
// Intent methods mutate the receiver and return it, so calls chain fluently. A
// DAO value is not safe for concurrent use; acquire a fresh one per query.
//
// Type parameters:
//   - R  is the row/model type (typically *model.X).
//   - C  is the field enum (~string) that keys filtering and selection.
//   - ID is the primary-key type returned by Insert.
type DAO[R any, C ~string, ID any] interface {
	// Identity.
	Named

	// Intent: what the query means. Each returns the DAO so calls chain.
	Filterer[R, C, ID]
	Setter[R, C, ID]
	ResultShaper[R, C, ID]
	TxBinder[R, C, ID]

	// Terminal verbs: each runs the statement.
	Reader[R, C]
	Writer[ID]
	Batcher[R, C]
}

// Iterator streams rows from a result set without buffering them all. Close
// releases the underlying driver rows; always defer Close.
type Iterator[R any] interface {
	Next() bool
	Value() R
	Err() error
	Close() error
}
