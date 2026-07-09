package style

// Extensibility (ADR-0006 §2.7): StyleOption hooks plus one extras escape
// hatch. Third parties cannot add typed fields to Style; these two
// mechanisms cover them without taxing the core — the render path never
// consults extras, and styles that never touch Ext carry one nil pointer
// and zero overhead.

// StyleOption composes third-party style transformations into a fluent
// chain. Options are applied via [Style.Apply]; an option receives Apply's
// private working copy and may use the full fluent surface on it
// (*s = s.Bold(true), *s = s.Ext(k, v), ...).
type StyleOption func(*Style)

// ExtKey namespaces third-party extra properties. Comparable. Pkg should be
// the owning import path (e.g. "acme/tuix") so independent packages never
// collide.
type ExtKey struct{ Pkg, Name string }

// extraProps wraps the third-party property map behind the one pointer
// field Style carries. The pointer compares by identity (§2.1): two styles
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
