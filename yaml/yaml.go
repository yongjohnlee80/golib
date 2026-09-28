// Package yaml evaluates a YAML tree: it resolves each node's tag through one of YAML 1.2's
// schemas (Failsafe, JSON, Core) and constructs Go values from the result. It is to
// golib/parse/yaml what decl is to parse/qml: the parser builds the tree and resolves nothing, and
// this package decides what the tree means.
//
//	st, err := pyaml.Parse(src)
//	v, err := yaml.Evaluate(st.Docs[0], yaml.Core)
//	title, _ := v.(yaml.Map).Get("title")
//
// https://yaml.org/spec/1.2.2/#chapter-10-recommended-schemas
package yaml

import (
	"errors"

	"github.com/yongjohnlee80/golib/parse"
	pyaml "github.com/yongjohnlee80/golib/parse/yaml"
)

// Schema is one of the recommended schemas of YAML 1.2, chapter 10.
type Schema int

const (
	// Failsafe resolves by node kind only: seq, map, str. Nodes with the "?" non-specific tag (plain
	// scalars, untagged collections) are left unresolved.
	Failsafe Schema = iota
	// JSON resolves plain scalars by the JSON formats of null, bool, int and float; any other plain
	// scalar is an error.
	JSON
	// Core, the schema the spec recommends, is JSON's rules made human-friendly: null, ~ and the
	// empty scalar; true/True/TRUE; octal 0o and hex 0x integers; .inf and .nan. Any other plain
	// scalar is a string.
	Core
)

// The tags of the recommended schemas.
const (
	TagStr   = "tag:yaml.org,2002:str"
	TagSeq   = "tag:yaml.org,2002:seq"
	TagMap   = "tag:yaml.org,2002:map"
	TagNull  = "tag:yaml.org,2002:null"
	TagBool  = "tag:yaml.org,2002:bool"
	TagInt   = "tag:yaml.org,2002:int"
	TagFloat = "tag:yaml.org,2002:float"
)

// Tags maps each node to its resolved tag. A node the schema leaves unresolved is absent.
type Tags map[*pyaml.Node]string

// ErrFailsafe is Evaluate's answer for the Failsafe schema: it leaves plain scalars and untagged
// collections unresolved, so it defines no complete native value. Use Resolve.
var ErrFailsafe = errors.New("yaml: the Failsafe schema leaves nodes unresolved, so it constructs no value; use Resolve")

// Map is a mapping as constructed: its entries in source order, with keys of any kind. A Go map
// would lose both.
type Map []MapItem

// MapItem is one entry of a Map.
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

// Error is a document that cannot be resolved or constructed: where, and why. Other is the second
// place a duplicate key names, if any.
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

// Option configures Evaluate.
type Option func(*config)

type config struct{ maxNodes int }

// DefaultMaxNodes is the construction bound when MaxNodes is not given.
const DefaultMaxNodes = 1_000_000

// MaxNodes bounds how many nodes Evaluate constructs, counting an alias's whole target again each
// time it is used. The bound refuses documents whose aliases expand exponentially (the "billion
// laughs" shape) before they are built.
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
