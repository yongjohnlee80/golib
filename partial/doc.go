// Package partial models an HTTP PATCH body as a presence-aware, three-state
// payload for a model type T (ADR-0001).
//
// A PATCH field is one of three states — it carries a value, it is omitted, or
// it is asked to be cleared — a distinction encoding/json collapses (an
// omitted field and an explicit null both leave the zero value). Patch[T]
// tracks that state out of band from the typed struct.
//
// Bind parses a body once into per-field raw slots keyed by the canonical
// effective JSON name (from a cached per-T plan), validates at bind time, and
// primes one typed decode. What "clear" looks like on the wire is a declared
// ClearMode: ClearOnNull (default — a JSON null clears) or ExplicitClear
// (LM-compat — null is absent, clears ride a reserved "$clear" array).
//
// Server code mutates the patch with Set/Clear/Remove/Only (last mutation
// wins over a clear; unknown field names are loud). Rules projects the patch
// into a Write/Skip/Clear disposition per field; ApplyRules (the only
// dao-importing surface) stages that onto a dao.DAO via ADR-0010's SetRules —
// end-to-end PATCH with zero per-entity code.
package partial
