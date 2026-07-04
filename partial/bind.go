package partial

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// BindOption configures a Bind call.
type BindOption func(*bindConfig)

type bindConfig struct {
	mode ClearMode
}

// WithClearMode selects the clear policy (default ClearOnNull, §2.3).
func WithClearMode(m ClearMode) BindOption {
	return func(c *bindConfig) { c.mode = m }
}

// Bind decodes a PATCH body into a Patch[T]: one source-order pass into
// per-field raw slots, wire keys normalized to canonical names via the type
// plan, the clear channel resolved per the ClearMode, and the full typed
// decode run once — so every type mismatch surfaces HERE, as a
// *ValidationError, not later in a handler. Unknown keys are ignored (they are
// not fields of T; rejecting request shape is an API-layer choice). See
// ADR-0001 §2.2.
func Bind[T any](body []byte, opts ...BindOption) (*Patch[T], error) {
	cfg := bindConfig{mode: ClearOnNull}
	for _, o := range opts {
		o(&cfg)
	}
	pl := planFor[T]() // panics if T declares a ClearKey field or pointer embed
	p := &Patch[T]{
		mode:   cfg.mode,
		fields: map[string]json.RawMessage{},
		clear:  map[string]struct{}{},
	}
	var clearRaw json.RawMessage

	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil {
		return nil, docError(err.Error())
	}
	if tok != json.Delim('{') {
		return nil, docError("expected a JSON object")
	}
	for dec.More() {
		keyTok, err := dec.Token() // keys in SOURCE ORDER
		if err != nil {
			return nil, docError(err.Error())
		}
		key := keyTok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, docError(err.Error())
		}
		// ExplicitClear reserves ClearKey by package policy, checked before
		// canonicalization; a T field can never claim it (planFor rejects that).
		if cfg.mode == ExplicitClear && key == ClearKey {
			clearRaw = v
			continue
		}
		name, ok := pl.canonical(key)
		if !ok {
			continue // unknown wire key: ignored
		}
		if isNull(v) {
			switch cfg.mode {
			case ClearOnNull:
				delete(p.fields, name) // last-key-wins across a value->null collision
				p.clear[name] = struct{}{}
			case ExplicitClear:
				delete(p.fields, name) // LM semantics: null => absent
				delete(p.clear, name)
			}
			continue
		}
		delete(p.clear, name) // last-key-wins across a clear->value collision
		p.fields[name] = v    // later canonical slot overwrites earlier
	}
	// Consume the closing '}' and reject trailing content.
	if _, err := dec.Token(); err != nil {
		return nil, docError(err.Error())
	}
	if dec.More() {
		return nil, docError("unexpected trailing content after JSON object")
	}

	if clearRaw != nil {
		if err := p.applyClearArray(pl, clearRaw); err != nil {
			return nil, err
		}
	}
	// One typed decode primes the cache and pins type errors to bind.
	if _, err := p.Data(); err != nil {
		return nil, err
	}
	return p, nil
}

// UnmarshalJSON lets a Patch[T] bind itself when it appears as a field of an
// outer decoded type — a Patch[Inner] inside a struct, or inside another
// Patch[Outer]'s T (ADR-0001 §2.7). It uses the default ClearOnNull mode: Go
// gives UnmarshalJSON no options channel, so an LM-compat nested contract must
// bind the inner type explicitly instead.
func (p *Patch[T]) UnmarshalJSON(data []byte) error {
	bound, err := Bind[T](data)
	if err != nil {
		return err
	}
	*p = *bound
	return nil
}

// BindReader is Bind over an io.Reader (sugar for http.Request.Body).
func BindReader[T any](r io.Reader, opts ...BindOption) (*Patch[T], error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, docError(err.Error())
	}
	return Bind[T](body, opts...)
}

// isNull reports whether a raw value is JSON null (ignoring surrounding
// whitespace, as the decoder trims tokens but RawMessage keeps bytes verbatim).
func isNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

// decode is the single typed-decode funnel (raw bytes -> into). Isolating it
// here keeps the future encoding/json/v2 swap local (ADR-0001 §1.4, §2.5).
func decode(raw []byte, into any) error {
	return json.Unmarshal(raw, into)
}

// bindDecodeError converts a decode error from the assembled object into a
// *ValidationError, mapping a json type mismatch to its canonical field.
func bindDecodeError(pl *plan, err error) error {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		field := te.Field
		if name, ok := pl.canonical(field); ok {
			field = name
		}
		if field == "" {
			field = "."
		}
		return &ValidationError{Fields: []FieldError{{
			Field: field, Reason: "expected " + te.Type.String()}}}
	}
	return docError(err.Error())
}
