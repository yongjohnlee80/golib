package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/yongjohnlee80/golib/errs"
)

// ADR-0018 §2.5: the closed neutral vocabulary. [ExtendedOp] is the [PinnedConn.Send]
// argument, [ExtendedMessage] the [PinnedConn.Receive] result — small types mirroring
// the wire messages a relay must forward, byte-faithfully, with the conversion to and
// from pgproto3 frames kept inside the driver.
//
// THE SUM IS CLOSED. It is exactly Parse, Bind, Describe, Execute, Close — no RawFrame,
// no Other, no []byte passthrough, no embedded pgproto3 value, no extension hook (each
// would recreate the rejected raw-frontend exposure under another name). The
// simple-protocol Query frame is EXCLUDED BY DESIGN: a relay consumer's classifier /
// grant gate runs at Parse, and a Query op on this seam would bypass it entirely. There
// is no constructor for it — the exclusion is compile-enforced — and the boundary test
// proves no spelling of a simple Query reaches the wire through the seam.
//

// ExtendedOp is one extended-protocol frontend frame the consumer may queue with
// [PinnedConn.Send]. Exactly five shapes exist; the type is a closed sum, built only
// through the named constructors in this file.
type ExtendedOp struct {
	// kind discriminates the sum. Unexported and unconstructable from outside the
	// package: a caller builds an op through the named constructors below, which are
	// the whole vocabulary.
	kind opKind

	// Parse: Name ("" = unnamed), SQL, and the declared parameter OIDs (empty = let
	// the server infer).
	name      string
	sql       string
	paramOIDs []uint32

	// Bind: PortalName ("" = unnamed), StatementName, and the raw parameter payloads —
	// byte-for-byte as the relayed client sent them, with the format codes they
	// carried.
	portalName    string
	statementName string
	paramValues   [][]byte
	paramFormats  []int16

	// Bind's per-column result formats (nil = all-text).
	resultFormats []int16

	// Describe: which object — statement or portal — by name.
	describePortal bool

	// Execute: the portal's name and the row limit (0 = unlimited).
	maxRows uint32

	// Close: which object kind, by name.
	closePortal bool
}

// opKind is the unexported discriminator of the closed sum.
type opKind int

const (
	// opInvalid is the zero value: an ExtendedOp{} built without a constructor. Send
	// refuses it, so the composite literal is not a sixth spelling of the vocabulary.
	opInvalid opKind = iota
	opParse
	opBind
	opDescribe
	opExecute
	opClose
)

// ErrInvalidExtendedOp reports an ExtendedOp that was not built through one of the named
// constructors (the zero value). The vocabulary is exactly the constructors.
var ErrInvalidExtendedOp = errors.New("postgres: extended op not built through a constructor")

// ParseOp queues a Parse frame. An empty name is the unnamed statement; empty paramOIDs
// lets the server infer them.
func ParseOp(name, sql string, paramOIDs []uint32) ExtendedOp {
	return ExtendedOp{kind: opParse, name: name, sql: sql, paramOIDs: paramOIDs}
}

// BindOp queues a Bind frame: raw parameter values and format codes, byte-for-byte,
// against the named (or unnamed) statement, into the named (or unnamed) portal. A nil
// resultFormats requests all-text results.
func BindOp(portalName, statementName string, paramValues [][]byte, paramFormats, resultFormats []int16) ExtendedOp {
	return ExtendedOp{
		kind: opBind, portalName: portalName, statementName: statementName,
		paramValues: paramValues, paramFormats: paramFormats, resultFormats: resultFormats,
	}
}

// DescribeStatementOp queues a Describe of the named (or unnamed) prepared statement.
func DescribeStatementOp(name string) ExtendedOp {
	return ExtendedOp{kind: opDescribe, name: name, describePortal: false}
}

// DescribePortalOp queues a Describe of the named (or unnamed) portal.
func DescribePortalOp(name string) ExtendedOp {
	return ExtendedOp{kind: opDescribe, name: name, describePortal: true}
}

