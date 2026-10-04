package yaml

import (
	"errors"
	"iter"
	"sort"

	"github.com/yongjohnlee80/golib/parse"
)

// Kind names what a node is.
type Kind uint8

const (
	KindScalar Kind = iota + 1
	KindSequence
	KindMapping
	KindAlias
)

func (k Kind) String() string {
	switch k {
	case KindScalar:
		return "scalar"
	case KindSequence:
		return "sequence"
	case KindMapping:
		return "mapping"
	case KindAlias:
		return "alias"
	}
	return "kind(?)"
}

// Style is how a node was written: a scalar's quoting or block form, or a collection's block or flow
// form.
type Style uint8

const (
	StylePlain Style = iota + 1
	StyleSingleQuoted
	StyleDoubleQuoted
	StyleLiteral
	StyleFolded
	StyleBlock // a block sequence or mapping
	StyleFlow  // a flow sequence or mapping
)

// Span is a half-open byte range [Start, End) of Stream.Source.
type Span struct{ Start, End int }

// Encoding is the character encoding the input was in (spec 5.2).
type Encoding uint8

const (
	UTF8 Encoding = iota
	UTF16LE
	UTF16BE
	UTF32LE
	UTF32BE
)

// Stream is a parsed YAML stream: its documents, and the UTF-8 source every span indexes.
type Stream struct {
	Docs []*Document
	// Source is the input as UTF-8. A UTF-16 or UTF-32 input is transcoded first, so offsets,
	// spans and Position.Offset index Source, not the original bytes.
	Source   []byte
	Encoding Encoding

	lines []int
}

// Position resolves an offset of Source to a line and a column. Lines end at LF, CR or CRLF (spec
// 5.4); the column counts characters, as parse.Scanner's does, so it is the same whatever the
// input's encoding was.
func (s *Stream) Position(off int) parse.Position {
	if s.lines == nil {
		s.lines = lineStarts(s.Source)
	}
	return position(s.Source, s.lines, off)
}

// Directive is a %YAML or %TAG directive, as written.
type Directive struct {
	Name   string // "YAML" or "TAG"
	Params []string
	Span   Span
}

// Document is one document of a stream.
type Document struct {
	Directives    []Directive
	Root          *Node
	ExplicitStart bool // began with "---"
	ExplicitEnd   bool // ended with "..."
	Span          Span

	stream *Stream
}

// Position resolves an offset of the document's stream Source, as Stream.Position does.
func (d *Document) Position(off int) parse.Position {
	if d.stream == nil {
		return parse.Position{Offset: off}
	}
	return d.stream.Position(off)
}

// Node is one node of a document's tree. The parser resolves no tags: a plain scalar is its text
// and its style, and what it means is a schema's to decide.
type Node struct {
	Kind  Kind
	Style Style
	// Tag is the node's tag with any %TAG handle expanded, or "" when it has none. A node written
	// with the non-specific tag "!" has Tag "!". An untagged node is "?" (a plain scalar, a
	// collection) or "!" (another scalar) to a schema; the evaluator decides.
	Tag    string
	Anchor string // the node's anchor name; "" when none
	Alias  string // KindAlias: the anchor it names
	// Target is, for KindAlias, the most recent node before it in the same document with that
	// anchor. The alias is not expanded: the tree shares the node, as the source does.
	Target *Node
	Value  []byte  // KindScalar: the content, after the style's folding and escapes
	Items  []*Node // KindSequence
	Pairs  []Pair  // KindMapping, in source order; a key may be any node
	Span   Span
}

// Pair is one entry of a mapping.
type Pair struct{ Key, Value *Node }

// Error is an ill-formed stream: where, and what the parser expected there.
type Error struct {
	Pos parse.Position
	Msg string
}

func (e *Error) Error() string {
	return "yaml: " + itoa(e.Pos.Line) + ":" + itoa(e.Pos.Column) + ": " + e.Msg
}

// As also answers for golib/parse's shared syntax error, so a caller handling several formats reads
// where any of them failed the same way, with a parse.SyntaxError VALUE as the target:
//
//	var se parse.SyntaxError
//	if errors.As(err, &se) { … se.Pos … }
//
// The parse.SyntaxError carries the format and the position; the message stays on Error.
func (e *Error) As(target any) bool {
	se, ok := target.(*parse.SyntaxError)
	if ok {
		*se = e.syntax()
	}
	return ok
}

// Is reports a syntax error's identities, parse.ErrSyntax and through it errs.ErrInvalidArgument.
func (e *Error) Is(target error) bool { return errors.Is(e.syntax(), target) }

func (e *Error) syntax() parse.SyntaxError { return parse.SyntaxError{Format: "yaml", Pos: e.Pos} }

// Option configures Parse and Events.
type Option func(*config)

type config struct{ maxDepth int }

// DefaultMaxDepth is the nesting bound when MaxDepth is not given.
const DefaultMaxDepth = 1000

// MaxDepth bounds how deeply collections may nest. Deeper input is an *Error, so no input can
// exhaust the stack or the memory a deep parse holds.
func MaxDepth(n int) Option { return func(c *config) { c.maxDepth = n } }

func newConfig(opts []Option) config {
	c := config{maxDepth: DefaultMaxDepth}
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	return c
}

// Parse reads a YAML stream into its documents. An ill-formed stream is an *Error. Parse composes
// its tree from exactly the events Events yields, binding each alias to its anchor's node.
func Parse(src []byte, opts ...Option) (*Stream, error) {
	p, err := newParser(src, newConfig(opts))
	if err != nil {
		return p.stream(), err
	}
	c := composer{stream: p.stream()}
	for {
		ev, err := p.next()
		if err != nil {
			return c.stream, err
		}
		if err := c.add(ev); err != nil {
			return c.stream, err
		}
		if ev.Kind == EventStreamEnd {
			return c.stream, nil
		}
	}
}

// Events yields the parse as the specification's event sequence: the serialization tree, in the
// order the yaml-test-suite's expected results state it. After an error it yields nothing more.
func Events(src []byte, opts ...Option) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		p, err := newParser(src, newConfig(opts))
		if err != nil {
			yield(Event{}, err)
			return
		}
		for {
			ev, err := p.next()
			if err != nil {
				yield(Event{}, err)
				return
			}
			if !yield(ev, nil) || ev.Kind == EventStreamEnd {
				return
			}
		}
	}
}

// EventKind names an event.
type EventKind uint8

const (
	EventStreamStart EventKind = iota + 1
	EventStreamEnd
	EventDocumentStart
	EventDocumentEnd
	EventSequenceStart
	EventSequenceEnd
	EventMappingStart
	EventMappingEnd
	EventScalar
	EventAlias
)

// Event is one step of the serialization. Anchor, Tag and Style are set on node events; Value on a
// scalar; Alias on an alias; Explicit on a document start ("---") or end ("...").
type Event struct {
	Kind       EventKind
	Anchor     string
	Tag        string
	Alias      string
	Style      Style
	Value      []byte
	Explicit   bool
	Directives []Directive // EventDocumentStart
	Span       Span
}

func lineStarts(src []byte) []int {
	out := []int{0}
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\n':
			out = append(out, i+1)
		case '\r':
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
			}
			out = append(out, i+1)
		}
	}
	return out
}

func position(src []byte, lines []int, off int) parse.Position {
	off = max(0, min(off, len(src)))
	i := sort.Search(len(lines), func(i int) bool { return lines[i] > off }) - 1
	col := 1
	for j := lines[i]; j < off; {
		j += charLen(src[j:])
		col++
	}
	return parse.Position{Offset: off, Line: i + 1, Column: col}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
