package dao

// This file decomposes the DAO contract into small, named ROLES.
//
// The problem it solves: DAO carries 24 methods, so any function that takes
// one depends on 24 methods even when it calls a single one, and any test
// double has to implement all 24 to stand in for it.
//
// The rule this file follows:
//
//  1. Every DAO method belongs to EXACTLY ONE role. The roles partition the
//     contract — none of them overlap, and together they cover all of it.
//  2. DAO is nothing but the union of the roles. It declares no method of its
//     own, so DAO's method set cannot drift away from the parts.
//  3. Adding a method to DAO therefore means adding it to a role, or writing
//     a new role. Both are visible in review; neither can happen by accident.
//
// Both halves of that rule are enforced by a test, not by convention alone:
// roles_test.go fails if a DAO method belongs to no role or to two, if a role
// is declared but not part of the union, or if the total method set changes.
//
// WHICH ROLE TO ACCEPT. Take the narrowest role your code actually uses.
// The terminal roles — Reader, Writer, Batcher — are fully independent: they
// mention no other role and drop the type parameters they do not need, so a
// fake for one is a handful of methods. The chaining roles — Filterer,
// Setter, ResultShaper, TxBinder — still return DAO, because that is what
// makes their calls chain; they narrow what a function may DO, and what a
// fake must implement, but they do not remove DAO from the type graph.
//
// Narrowing a parameter from DAO to a role is SAFE FOR CALLERS: a DAO value
// satisfies every role, and Go infers the type arguments through the role's
// method signatures. That inference is why Named carries no type parameters
// — a role whose methods never mention R, C or ID leaves them unresolvable
// at the call site.

// Named reports the entity a value acts on.
//
// It is the one role that carries no type parameters, so it can be accepted
// by ordinary non-generic code — logging, metrics, error decoration — that
// only needs to say WHICH entity a failing query belonged to.
type Named interface {
	// Name reports the entity name (table or logical name), used in logs and
	// error context.
	Name() string
}

// Filterer narrows WHICH ROWS a query matches.
//
// Every method ANDs into the same predicate set and returns the DAO so calls
// chain. Accept this role when a helper's whole job is to add conditions and
// it must not be able to stage a write or run a verb.
type Filterer[R any, C ~string, ID any] interface {
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

	// Search applies the entity's declared search operators to a
	// structured query string, e.g. "title:liquid public:true".
	Search(query string) DAO[R, C, ID]
}

// Setter stages WHICH VALUES a write will carry.
//
// Staging never touches the database: it records column values for a later
// Insert, Update or Upsert. Accept this role — rather than the whole DAO —
// when a helper only translates data into staged columns, which is what
// makes it testable against a four-method fake instead of a full DAO.
type Setter[R any, C ~string, ID any] interface {
	// Set stages a column for INSERT/UPDATE/UPSERT. Last write wins per field.
	Set(field C, value any) DAO[R, C, ID]

	// SetMap stages many columns at once, equivalent to calling Set per entry.
	SetMap(values map[C]any) DAO[R, C, ID]

	// Clear stages a column to be written as NULL, distinct from "not set".
	// A field declaring a ClearValue stages that sentinel
	// instead. Developer intent in trusted code: it does not consult
	// Clearable — request-derived clears go through SetRules.
	Clear(field C) DAO[R, C, ID]

	// SetRules stages a partial-write disposition per field: Write
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
}

// ResultShaper controls the ORDER, the SIZE and the SOURCE of a result set,
// without changing which rows qualify.
//
// Sorting, paging and forced joins are the presentation half of a read:
// paginating middleware can accept this role and provably cannot alter the
// query's meaning, because no method here touches the predicates.
type ResultShaper[R any, C ~string, ID any] interface {
	// OrderBy appends ORDER BY terms.
	OrderBy(sorts ...Sort) DAO[R, C, ID]

	// Limit caps the number of returned rows.
	Limit(n uint64) DAO[R, C, ID]

	// Offset skips the first n rows.
	Offset(n uint64) DAO[R, C, ID]

	// Join forces optional joins to be applied even when no selected column or
	// sort triggers them (e.g. when only filtering on a joined table).
	Join(keys ...JoinKey) DAO[R, C, ID]
}

// TxBinder re-binds a value to a transaction, or unbinds it.
//
// Prefer acquiring an already-bound value from the Schema. This role exists
// so the rare code that must move an existing value between scopes can say
// so in its signature and gets nothing else.
type TxBinder[R any, C ~string, ID any] interface {
	// Use binds this DAO to a transaction. Prefer schema.On(tx), which
	// binds at acquisition time; Use exists for completeness.
	//
	// Use(nil) is the ONE EXCEPTION to the nil-transaction contract of
	// [Schema.On]: it unbinds the transaction, so statements run
	// on the pool, but it does NOT clear a context this DAO already inherited
	// from a transaction. So a DAO acquired with On(tx) and then unbound with
	// Use(nil) issues POOL statements carrying the TRANSACTION'S context —
	// including its deadline and its cancellation. That is deliberate stickiness,
	// not a fallthrough
	// to the pool's own context.
	//
	// If you want a pool DAO with no transaction context, acquire one:
	// schema.On(nil) or schema.DAO(), which are equivalent and carry no
	// transaction context.
	Use(tx *Transaction) DAO[R, C, ID]
}

// Reader runs the terminal verbs that RETURN data and change nothing.
//
// Accepting a Reader is an enforced read-only guarantee: the type has no
// Insert, Update, Upsert or Delete to call, so a reporting or export path
// that takes one cannot write, and a reviewer can see that from the
// signature alone. Reader needs no ID type parameter, because reads never
// produce a generated key.
type Reader[R any, C ~string] interface {
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
}

// Writer runs the terminal verbs that CHANGE data.
//
// Which rows are affected and which values are written were decided earlier,
// by a Filterer and a Setter; Writer only executes. It needs no row or field
// type parameter, because a write reports an ID and an error, never rows.
type Writer[ID any] interface {
	// Insert inserts one row from the staged values and returns the generated ID
	// (via RETURNING where the dialect supports it).
	Insert() (ID, error)

	// Update updates rows matching the predicates with the staged values. It
	// returns [ErrNoConditions] when no predicate is set. An empty value set is a
	// no-op, not an error.
	Update() error

	// Upsert inserts, or updates the staged columns on conflict with the
	// configured conflict target.
	Upsert() error

	// Delete deletes rows matching the predicates. It returns [ErrNoConditions]
	// when no predicate is set.
	Delete() error
}

// Batcher opens the bulk-write surface for many rows at once.
//
// It is deliberately its own role: batch writing has a different shape and a
// different failure model from the single-row verbs on Writer, and most code
// that writes never needs it.
type Batcher[R any, C ~string] interface {
	// Batch returns a batch writer for bulk insert/upsert.
	Batch() BatchWriter[R, C]
}
