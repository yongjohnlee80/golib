package partial

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Patch is a presence-aware partial payload for the model type T. It is the
// unit passed between bind, server-side mutation, and the DAO adapter; the
// typed T is derived on demand (Data) from one underlying parse.
//
// The zero Patch is empty and usable (a server-constructed patch starts zero
// and is populated via Set/Clear). A Patch is not safe for concurrent use.
type Patch[T any] struct {
	fields map[string]json.RawMessage // canonical name -> raw value bytes
	clear  map[string]struct{}        // canonical names marked cleared
	mode   ClearMode

	obj   T // cached Data() decode
	objOK bool
	err   error // sticky first mutator error
}

// State is one field's disposition in the patch.
type State uint8

const (
	Absent  State = iota // not mentioned: skip on write
	Present              // carries a value: write it
	Cleared              // marked cleared: write the cleared state
)

// plan resolves the model's canonical name space. Cached per T.
func (p *Patch[T]) plan() *plan { return planFor[T]() }

func (p *Patch[T]) ensure() {
	if p.fields == nil {
		p.fields = map[string]json.RawMessage{}
	}
	if p.clear == nil {
		p.clear = map[string]struct{}{}
	}
}

// invalidate drops the cached Data() decode after a mutation.
func (p *Patch[T]) invalidate() {
	var zero T
	p.obj = zero
	p.objOK = false
}

// State reports the field's disposition. Unknown names report Absent.
func (p *Patch[T]) State(field string) State {
	name, ok := p.plan().canonical(field)
	if !ok {
		return Absent
	}
	if _, ok := p.fields[name]; ok {
		return Present
	}
	if _, ok := p.clear[name]; ok {
		return Cleared
	}
	return Absent
}

// Present reports whether field carries a value.
func (p *Patch[T]) Present(field string) bool { return p.State(field) == Present }

// Cleared reports whether field is marked cleared.
func (p *Patch[T]) Cleared(field string) bool { return p.State(field) == Cleared }

// Contains reports whether every named field is Present.
func (p *Patch[T]) Contains(fields ...string) bool {
	for _, f := range fields {
		if p.State(f) != Present {
			return false
		}
	}
	return true
}

