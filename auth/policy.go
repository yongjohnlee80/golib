package auth

import (
	"context"
	"time"
)

// evalCtx carries what a node needs while walking the tree.
type evalCtx struct {
	ctx   context.Context
	req   *Request
	audit *Audit
}

// NewPolicy validates a FINISHED tree and returns the only thing callers
// invoke. Validation is on the whole tree, not a node in isolation: an Any
// cannot know whether it is the root (ADR-0001 §2.2.2).
//
// It returns an error rather than panicking, because policies are frequently
// assembled from configuration.
func NewPolicy(root Node) (Policy, error) {
	if root == nil {
		return nil, ErrEmptyPolicy
	}
	if !root.identityBearing() {
		return nil, ErrNoIdentityProof
	}
	return &policy{root: root}, nil
}

type policy struct{ root Node }

// Authenticate evaluates the tree and merges the contributions.
//
// Every failure returns ErrUnauthenticated and a nil Identity; the specific
// reason goes to the returned Audit via the request's audit sink, never to the
// caller (ADR-0001 §2.2).
func (p *policy) Authenticate(ctx context.Context, r *Request) (*Identity, error) {
	if r == nil {
		return nil, ErrUnauthenticated
	}
	a := newAudit()
	scopes, err := p.root.eval(evalCtx{ctx: ctx, req: r, audit: a})
	if err != nil {
		a.note("tree", err.Error())
		return nil, ErrUnauthenticated
	}
	id, err := merge(scopes)
	if err != nil {
		a.note("merge", err.Error())
		return nil, ErrUnauthenticated
	}
	return id, nil
}

// eval for a leaf: run the factor, then enforce the Subject rule against the
// factor's DECLARED kind so evaluation cannot disagree with the classification
// the tree was validated under (ADR-0001 §2.1).
func (n leafNode) eval(ec evalCtx) ([]scoped, error) {
	kind := n.f.Kind()
	c, err := n.f.Verify(ec.ctx, ec.req)
	if err != nil {
		// INVARIANT: an error means the zero Contribution. Discard anything a
		// misbehaving factor returned alongside it.
		return nil, err
	}
	switch kind {
	case FactorIdentity:
		if c.Subject == "" {
			return nil, errKindViolation
		}
	default:
		if c.Subject != "" {
			return nil, errKindViolation
		}
	}
	return []scoped{{c: c, kind: kind}}, nil
}

// eval for All: every child must pass, in declaration order, and every
// contribution accumulates.
func (n allNode) eval(ec evalCtx) ([]scoped, error) {
	if len(n.children) == 0 {
		return nil, errEmptyNode // an empty node denies
	}
	var out []scoped
	for _, c := range n.children {
		got, err := c.eval(ec)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

// eval for Any: the first passing branch wins; a failing branch is recorded and
// skipped.
func (n anyNode) eval(ec evalCtx) ([]scoped, error) {
	if len(n.children) == 0 {
		return nil, errEmptyNode
	}
	var last error
	for _, c := range n.children {
		got, err := c.eval(ec)
		if err == nil {
			return got, nil
		}
		last = err
		ec.audit.note("branch", err.Error())
	}
	return nil, last
}

// merge turns contributions into one Identity (ADR-0001 §2.2.1):
//
//   - every subject-bearing contribution must AGREE on Subject; disagreement is
//     a failure, not a merge — it means two different principals were proved;
//   - at least one identity-bearing contribution must be present;
//   - proofs accumulate in evaluation order;
//   - the validity interval is the INTERSECTION: IssuedAt is the LATEST
//     non-zero value, ExpiresAt the MINIMUM finite non-zero one, and a zero
//     ExpiresAt imposes no bound.
func merge(scopes []scoped) (*Identity, error) {
	if len(scopes) == 0 {
		return nil, errNoContributions
	}
	id := &Identity{Proofs: make([]Proof, 0, len(scopes))}
	haveIdentity := false
	for _, s := range scopes {
		if s.kind == FactorIdentity {
			if !haveIdentity {
				id.Subject = s.c.Subject
				haveIdentity = true
			} else if s.c.Subject != id.Subject {
				return nil, errSubjectConflict
			}
		}
		if !s.c.IssuedAt.IsZero() && s.c.IssuedAt.After(id.IssuedAt) {
			id.IssuedAt = s.c.IssuedAt
		}
		if !s.c.ExpiresAt.IsZero() {
			if id.ExpiresAt.IsZero() || s.c.ExpiresAt.Before(id.ExpiresAt) {
				id.ExpiresAt = s.c.ExpiresAt
			}
		}
		id.Proofs = append(id.Proofs, Proof{Method: s.c.Method, Kind: s.kind})
	}
	if !haveIdentity {
		return nil, ErrNoIdentityProof
	}
	if !id.ExpiresAt.IsZero() && !id.ExpiresAt.After(time.Now()) {
		return nil, errExpired
	}
	return id, nil
}
