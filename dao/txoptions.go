package dao

import "fmt"

// Driver-transaction options. [TxOptions] is the value a
// driver's BEGIN carries — access mode, isolation level, deferrability. It is
// deliberately NOT the coordinator's [TxOption] (a functional option
// configuring a logical [Transaction]): the two live at
// different layers and never mix.
//
// Every field is tri-state, so an explicit READ WRITE / NOT DEFERRABLE is
// expressible against a server whose default is the opposite. The zero value
// asks for the server's defaults, which is exactly what [DataConn.Begin] has
// always done — so a zero TxOptions is byte-compatible with today's behavior.

// TxAccess is a transaction's access mode.
type TxAccess int

const (
	// TxAccessDefault leaves the access mode to the server's default
	// (default_transaction_read_only on PostgreSQL; READ WRITE elsewhere).
	TxAccessDefault TxAccess = iota
	// TxReadOnly begins a READ ONLY transaction. On PostgreSQL a write inside
	// it fails with SQLSTATE 25006 — the engine-enforced read-only guarantee
	// (autodb M9), which no transport-level check can provide.
	TxReadOnly
	// TxReadWrite begins an explicit READ WRITE transaction, overriding a
	// server default of READ ONLY.
	TxReadWrite
)

// String returns the SQL spelling of the access mode ("" for the default).
func (a TxAccess) String() string {
	switch a {
	case TxAccessDefault:
		return ""
	case TxReadOnly:
		return "read only"
	case TxReadWrite:
		return "read write"
	default:
		return fmt.Sprintf("TxAccess(%d)", int(a))
	}
}

func (a TxAccess) valid() bool { return a >= TxAccessDefault && a <= TxReadWrite }

// TxIsolation is a transaction's isolation level.
type TxIsolation int

const (
	// TxIsoDefault leaves the isolation level to the server's default.
	TxIsoDefault TxIsolation = iota
	// TxReadUncommitted requests READ UNCOMMITTED. PostgreSQL accepts it and
	// reports it back, but behaves as READ COMMITTED — it has no dirty-read
	// mode; MySQL honors it literally.
	TxReadUncommitted
	// TxReadCommitted requests READ COMMITTED.
	TxReadCommitted
	// TxRepeatableRead requests REPEATABLE READ.
	TxRepeatableRead
	// TxSerializable requests SERIALIZABLE.
	TxSerializable
)

// String returns the SQL spelling of the isolation level ("" for the default).
func (i TxIsolation) String() string {
	switch i {
	case TxIsoDefault:
		return ""
	case TxReadUncommitted:
		return "read uncommitted"
	case TxReadCommitted:
		return "read committed"
	case TxRepeatableRead:
		return "repeatable read"
	case TxSerializable:
		return "serializable"
	default:
		return fmt.Sprintf("TxIsolation(%d)", int(i))
	}
}

func (i TxIsolation) valid() bool { return i >= TxIsoDefault && i <= TxSerializable }

// TxDeferrableMode is a transaction's deferrability (PostgreSQL only).
type TxDeferrableMode int

const (
	// TxDeferrableDefault leaves deferrability to the server's default.
	TxDeferrableDefault TxDeferrableMode = iota
	// TxDeferrable begins a DEFERRABLE transaction: it may block at start but
	// never sees a serialization failure. Valid only with [TxSerializable] +
	// [TxReadOnly], the only combination where PostgreSQL gives it effect.
	TxDeferrable
	// TxNotDeferrable begins an explicit NOT DEFERRABLE transaction. Same
	// combination rule as [TxDeferrable].
	TxNotDeferrable
)

// String returns the SQL spelling of the deferrable mode ("" for the default).
func (d TxDeferrableMode) String() string {
	switch d {
	case TxDeferrableDefault:
		return ""
	case TxDeferrable:
		return "deferrable"
	case TxNotDeferrable:
		return "not deferrable"
	default:
		return fmt.Sprintf("TxDeferrableMode(%d)", int(d))
	}
}

func (d TxDeferrableMode) valid() bool {
	return d >= TxDeferrableDefault && d <= TxNotDeferrable
}

// TxOptions are the driver-transaction options carried by [TxBeginner.BeginTx]
// and [SessionTxBeginner.BeginSessionTx]. The zero value means
// "server defaults" and is what [DataConn.Begin] has always sent.
//
// Not every driver can honor every field; the per-driver matrix is
// and is enforced before any BEGIN reaches the wire — see
// [ErrTxOptionInvalid] and [ErrTxOptionUnsupported].
type TxOptions struct {
	// Access is the transaction's access mode.
	Access TxAccess
	// Isolation is the transaction's isolation level.
	Isolation TxIsolation
	// Deferrable is the transaction's deferrability (PostgreSQL only, and only
	// alongside SERIALIZABLE READ ONLY).
	Deferrable TxDeferrableMode
}

