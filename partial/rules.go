package partial

import "reflect"

// RuleKind mirrors the Write/Skip/Clear disposition of dao ADR-0010 without
// importing dao — the projection is data, the adapter (dao.go) is the coupling
// (ADR-0001 §2.9).
type RuleKind uint8

const (
	RuleSkip  RuleKind = iota
	RuleWrite          // Value carries the decoded Go value
	RuleClear
)

// Rule is one field's projected disposition. Value is meaningful only for
// RuleWrite.
type Rule struct {
	Kind  RuleKind
	Value any
}

// Rules projects the patch: one entry per Present field (RuleWrite with the
// value decoded into the field's declared Go type) and per Cleared field
// (RuleClear). Absent fields have no entry. Keys are canonical effective JSON
// names.
func (p *Patch[T]) Rules() (map[string]Rule, error) {
	if p.err != nil {
		return nil, p.err
	}
	pl := p.plan()
	out := make(map[string]Rule, len(p.fields)+len(p.clear))
	for name, raw := range p.fields {
		v, err := typedValue(pl.byName[name].typ, raw)
		if err != nil {
			return nil, &ValidationError{Fields: []FieldError{{Field: name, Reason: err.Error()}}}
		}
		out[name] = Rule{Kind: RuleWrite, Value: v}
	}
	for name := range p.clear {
		out[name] = Rule{Kind: RuleClear}
	}
	return out, nil
}

// typedValue decodes raw into the field's declared type, returning the value
// as the model would carry it (so a downstream write binds the right Go type,
// not a generic any/float64). A nil type (unreachable for planned fields)
// falls back to a generic decode.
func typedValue(typ reflect.Type, raw []byte) (any, error) {
	if typ == nil {
		var v any
		return v, decode(raw, &v)
	}
	ptr := reflect.New(typ)
	if err := decode(raw, ptr.Interface()); err != nil {
		return nil, err
	}
	return ptr.Elem().Interface(), nil
}