// ExecuteOp queues an Execute of the named (or unnamed) portal, with the row limit the
// client sent (0 = unlimited).
func ExecuteOp(portalName string, maxRows uint32) ExtendedOp {
	return ExtendedOp{kind: opExecute, portalName: portalName, maxRows: maxRows}
}

// CloseStatementOp queues a Close of the named (or unnamed) prepared statement.
func CloseStatementOp(name string) ExtendedOp {
	return ExtendedOp{kind: opClose, name: name, closePortal: false}
}

// ClosePortalOp queues a Close of the named (or unnamed) portal.
func ClosePortalOp(name string) ExtendedOp {
	return ExtendedOp{kind: opClose, name: name, closePortal: true}
}

// encode renders the op as its pgproto3 frame on the frontend's write buffer. The
// switch is exhaustive over the closed sum; an unknown kind is a programming error
// inside this package and fails loudly. The frontend's Send methods buffer only — any
// encode failure surfaces at the handle's Flush.
func (op ExtendedOp) encode(fe *pgproto3.Frontend) error {
	switch op.kind {
	case opParse:
		fe.SendParse(&pgproto3.Parse{Name: op.name, Query: op.sql, ParameterOIDs: op.paramOIDs})
	case opBind:
		fe.SendBind(&pgproto3.Bind{
			DestinationPortal: op.portalName, PreparedStatement: op.statementName,
			ParameterFormatCodes: op.paramFormats, Parameters: op.paramValues,
			ResultFormatCodes: op.resultFormats,
		})
	case opDescribe:
		fe.SendDescribe(&pgproto3.Describe{ObjectType: objectTypeByte(op.describePortal), Name: op.name})
	case opExecute:
		fe.SendExecute(&pgproto3.Execute{Portal: op.portalName, MaxRows: op.maxRows})
	case opClose:
		fe.SendClose(&pgproto3.Close{ObjectType: objectTypeByte(op.closePortal), Name: op.name})
	case opInvalid:
		return ErrInvalidExtendedOp
	default:
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: unknown extended op kind %d", op.kind)
	}
	return nil
}

// objectTypeByte renders the Describe/Close object-type byte: 'S' for a statement, 'P'
// for a portal.
func objectTypeByte(portal bool) byte {
	if portal {
		return 'P'
	}
	return 'S'
}

// ExtendedMessage is one backend message of a response group, as protocol data. An
// ErrorResponse arrives as [ExtendedMessage.Err], NOT a Go error — the consumer
// classifies and re-frames it for its own client.
type ExtendedMessage struct {
	// Kind names the message: "ParseComplete", "BindComplete",
	// "ParameterDescription", "RowDescription", "DataRow", "CommandComplete",
	// "PortalSuspended", "CloseComplete", "NoData", "EmptyQueryResponse",
	// "ErrorResponse" — and the three asynchronous messages the server may interleave
	// with any group, which a relay forwards verbatim: "NoticeResponse",
	// "ParameterStatus", "NotificationResponse".
	Kind string

	// Fields is the RowDescription's column descriptors, in projection order, for a
	// "RowDescription" message.
	Fields []ExtendedFieldDescription

	// Values is the DataRow's column payloads — BORROWED, valid only until the next
	// Receive on the same handle; a kept row is copied with bytes.Clone (the RawRows
	// rule). A NULL column is a nil slice; an empty non-NULL column is a zero-length
	// non-nil slice.
	Values [][]byte

	// Tag is the CommandComplete's command tag.
	Tag string

	// ParameterOIDs is the ParameterDescription's declared OIDs.
	ParameterOIDs []uint32

	// Err is the server's ErrorResponse, verbatim, when Kind is "ErrorResponse".
	Err *pgconn.PgError

	// Notice is the server's NoticeResponse, verbatim, when Kind is "NoticeResponse".
	Notice *pgconn.Notice

	// ParameterName and ParameterValue carry a ParameterStatus (a run-time parameter
	// the server reports changed, e.g. after SET) when Kind is "ParameterStatus".
	ParameterName, ParameterValue string

	// Notification is a LISTEN/NOTIFY payload when Kind is "NotificationResponse".
	Notification *pgconn.Notification
}

