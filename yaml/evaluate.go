package yaml

import (
	"math"
	"sort"
	"strconv"
	"strings"

	pyaml "github.com/yongjohnlee80/golib/parse/yaml"
)

// Evaluate resolves doc's tags under schema, then constructs its value: nil, bool, int64, float64,
// string, []any or Map. It takes JSON or Core; Failsafe defines no complete value, and is refused
// with ErrFailsafe.
//
// An explicit tag outside the schema is an error naming the tag; a caller that knows more tags
// wraps Evaluate. An integer beyond int64 is an error, never a float. An alias constructs its
// target again each time it is used, and every construction counts toward MaxNodes; an alias to
// its own ancestor is an error, since a Go value cannot hold the cycle. Two keys of one mapping
// that are equal nodes (spec 3.2.1.3: the same tag and canonical content, a mapping compared as a
// set) are an error naming both.
func Evaluate(doc *pyaml.Document, schema Schema, opts ...Option) (any, error) {
	if schema == Failsafe {
		return nil, ErrFailsafe
	}
	cfg := config{maxNodes: DefaultMaxNodes}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if doc == nil || doc.Root == nil {
		return nil, nil
	}
	tags, err := Resolve(doc, schema)
	if err != nil {
		return nil, err
	}
	e := evaluator{doc: doc, schema: schema, tags: tags, cfg: cfg, done: map[*pyaml.Node]built{}, building: map[*pyaml.Node]bool{}}
	b, err := e.build(doc.Root)
	if err != nil {
		return nil, err
	}
	return b.value, nil
}

type built struct {
	value any
	size  int // nodes constructed for this subtree, aliases counted as their targets' size
}

type evaluator struct {
	doc      *pyaml.Document
	schema   Schema
	tags     Tags
	cfg      config
	count    int
	done     map[*pyaml.Node]built
	building map[*pyaml.Node]bool
}

func (e *evaluator) fail(n *pyaml.Node, msg string) error {
	return &Error{Pos: e.doc.Position(n.Span.Start), Msg: msg}
}

// charge counts size more constructed nodes against the bound.
func (e *evaluator) charge(n *pyaml.Node, size int) error {
	e.count += size
	if e.count > e.cfg.maxNodes {
		return e.fail(n, "the document constructs more than "+itoa(e.cfg.maxNodes)+" nodes (counting each use of an alias)")
	}
	return nil
}

func (e *evaluator) build(n *pyaml.Node) (built, error) {
	if n.Kind == pyaml.KindAlias {
		t := n.Target
		if e.building[t] {
			return built{}, e.fail(n, "the alias *"+n.Alias+" refers to a node that contains it: a cycle no value can hold")
		}
		if b, ok := e.done[t]; ok {
			return b, e.charge(n, b.size) // the target's whole subtree, constructed again
		}
		b, err := e.build(t)
		if err != nil {
			return built{}, err
		}
		return b, e.charge(n, b.size)
	}
	if b, ok := e.done[n]; ok {
		return b, nil
	}
	e.building[n] = true
	defer delete(e.building, n)
	if err := e.charge(n, 1); err != nil {
		return built{}, err
	}
	tag := e.tags[n]
	var b built
	var err error
	switch n.Kind {
	case pyaml.KindScalar:
		b.size = 1
		b.value, err = e.scalar(n, tag)
	case pyaml.KindSequence:
		if tag != TagSeq {
			return built{}, e.fail(n, "a sequence cannot be constructed as "+tagName(tag))
		}
		items := make([]any, 0, len(n.Items))
		b.size = 1
		for _, c := range n.Items {
			cb, err := e.build(c)
			if err != nil {
				return built{}, err
			}
			items = append(items, cb.value)
			b.size = sat(b.size, cb.size)
		}
		b.value = items
	case pyaml.KindMapping:
		if tag != TagMap {
			return built{}, e.fail(n, "a mapping cannot be constructed as "+tagName(tag))
		}
		m := make(Map, 0, len(n.Pairs))
		b.size = 1
		seen := map[string]*pyaml.Node{}
		for _, p := range n.Pairs {
			key, err := e.canon(p.Key, map[*pyaml.Node]bool{})
			if err != nil {
				return built{}, err
			}
			if first, dup := seen[key]; dup {
				other := e.doc.Position(first.Span.Start)
				return built{}, &Error{Pos: e.doc.Position(p.Key.Span.Start), Other: &other, Msg: "a mapping has two equal keys"}
			}
			seen[key] = p.Key
			kb, err := e.build(p.Key)
			if err != nil {
				return built{}, err
			}
			vb, err := e.build(p.Value)
			if err != nil {
				return built{}, err
			}
			m = append(m, MapItem{Key: kb.value, Value: vb.value})
			b.size = sat(sat(b.size, kb.size), vb.size)
		}
		b.value = m
	}
	if err != nil {
		return built{}, err
	}
	e.done[n] = b
	return b, nil
}

