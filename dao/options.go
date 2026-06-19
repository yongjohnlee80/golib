package dao

import "github.com/yongjohnlee80/golib/logger"

// Table sets the entity's table/relation name (required).
func Table[R any, C ~string, K ~string, ID any](name string) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] { c.table = name; return c }
}

// ID sets the primary-key field, used for RETURNING and as the subselect key for
// join-bearing UPDATE/DELETE.
func ID[R any, C ~string, K ~string, ID any](field C) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] {
		c.idField = field
		c.hasID = true
		return c
	}
}

// Fields sets the one-source-of-truth field map (required).
func Fields[R any, C ~string, K ~string, ID any](m map[C]Field[R]) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] { c.fields = m; return c }
}

// Default sets the column set used by Get/Select/Iterate when none is given.
// Omitting it selects all fields.
func Default[R any, C ~string, K ~string, ID any](fields ...C) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] { c.defaults = fields; return c }
}

// NewRow sets the row allocator for scanning. It defaults to new(T) for a pointer
// row type *T.
func NewRow[R any, C ~string, K ~string, ID any](fn func() R) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] { c.newRow = fn; return c }
}

// OptionalJoin registers a demand-driven join by key. A field referencing the key
// (Field.Join) or a forced DAO.Join triggers it.
func OptionalJoin[R any, C ~string, K ~string, ID any](key JoinKey, sql string) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] {
		if c.optionalJoins == nil {
			c.optionalJoins = map[JoinKey]string{}
		}
		c.optionalJoins[key] = sql
		return c
	}
}

// JoinForSort declares that ordering by sortKey triggers the named join.
func JoinForSort[R any, C ~string, K ~string, ID any](sortKey K, join JoinKey) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] {
		if c.sortJoins == nil {
			c.sortJoins = map[K]JoinKey{}
		}
		c.sortJoins[sortKey] = join
		return c
	}
}

// SortMap maps each sort-enum key to its ORDER BY expression.
//
// Note: ADR-0006 names this option Sort; it is SortMap here because Sort is the
// ORDER BY term type (ADR-0002) and a package can't have both.
func SortMap[R any, C ~string, K ~string, ID any](m map[K]string) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] { c.sortMap = m; return c }
}

// Search declares the entity's search operators (applied by DAO.Search).
func Search[R any, C ~string, K ~string, ID any](ops ...SearchOp) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] {
		c.search = append(c.search, ops...)
		return c
	}
}

// Conflict sets the ON CONFLICT target columns for Upsert.
func Conflict[R any, C ~string, K ~string, ID any](cols ...C) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] {
		c.conflictFields = cols
		return c
	}
}

// DefaultValues sets a SetMap applied to every write before per-call Set/SetMap.
func DefaultValues[R any, C ~string, K ~string, ID any](m map[C]any) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] { c.defaultValues = m; return c }
}

// Errors maps constraint names to domain errors (applied by Schema.translate).
func Errors[R any, C ~string, K ~string, ID any](m ErrorMap) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] { c.errorMap = m; return c }
}

// WithLogger sets the logger the DAO emits to (default no-op).
func WithLogger[R any, C ~string, K ~string, ID any](l logger.Logger) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] { c.logger = l; return c }
}

// Debug toggles verbose per-statement SQL/arg logging.
func Debug[R any, C ~string, K ~string, ID any](on bool) Option[R, C, K, ID] {
	return func(c *config[R, C, K, ID]) *config[R, C, K, ID] { c.debug = on; return c }
}

// ErrorMap maps a constraint name to a domain error, declared per entity via
// [Errors]. It lets a unique-index name resolve to a specific domain error, e.g.
// {"artist_uri_key": ErrDuplicate}.
type ErrorMap map[string]error