// ExtendedFieldDescription is a RowDescription column descriptor — the same shape
// [dao.FieldDescription] exposes, re-declared here because the
// vocabulary is leaf-local: dao core gains no aliases for this capability.
type ExtendedFieldDescription struct {
	// Name is the column's name in the result set.
	Name string
	// TableOID is the OID of the table the column came from, or 0.
	TableOID uint32
	// ColumnAttr is the column's attribute number within that table, or 0.
	ColumnAttr uint16
	// TypeOID is the OID of the column's data type.
	TypeOID uint32
	// TypeSize is the type's size in bytes; negative for variable-length types.
	TypeSize int16
	// TypeModifier is the type-specific modifier, or -1.
	TypeModifier int32
	// Format is the wire format of the values: 0 text, 1 binary.
	Format int16
}

// decodeMessage converts one raw backend message into the neutral vocabulary. Messages
// the relay does not forward are still surfaced — with their Kind named — so the
// consumer's accounting sees everything the wire said. ErrorResponse and ReadyForQuery
// are handled by the caller before this point (they carry state-machine meaning); a
// message this function does not recognize is a protocol error.
func decodeMessage(msg pgproto3.BackendMessage) (ExtendedMessage, error) {
	switch m := msg.(type) {
	case *pgproto3.ParseComplete:
		return ExtendedMessage{Kind: "ParseComplete"}, nil
	case *pgproto3.BindComplete:
		return ExtendedMessage{Kind: "BindComplete"}, nil
	case *pgproto3.ParameterDescription:
		return ExtendedMessage{Kind: "ParameterDescription", ParameterOIDs: m.ParameterOIDs}, nil
	case *pgproto3.RowDescription:
		fields := make([]ExtendedFieldDescription, len(m.Fields))
		for i, f := range m.Fields {
			fields[i] = ExtendedFieldDescription{
				Name: string(f.Name), TableOID: f.TableOID, ColumnAttr: f.TableAttributeNumber,
				TypeOID: f.DataTypeOID, TypeSize: f.DataTypeSize, TypeModifier: f.TypeModifier,
				Format: f.Format,
			}
		}
		return ExtendedMessage{Kind: "RowDescription", Fields: fields}, nil
	case *pgproto3.DataRow:
		return ExtendedMessage{Kind: "DataRow", Values: m.Values}, nil
	case *pgproto3.CommandComplete:
		return ExtendedMessage{Kind: "CommandComplete", Tag: string(m.CommandTag)}, nil
	case *pgproto3.PortalSuspended:
		return ExtendedMessage{Kind: "PortalSuspended"}, nil
	case *pgproto3.CloseComplete:
		return ExtendedMessage{Kind: "CloseComplete"}, nil
	case *pgproto3.NoData:
		return ExtendedMessage{Kind: "NoData"}, nil
	case *pgproto3.EmptyQueryResponse:
		return ExtendedMessage{Kind: "EmptyQueryResponse"}, nil
	case *pgproto3.NoticeResponse:
		n := pgconn.Notice(*pgconn.ErrorResponseToPgError((*pgproto3.ErrorResponse)(m)))
		return ExtendedMessage{Kind: "NoticeResponse", Notice: &n}, nil
	case *pgproto3.ParameterStatus:
		return ExtendedMessage{Kind: "ParameterStatus", ParameterName: m.Name, ParameterValue: m.Value}, nil
	case *pgproto3.NotificationResponse:
		return ExtendedMessage{Kind: "NotificationResponse", Notification: &pgconn.Notification{PID: m.PID, Channel: m.Channel, Payload: m.Payload}}, nil
	default:
		return ExtendedMessage{}, errs.Wrap(errs.ErrProtocol, "postgres: unexpected backend message %T on the extended face", msg)
	}
}

// errorResponseMessage converts a raw ErrorResponse into the neutral vocabulary's
// protocol-data shape, via pgconn's own canonical converter so the *pgconn.PgError the
// consumer receives is byte-identical to what pgx itself would surface.
func errorResponseMessage(m *pgproto3.ErrorResponse) ExtendedMessage {
	return ExtendedMessage{Kind: "ErrorResponse", Err: pgconn.ErrorResponseToPgError(m)}
}