// IsDefault reports whether every option is at its server-default zero value —
// the case that maps to the unchanged [DataConn.Begin].
func (o TxOptions) IsDefault() bool { return o == TxOptions{} }

// String renders the options as their SQL tail ("" when all are default), e.g.
// "isolation level serializable read only deferrable".
func (o TxOptions) String() string {
	var out string
	add := func(s string) {
		if s == "" {
			return
		}
		if out != "" {
			out += " "
		}
		out += s
	}
	if o.Isolation != TxIsoDefault {
		add("isolation level " + o.Isolation.String())
	}
	add(o.Access.String())
	add(o.Deferrable.String())
	return out
}

// Validate applies the driver-neutral option rules: every field must be in its
// declared domain, and deferrability is meaningful only under SERIALIZABLE
// READ ONLY (the one combination where PostgreSQL gives it effect — refused
// elsewhere rather than silently no-op'd).
//
// It runs BEFORE any capability probe, so malformed input is reported as
// malformed input ([ErrTxOptionInvalid]) rather than as a capability miss
// ([ErrTxOptionUnsupported]) — validation order 1 of.
// [BeginConnTx] calls it for you; a driver implementing [TxBeginner] calls it
// itself, before building its BEGIN, so the ordering holds however the
// capability is reached.
func (o TxOptions) Validate() error {
	if !o.Access.valid() {
		return &ErrTxOptionInvalid{Option: "Access", Value: o.Access.String()}
	}
	if !o.Isolation.valid() {
		return &ErrTxOptionInvalid{Option: "Isolation", Value: o.Isolation.String()}
	}
	if !o.Deferrable.valid() {
		return &ErrTxOptionInvalid{Option: "Deferrable", Value: o.Deferrable.String()}
	}
	if o.Deferrable != TxDeferrableDefault &&
		!(o.Isolation == TxSerializable && o.Access == TxReadOnly) {
		return &ErrTxOptionInvalid{
			Option: "Deferrable",
			Value:  o.Deferrable.String(),
			Reason: "valid only with Isolation=serializable and Access=read only",
		}
	}
	return nil
}

// nonDefault names the options that are not at their server default, for the
// capability-miss message ("" when the options are all default).
func (o TxOptions) nonDefault() string {
	var out string
	add := func(name, val string) {
		if val == "" {
			return
		}
		if out != "" {
			out += ", "
		}
		out += name + "=" + val
	}
	add("Access", o.Access.String())
	add("Isolation", o.Isolation.String())
	add("Deferrable", o.Deferrable.String())
	return out
}

// ErrTxOptionInvalid reports malformed caller input: an out-of-range enum
// value, or a combination the SQL standard/PostgreSQL does not give meaning to
// (Reason names the rule). It is checked FIRST, before any capability probe,
// and is returned before a BEGIN reaches the wire.
//
// It deliberately does NOT match [ErrUnsupported]: bad input is not a
// capability miss, and conflating the two would let a caller "handle" a typo
// as a driver limitation. Extract it with errors.As.
type ErrTxOptionInvalid struct {
	// Option is the offending field's name ("Access", "Isolation",
	// "Deferrable").
	Option string
	// Value is the offending value's SQL spelling, or its Go rendering when the
	// value is outside the declared domain.
	Value string
	// Reason, when set, explains a cross-field policy violation.
	Reason string
}

// Error implements error.
func (e *ErrTxOptionInvalid) Error() string {
	v := e.Value
	if v == "" {
		v = "(default)"
	}
	if e.Reason == "" {
		return fmt.Sprintf("dao: invalid transaction option %s=%s", e.Option, v)
	}
	return fmt.Sprintf("dao: invalid transaction option %s=%s: %s", e.Option, v, e.Reason)
}

// ErrTxOptionUnsupported reports a well-formed option the driver cannot honor.
// It is checked SECOND — only after [ErrTxOptionInvalid] passes — and, like
// every capability miss in this package, it matches [ErrUnsupported]:
// errors.Is(err, dao.ErrUnsupported) is guaranteed. Extract the
// struct with errors.As to report the driver and the option by name.
//
// It is always returned before a BEGIN reaches the wire: a refused option
// never becomes a silently-weaker transaction.
type ErrTxOptionUnsupported struct {
	// Driver is the dialect name that refused the option ("mysql", "sqlite").
	Driver string
	// Option names the refused option(s), e.g. "Access=read write".
	Option string
}

// Error implements error.
func (e *ErrTxOptionUnsupported) Error() string {
	return fmt.Sprintf("%s: %v: transaction option %s", e.Driver, ErrUnsupported, e.Option)
}

// Unwrap returns [ErrUnsupported] so errors.Is(err, dao.ErrUnsupported) holds.
func (e *ErrTxOptionUnsupported) Unwrap() error { return ErrUnsupported }
