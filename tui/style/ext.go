package style

// Package style's extensibility subsystem provides third-party property attachment
// and composable functional options while preserving value semantics, zero-allocation
// fast paths, and struct comparability.
//
// # What Ext Solves
//
// Core terminal styling encompasses standard ANSI/ECMA-48 attributes (color, weight,
// margins, borders). However, modern terminals and advanced TUI frameworks frequently
// introduce specialized features: OSC 8 hyperlinks, curly/dotted underline styles,
// custom drop shadows, mouse cursor behaviors, or protocol-specific graphics flags.
//
// Directly adding fields to [Style] for every emerging terminal capability would:
//  1. Bloat the memory footprint of every cell/style in the application.
//  2. Degrade cache locality and performance on core layout/render paths.
//  3. Risk breaking struct comparability (==) if slices or functions were added.
//
// The extensibility system solves this tension via two decoupled mechanisms:
//  - [ExtKey] & [Style.Ext]: A namespaced, copy-on-write dynamic property map.
//  - [StyleOption] & [Style.Apply]: A composable, batching functional option seam.
//
// In standard use (where extensions are not touched), [Style] carries only a single
// nil pointer with zero heap allocations and zero render overhead.
//
// # The O(N²) Chaining Problem vs. O(1) Batching via Apply
//
// Because [Style] is an immutable value type, calling .Ext() naively in a fluent chain
// must clone the internal map on every invocation to prevent mutating prior styles:
//
//	// Naive chaining: O(N²) map allocations!
//	st = st.Ext(k1, v1).Ext(k2, v2).Ext(k3, v3)
//
// To eliminate this overhead, [Style.Apply] implements a batching state machine:
//
//	                 st.Apply(opt1, opt2, opt3)
//	                            │
//	                            ▼
//	                 ┌──────────────────────┐
//	                 │  c.extraMode =       │
//	                 │       extBatch       │
//	                 └──────────┬───────────┘
//	                            │
//	              First Ext(k, v) within Apply?
//	                            │
//	                            ▼
//	                 ┌──────────────────────┐
//	                 │ Clone map ONCE;      │
//	                 │ set extOwned         │
//	                 └──────────┬───────────┘
//	                            │
//	             Subsequent Ext(k, v) in Apply:
//	                            │
//	                            ▼
//	                 ┌──────────────────────┐
//	                 │ Mutate in place      │
//	                 │ (safe: copy is owned)│
//	                 └──────────┬───────────┘
//	                            │
//	                            ▼
//	                 ┌──────────────────────┐
//	                 │ Reset to extCOW;     │
//	                 │ return modified copy │
//	                 └──────────────────────┘
//
// Through this mechanism, N extensions applied inside a single Apply call share exactly
// ONE map clone.
//
// # Complete Usage Example
//
//	package hyperlink // third-party extension package
//
//	import "github.com/yongjohnlee80/golib/tui/style"
//
//	var urlKey = style.ExtKey{Pkg: "github.com/acme/hyperlink", Name: "target-url"}
//
//	// WithURL creates a composable StyleOption for setting a hyperlink target.
//	func WithURL(url string) style.StyleOption {
//		return func(s *style.Style) {
//			*s = s.Ext(urlKey, url)
//		}
//	}
//
//	// GetURL extracts the hyperlink target in custom renderers.
//	func GetURL(s style.Style) (string, bool) {
//		v, ok := s.GetExt(urlKey)
//		if !ok {
//			return "", false
//		}
//		str, ok := v.(string)
//		return str, ok
//	}
//
//	// Consumer usage:
//	st := style.New().
//		Foreground(style.TokenPrimary).
//		Underline(true).
//		Apply(
//			hyperlink.WithURL("https://example.com"),
//			tuix.WithUnderlineColor(style.RGB(255, 0, 0)),
//		)
type StyleOption func(*Style)

// ExtKey namespaces third-party extra properties. It is a flat comparable struct.
//
// Best Practice: Pkg should be set to the owning module/package import path
// (e.g. "github.com/author/pkg") to eliminate namespace collisions between
// independent plugins.
type ExtKey struct{ Pkg, Name string }

// extraProps wraps the third-party property map behind the one pointer
// field Style carries. The pointer compares by identity: two styles
// differing only in equal-but-distinct extras maps compare unequal.
type extraProps struct {
	m map[ExtKey]any
}

// cloneWith returns a copy of e's map with k=v applied.
func (e *extraProps) cloneWith(k ExtKey, v any) *extraProps {
	m := make(map[ExtKey]any, len(e.m)+1)
	for kk, vv := range e.m {
		m[kk] = vv
	}
	m[k] = v
	return &extraProps{m: m}
}

// Apply runs opts against a copy — the value-semantics-preserving extension
// seam, and the documented batching path for multiple extensions: every
// option receives the SAME working copy, so N Ext calls inside one Apply
// share a single extras clone instead of Ext-chaining's O(N²) copy-on-write:
//
//	st.Apply(tuix.WithUnderlineColor(c), tuix.WithHyperlink(url))
//
// Nil options are skipped.
func (s Style) Apply(opts ...StyleOption) Style {
	c := s
	c.extraMode = extBatch
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	c.extraMode = extCOW
	return c
}

// Ext sets an extra property and returns the modified copy. Copy-on-write:
// the extras map is cloned, never mutated in place, preserving value
// semantics — the receiver and every prior copy are untouched. Chaining N
// Ext calls is therefore O(N²); batch multiple extensions through
// [Style.Apply] instead (one clone per Apply).
func (s Style) Ext(k ExtKey, v any) Style {
	c := s
	switch {
	case c.extras == nil:
		c.extras = &extraProps{m: map[ExtKey]any{k: v}}
	case c.extraMode == extOwned:
		// Inside Apply and already cloned for this working copy: mutate in
		// place. Safe — no other Style can hold this pointer.
		c.extras.m[k] = v
		return c
	default:
		c.extras = c.extras.cloneWith(k, v)
	}
	if c.extraMode == extBatch {
		c.extraMode = extOwned // first Ext inside Apply: the clone is now owned
	}
	return c
}

// GetExt reads one extra property.
func (s Style) GetExt(k ExtKey) (any, bool) {
	if s.extras == nil {
		return nil, false
	}
	v, ok := s.extras.m[k]
	return v, ok
}
