package partial

import (
	"encoding/json"
	"fmt"
)

// ClearMode declares what "clear this field" looks like on the wire
// (ADR-0001 §2.3).
type ClearMode uint8

const (
	// ClearOnNull (default): a JSON null value means "clear the field". The
	// ecosystem-natural reading of PATCH (RFC 7386 merge-patch uses it), and
	// the fix for LM's footgun where a client null silently no-opped.
	ClearOnNull ClearMode = iota

	// ExplicitClear (LM-compat): null means absent (ignored); clears ride the
	// reserved ClearKey ("$clear") array of field names. For wire contracts
	// migrated from LM, where clients already send $clear.
	ExplicitClear
)

// ClearKey is the reserved key naming the clear array in ExplicitClear mode.
// It is reserved by PACKAGE POLICY, not by JSON construction: a "$" prefix is
// not collision-free once JSON tags join the namespace (a field may be tagged
// json:"$clear"). planFor rejects any model whose canonical name is ClearKey,
// and Bind consumes ClearKey at the document level before per-field
// canonicalization — so a model field can never intercept the clear channel,
// nor the clear channel a field (ADR-0001 §2.3).
const ClearKey = "$clear"

// applyClearArray resolves the raw $clear array (ExplicitClear mode) into the
// patch's clear channel: each entry canonicalizes through the plan, unknown
// entries are dropped (the ordinary unknown-key rule), and the value-AND-clear
// ambiguity is rejected as a *ValidationError. Duplicate entries are
// idempotent. Runs after the field pass, so it sees the final value channel.
func (p *Patch[T]) applyClearArray(pl *plan, raw json.RawMessage) error {
	var entries []string
	if err := json.Unmarshal(raw, &entries); err != nil {
		return &ValidationError{Fields: []FieldError{{
			Field: ClearKey, Reason: "must be an array of field names"}}}
	}
	var verr ValidationError
	for _, e := range entries {
		name, ok := pl.canonical(e)
		if !ok {
			continue // unknown clear entry: ignored, same as an unknown wire key
		}
		if _, present := p.fields[name]; present {
			verr.Fields = append(verr.Fields, FieldError{
				Field:  name,
				Reason: fmt.Sprintf("field %q cannot both carry a value and be cleared", name),
			})
			continue
		}
		p.clear[name] = struct{}{}
	}
	if len(verr.Fields) > 0 {
		return &verr
	}
	return nil
}
