package auth

import (
	"context"
	"errors"
	"time"

	"github.com/yongjohnlee80/golib/logger"
)

// evalCtx carries what a node needs while walking the tree.
type evalCtx struct {
	ctx   context.Context
	req   *Request
	audit *Audit
}

// NewPolicy validates a FINISHED tree and returns the only thing callers
// invoke. Validation is on the whole tree, not a node in isolation: an Any
// cannot know whether it is the root.
//
// It returns an error rather than panicking, because policies are frequently
// assembled from configuration.
func NewPolicy(root Node, opts ...PolicyOption) (Policy, error) {
	if root == nil {
		return nil, ErrEmptyPolicy
	}
	if !root.identityBearing() {
		return nil, ErrNoIdentityProof
	}
	p := &policy{root: root, log: logger.Nop{}}
	for _, o := range opts {
		if o != nil {
			o(p)
		}
	}
	return p, nil
}

type policy struct {
	root    Node
	log     logger.Logger
	observe func(Attempt)
}

// Authenticate evaluates the tree and merges the contributions.
//
// Every failure returns ErrUnauthenticated and a nil Identity; the specific
// reason goes to the returned Audit via the request's audit sink, never to the
// caller.
func (p *policy) Authenticate(ctx context.Context, r *Request) (*Identity, error) {
	a := newAudit()
	if r == nil {
		// promises exactly ONE record per authentication, so this path emits
		// one too rather than being a silent hole in the trail.
		a.note("request", errNilRequest.AuditDetail())
		p.emit(ctx, Attempt{ID: a.AttemptID, Outcome: "failure", Reasons: a.Reasons})
		return nil, ErrUnauthenticated
	}
	peer := ""
	if r.Peer.IsValid() {
		peer = r.Peer.String()
	}

	scopes, err := p.root.eval(evalCtx{ctx: ctx, req: r, audit: a})
	if err != nil {
		// auditDetail, NEVER err.Error(): a factor is third-party code, and
		// fmt.Errorf("bad token %q", presented) puts the credential into the
		// error text. Only an error that asserts safety contributes its text.
		a.note("tree", auditDetail(err))
		p.emit(ctx, Attempt{ID: a.AttemptID, Outcome: outcomeFor(err), Peer: peer, Reasons: a.Reasons})
		return nil, ErrUnauthenticated
	}
	id, err := merge(scopes)
	if err != nil {
		a.note("merge", auditDetail(err))
		p.emit(ctx, Attempt{ID: a.AttemptID, Outcome: "failure", Peer: peer, Reasons: a.Reasons})
		return nil, ErrUnauthenticated
	}
	methods := make([]string, 0, len(id.Proofs))
	for _, pr := range id.Proofs {
		methods = append(methods, pr.Method)
	}
	p.emit(ctx, Attempt{ID: a.AttemptID, Outcome: "success", Subject: id.Subject, Methods: methods, Peer: peer})
	return id, nil
}

// outcomeFor separates a backoff refusal from a credential failure IN THE LOG
// ONLY. The caller still receives the same ErrUnauthenticated; an operator
// looking at a flood of "throttled" needs to see something different from a
// flood of "failure".
func outcomeFor(err error) string {
	if errors.Is(err, ErrThrottled) {
		return "throttled"
	}
	return "failure"
}

// eval for a leaf: run the factor, then enforce the Subject rule against the
// factor's DECLARED kind so evaluation cannot disagree with the classification
// the tree was validated under.
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
	// Every branch error is JOINED, not just the last one kept.
	//
	// Keeping only the last made the outcome depend on declaration order: an
	// Any whose throttled branch was not last reported plain "failure", so a
	// backoff refusal became invisible in the log for exactly the topology where
	// it matters — the fallback policy. errors.Join lets errors.Is see every
	// branch's reason regardless of position.
	var joined error
	for _, c := range n.children {
		got, err := c.eval(ec)
		if err == nil {
			return got, nil
		}
		joined = errors.Join(joined, err)
		ec.audit.note("branch", auditDetail(err))
	}
	return nil, joined
}

// merge turns contributions into one Identity:
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
