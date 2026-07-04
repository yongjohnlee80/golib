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
	// Name reports the entity name (table or logical name), used in logs and
	// error context.
	Name() string

	// With adds an equality/IN predicate: field == values[0] for one value, or
	// field IN (values...) for several. Multiple With calls AND together. With no
	// values it is a no-op.
	With(field C, values ...any) DAO[R, C, ID]

	// Excluding adds a negative predicate: field <> value / field NOT IN (...).
	// With no values it is a no-op.
	Excluding(field C, values ...any) DAO[R, C, ID]

	// WithPredicate adds an arbitrary predicate (ranges, NULL checks, OR groups,
	// raw expressions).
	WithPredicate(p Predicate) DAO[R, C, ID]

	// Search applies the entity's declared search operators (ADR-0006) to a
	// structured query string, e.g. "title:liquid public:true".
	Search(query string) DAO[R, C, ID]

	// Set stages a column for INSERT/UPDATE/UPSERT. Last write wins per field.
	Set(field C, value any) DAO[R, C, ID]

	// SetMap stages many columns at once, equivalent to calling Set per entry.
	SetMap(values map[C]any) DAO[R, C, ID]

	// Clear stages a column to be written as NULL, distinct from "not set".
	// A field declaring a ClearValue (ADR-0010 §2.2) stages that sentinel
	// instead. Developer intent in trusted code: it does not consult
	// Clearable — request-derived clears go through SetRules.
	Clear(field C) DAO[R, C, ID]

	// SetRules stages a partial-write disposition per field (ADR-0010): Write
	// stages a value, Skip removes any staged value for the field, Clear
	// stages the field's declared cleared state. Rules are authoritative for
	// their field over Set/SetMap/DefaultValues regardless of call order;
	// across multiple SetRules calls the last rule per field wins.
	//
	// SetRules is the WIRE-FACING write surface: keys that don't resolve to a
	// writable field (unknown, or ReadOnly) are skipped silently, because a
	// rules map is typically derived from request data whose extra keys are
	// normal. Set/SetMap keep their loud ErrUnknownField/ErrReadOnlyField
	// behavior for developer-authored writes.
	SetRules(rules map[C]Rule) DAO[R, C, ID]

	// OrderBy appends ORDER BY terms.
	OrderBy(sorts ...Sort) DAO[R, C, ID]

	// Limit caps the number of returned rows.
	Limit(n uint64) DAO[R, C, ID]

	// Offset skips the first n rows.
	Offset(n uint64) DAO[R, C, ID]

	// Join forces optional joins to be applied even when no selected column or
	// sort triggers them (e.g. when only filtering on a joined table).
	Join(keys ...JoinKey) DAO[R, C, ID]

	// Use binds this DAO to a transaction. Prefer schema.On(tx) (ADR-0006), which
	// binds at acquisition time; Use exists for completeness.
	Use(tx *Transaction) DAO[R, C, ID]

	// Get returns exactly one row, or [ErrNoRows]. With no cols, the schema's
	// default column set is used.
	Get(cols ...C) (R, error)

	// Select returns all matching rows. With no cols, the default set is used.
	Select(cols ...C) ([]R, error)

	// Iterate streams rows without buffering the whole result set.
	Iterate(cols ...C) (Iterator[R], error)

	// Exists reports whether at least one row matches the current predicates.
	Exists() (bool, error)

	// Count returns the total matching rows, ignoring Limit/Offset.
	Count() (uint64, error)

	// Insert inserts one row from the staged values and returns the generated ID
	// (via RETURNING where the dialect supports it).
	Insert() (ID, error)

	// Update updates rows matching the predicates with the staged values. It
	// returns [ErrNoConditions] when no predicate is set. An empty value set is a
	// no-op, not an error.
	Update() error

	// Upsert inserts, or updates the staged columns on conflict with the
	// configured conflict target (ADR-0006).
	Upsert() error

	// Delete deletes rows matching the predicates. It returns [ErrNoConditions]
	// when no predicate is set.
	Delete() error

	// Batch returns a batch writer for bulk insert/upsert (ADR-0004).
	Batch() BatchWriter[R, C]
}

// Iterator streams rows from a result set without buffering them all. Close
// releases the underlying driver rows; always defer Close.
type Iterator[R any] interface {
	Next() bool
	Value() R
	Err() error
	Close() error
}