// sat adds without overflowing: sizes of exponentially aliased documents grow past any int.
func sat(a, b int) int {
	if a > math.MaxInt-b {
		return math.MaxInt
	}
	return a + b
}

func tagName(tag string) string {
	if tag == "" {
		return "an unresolved node"
	}
	return "!<" + tag + ">"
}

// scalar constructs a scalar by its resolved tag. An explicitly tagged scalar must be in one of its
// tag's formats under the schema.
func (e *evaluator) scalar(n *pyaml.Node, tag string) (any, error) {
	s := string(n.Value)
	switch tag {
	case TagStr:
		return s, nil
	case TagNull:
		if (e.schema == JSON && s == "null") || (e.schema == Core && coreNull(s)) {
			return nil, nil
		}
	case TagBool:
		if (e.schema == JSON && (s == "true" || s == "false")) || (e.schema == Core && coreBool(s)) {
			return s[0] == 't' || s[0] == 'T', nil
		}
	case TagInt:
		if (e.schema == JSON && jsonInt(s)) || (e.schema == Core && coreInt(s)) {
			v, err := parseInt(s)
			if err != nil {
				return nil, e.fail(n, "the integer "+clip(n.Value)+" is outside int64")
			}
			return v, nil
		}
	case TagFloat:
		if (e.schema == JSON && (jsonFloat(s) || jsonInt(s))) || (e.schema == Core && (coreFloat(s) || coreInt(s))) {
			return parseFloat(s), nil
		}
	case TagSeq, TagMap:
		return nil, e.fail(n, "a scalar cannot be constructed as "+tagName(tag))
	default:
		return nil, e.fail(n, "the tag "+tagName(tag)+" is not in the schema")
	}
	return nil, e.fail(n, "\""+clip(n.Value)+"\" is not a "+tagName(tag)+" in the schema")
}

func parseInt(s string) (int64, error) {
	switch {
	case len(s) > 2 && s[:2] == "0o":
		return strconv.ParseInt(s[2:], 8, 64)
	case len(s) > 2 && s[:2] == "0x":
		return strconv.ParseInt(s[2:], 16, 64)
	}
	return strconv.ParseInt(strings.TrimPrefix(s, "+"), 10, 64)
}

func parseFloat(s string) float64 {
	switch strings.ToLower(strings.TrimLeft(s, "+-")) {
	case ".inf":
		if s[0] == '-' {
			return math.Inf(-1)
		}
		return math.Inf(1)
	case ".nan":
		return math.NaN()
	}
	if len(s) > 2 && (s[:2] == "0o" || s[:2] == "0x") {
		v, _ := parseInt(s)
		return float64(v)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		// a well-formed float beyond float64's range saturates, as strconv reports it
		return v
	}
	return v
}

// canon is a node's canonical form for key equality (spec 3.2.1.3): its tag and canonical content,
// a sequence's items in order, a mapping's entries as a set. Two keys are equal nodes exactly when
// their canonical forms are the same string.
func (e *evaluator) canon(n *pyaml.Node, visiting map[*pyaml.Node]bool) (string, error) {
	if n.Kind == pyaml.KindAlias {
		n = n.Target
	}
	if visiting[n] {
		return "", e.fail(n, "a key contains itself through an alias: its equality is not defined")
	}
	visiting[n] = true
	defer delete(visiting, n)
	tag := e.tags[n]
	var b strings.Builder
	part := func(s string) { b.WriteString(itoa(len(s))); b.WriteByte(':'); b.WriteString(s) }
	part(tag)
	switch n.Kind {
	case pyaml.KindScalar:
		v, err := e.scalar(n, tag)
		if err != nil {
			return "", err
		}
		switch x := v.(type) {
		case nil:
			part("")
		case bool:
			part(strconv.FormatBool(x))
		case int64:
			part(strconv.FormatInt(x, 10))
		case float64:
			switch {
			case math.IsNaN(x):
				part("nan")
			case math.IsInf(x, 1):
				part("+inf")
			case math.IsInf(x, -1):
				part("-inf")
			default:
				part(strconv.FormatFloat(x, 'g', -1, 64))
			}
		case string:
			part(x)
		}
	case pyaml.KindSequence:
		b.WriteByte('[')
		for _, c := range n.Items {
			s, err := e.canon(c, visiting)
			if err != nil {
				return "", err
			}
			part(s)
		}
	case pyaml.KindMapping:
		b.WriteByte('{')
		entries := make([]string, 0, len(n.Pairs))
		for _, p := range n.Pairs {
			k, err := e.canon(p.Key, visiting)
			if err != nil {
				return "", err
			}
			v, err := e.canon(p.Value, visiting)
			if err != nil {
				return "", err
			}
			entries = append(entries, itoa(len(k))+":"+k+itoa(len(v))+":"+v)
		}
		sort.Strings(entries)
		for _, en := range entries {
			part(en)
		}
	}
	return b.String(), nil
}
