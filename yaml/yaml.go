// Package yaml evaluates a YAML tree: it resolves each node's tag through one of YAML 1.2's
// schemas (Failsafe, JSON, Core) and constructs Go values from the result. It is to
// golib/parse/yaml what decl is to parse/qml: the parser builds the tree and resolves nothing, and
// this package decides what the tree means.
//
// The evaluation lifecycle proceeds in two distinct stages:
//  1. Tag Resolution: [Resolve] assigns an explicit or schema-inferred tag URI to every node
//     in the syntax tree according to YAML 1.2 resolution rules.
//  2. Value Construction: [Evaluate] decodes the typed tree into native Go runtime values
//     (nil, bool, int64, float64, string, []any, or [Map]).
//
// Basic usage:
//
//	st, err := pyaml.Parse(src)
//	if err != nil {
//		return err
//	}
//	v, err := yaml.Evaluate(st.Docs[0], yaml.Core)
//	if err != nil {
//		return err
//	}
//	m, ok := v.(yaml.Map)
//	title, found := m.Get("title")
//
// REFERENCE: https://yaml.org/spec/1.2.2/#chapter-10-recommended-schemas
package yaml

import (
	"errors"

	"github.com/yongjohnlee80/golib/parse"
	pyaml "github.com/yongjohnlee80/golib/parse/yaml"
)

// Schema specifies one of the recommended tag resolution schemas from YAML 1.2, Chapter 10.
// A schema determines how untagged plain scalars and collections are mapped to canonical tag URIs.
type Schema int

const (
	// Failsafe resolves exclusively by structural node kind: sequences to tag:yaml.org,2002:seq,
	// mappings to tag:yaml.org,2002:map, and non-plain scalars to tag:yaml.org,2002:str.
	// Nodes marked with the "?" non-specific tag (such as plain scalars and untagged collections)
	// are left unresolved and omitted from resolution results.
	Failsafe Schema = iota

	// JSON resolves plain scalars matching standard JSON lexical formats for null, bool, int,
	// and float. Any plain scalar matching none of those formats produces a resolution error.
	JSON

	// Core is the recommended default general-purpose YAML schema. It relaxes JSON's strict formats
	// with human-friendly scalar patterns: null (~, empty scalar), boolean variations (true/True/TRUE),
	// octal (0o) and hex (0x) integers, and floating-point infinities/NaNs (.inf, .nan).
	// Any plain scalar not matching numeric or boolean patterns defaults to a string.
	Core
)

// Standard YAML 1.2 tag URIs supported across the recommended schemas.
const (
	TagStr   = "tag:yaml.org,2002:str"
	TagSeq   = "tag:yaml.org,2002:seq"
	TagMap   = "tag:yaml.org,2002:map"
	TagNull  = "tag:yaml.org,2002:null"
	TagBool  = "tag:yaml.org,2002:bool"
	TagInt   = "tag:yaml.org,2002:int"
	TagFloat = "tag:yaml.org,2002:float"
)

// Tags maps each syntax tree node pointer (*pyaml.Node) to its resolved canonical tag URI.
// Nodes that the chosen schema leaves unresolved (such as plain scalars under [Failsafe])
// are absent from the map.
type Tags map[*pyaml.Node]string

// ErrFailsafe is returned by [Evaluate] when called with the [Failsafe] schema. Because Failsafe
// leaves plain scalars and untagged collections unresolved, it cannot construct complete native
// values. Callers needing partial tag inspection under Failsafe should use [Resolve] directly.
var ErrFailsafe = errors.New("yaml: the Failsafe schema leaves nodes unresolved, so it constructs no value; use Resolve")

// Map represents an evaluated YAML mapping whose entries preserve source declaration order and
// admit keys of arbitrary types (such as integers, booleans, or nested structures).
// A native Go map would lose source ordering and prohibit non-hashable composite keys.
type Map []MapItem

// MapItem represents a single key-value entry in an evaluated [Map].
type MapItem struct{ Key, Value any }

// Get returns the value of the entry whose key is the string key: the common frontmatter case.
func (m Map) Get(key string) (any, bool) {
	for _, it := range m {
		if k, ok := it.Key.(string); ok && k == key {
			return it.Value, true
		}
	}
	return nil, false
}

// Error records a failure during YAML tag resolution or native value construction.
// It captures the precise source position of the offending syntax node and, when reporting
// duplicate mapping keys, names the location of the preceding identical key definition.
type Error struct {
	Pos   parse.Position
	Other *parse.Position
	Msg   string
}

func (e *Error) Error() string {
	s := "yaml: " + itoa(e.Pos.Line) + ":" + itoa(e.Pos.Column) + ": " + e.Msg
	if e.Other != nil {
		s += " (and at " + itoa(e.Other.Line) + ":" + itoa(e.Other.Column) + ")"
	}
	return s
}

// Option configures document evaluation behavior in [Evaluate].
type Option func(*config)

type config struct{ maxNodes int }

// DefaultMaxNodes is the default construction budget (1,000,000 nodes) applied by [Evaluate]
// when no custom [MaxNodes] option is specified.
const DefaultMaxNodes = 1_000_000

// MaxNodes returns an [Option] bounding the maximum number of AST nodes [Evaluate] is permitted
// to construct.
//
// In YAML documents containing recursive or multi-reference aliases (such as the "billion laughs"
// expansion attack), each alias dereference re-evaluates and counts its entire target subtree.
// Bounding total constructed nodes halts processing before excessive memory or CPU is consumed.
func MaxNodes(n int) Option { return func(c *config) { c.maxNodes = n } }

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