// Fields returns the canonical names carried by the patch (Present and
// Cleared), sorted — the enumerable view.
func (p *Patch[T]) Fields() []string {
	out := make([]string, 0, len(p.fields)+len(p.clear))
	for n := range p.fields {
		out = append(out, n)
	}
	for n := range p.clear {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Empty reports whether the patch carries nothing to apply — no values AND no
// clears (the LM emptiness lesson: a clear-only patch is not empty).
func (p *Patch[T]) Empty() bool { return len(p.fields) == 0 && len(p.clear) == 0 }

// Err returns the sticky first mutator error (nil when clean).
func (p *Patch[T]) Err() error { return p.err }

// Data returns the typed T decoded from the patch's value-bearing fields —
// cached after the first call (Bind primes it). Cleared and absent fields are
// T's zero values; presence is the patch's job, not T's.
func (p *Patch[T]) Data() (T, error) {
	if p.err != nil {
		var zero T
		return zero, p.err
	}
	if p.objOK {
		return p.obj, nil
	}
	var t T
	if len(p.fields) > 0 {
		if err := decode(p.assemble(), &t); err != nil {
			var zero T
			return zero, bindDecodeError(p.plan(), err)
		}
	}
	p.obj = t
	p.objOK = true
	return p.obj, nil
}

// assemble reconstructs a single JSON object from the retained raw slots, in
// canonical-name order for byte stability. No any round-trip — the raw value
// bytes pass through verbatim.
func (p *Patch[T]) assemble() []byte {
	names := make([]string, 0, len(p.fields))
	for n := range p.fields {
		names = append(names, n)
	}
	sort.Strings(names)
	var b bytes.Buffer
	b.WriteByte('{')
	for i, n := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(n)
		b.Write(key)
		b.WriteByte(':')
		b.Write(p.fields[n])
	}
	b.WriteByte('}')
	return b.Bytes()
}

// Get decodes one Present field into V without touching the rest of the patch.
// It is a package-level function because Go methods cannot introduce type
// parameters. The State return reports the field's disposition; v is only
// meaningful when it is Present.
func Get[V any, T any](p *Patch[T], field string) (v V, s State, err error) {
	if p.err != nil {
		return v, Absent, p.err
	}
	s = p.State(field)
	if s != Present {
		return v, s, nil
	}
	name, _ := p.plan().canonical(field)
	if derr := decode(p.fields[name], &v); derr != nil {
		return v, s, &ValidationError{Fields: []FieldError{{Field: name, Reason: derr.Error()}}}
	}
	return v, s, nil
}

// Set stages a server-owned value for field, replacing any bound value and
// dropping any pending clear — the last server mutation wins over a clear (a
// clear entry must not beat an injected value with a NULL write). The value is
// marshaled once here. An unknown field records the sticky first error.
func (p *Patch[T]) Set(field string, v any) *Patch[T] {
	name, ok := p.plan().canonical(field)
	if !ok {
		p.failMutator(field)
		return p
	}
	raw, err := json.Marshal(v)
	if err != nil {
		if p.err == nil {
			p.err = fmt.Errorf("partial: marshal field %q: %w", name, err)
		}
		return p
	}
	p.ensure()
	delete(p.clear, name)
	p.fields[name] = raw
	p.invalidate()
	return p
}

// Clear marks field cleared, dropping any value (a field is cleared or carries
// a value, never both). An unknown field records the sticky first error.
func (p *Patch[T]) Clear(field string) *Patch[T] {
	name, ok := p.plan().canonical(field)
	if !ok {
		p.failMutator(field)
		return p
	}
	p.ensure()
	delete(p.fields, name)
	p.clear[name] = struct{}{}
	p.invalidate()
	return p
}

// Remove strips field from BOTH channels — a server-controlled strip silently
// discards a client value and its clear intent alike. Removing an unknown
// field is a no-op (a strip of something not present), NOT a mutator error.
func (p *Patch[T]) Remove(field string) *Patch[T] {
	name, ok := p.plan().canonical(field)
	if !ok {
		return p
	}
	delete(p.fields, name)
	delete(p.clear, name)
	p.invalidate()
	return p
}

// Only keeps the named fields, intersecting BOTH channels (allowlist). Unknown
// names in the allowlist are ignored. Names are canonicalized first.
func (p *Patch[T]) Only(fields ...string) *Patch[T] {
	keep := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		if name, ok := p.plan().canonical(f); ok {
			keep[name] = struct{}{}
		}
	}
	for n := range p.fields {
		if _, ok := keep[n]; !ok {
			delete(p.fields, n)
		}
	}
	for n := range p.clear {
		if _, ok := keep[n]; !ok {
			delete(p.clear, n)
		}
	}
	p.invalidate()
	return p
}

func (p *Patch[T]) failMutator(field string) {
	if p.err == nil {
		p.err = fmt.Errorf("%w: %q", ErrUnknownField, field)
	}
}

// MarshalJSON emits the patch's value channel plus its clear channel per mode:
// null values under ClearOnNull, a $clear array under ExplicitClear — so a
// proxied or logged patch round-trips without losing clear intent. Derived
// fresh from the two channels every call (no byte-cache coherence hazard).
func (p *Patch[T]) MarshalJSON() ([]byte, error) {
	if p.err != nil {
		return nil, p.err
	}
	type kv struct {
		key string
		raw json.RawMessage
	}
	var pairs []kv
	for n, raw := range p.fields {
		pairs = append(pairs, kv{n, raw})
	}
	switch p.mode {
	case ClearOnNull:
		for n := range p.clear {
			pairs = append(pairs, kv{n, json.RawMessage("null")})
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].key < pairs[j].key })

	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(p.key)
		b.Write(key)
		b.WriteByte(':')
		b.Write(p.raw)
	}
	if p.mode == ExplicitClear && len(p.clear) > 0 {
		names := make([]string, 0, len(p.clear))
		for n := range p.clear {
			names = append(names, n)
		}
		sort.Strings(names)
		arr, _ := json.Marshal(names)
		if len(pairs) > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(ClearKey)
		b.Write(key)
		b.WriteByte(':')
		b.Write(arr)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
