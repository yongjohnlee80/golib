// Package auth answers one question — does this request carry a valid
// credential? — and returns an [Identity] or an error. It is not an
// authorization, session, or user-management library (ADR-0001 §3).
//
// # The model
//
// A [Factor] is one leaf check. It returns a [Contribution], not an Identity,
// because a contextual factor such as an address allowlist has no identity to
// return. Factors enter a tree through [Leaf]; [All] and [Any] compose them;
// [NewPolicy] validates the FINISHED tree and returns the [Policy] a caller
// invokes. Nothing else is exported as a composition surface, and [Node] is a
// closed interface, which is what lets NewPolicy reason about the whole tree.
//
// # Contextual factors cannot authenticate alone
//
// Every factor declares a [FactorKind]. A composite's kind is COMPUTED: an All
// is identity-bearing if ANY child is (the others constrain it), while an Any is
// identity-bearing only if EVERY branch is, since one contextual branch would be
// a way in. NewPolicy rejects a root that is not identity-bearing, so
//
//	NewPolicy(Any(Leaf(ipallow), Leaf(sshkey)))   // error: IP alone would admit
//	NewPolicy(All(Leaf(ipallow), Leaf(sshkey)))   // fine: both required
//	NewPolicy(Any(Leaf(mtls), All(Leaf(ipallow), Leaf(challenge))))  // fine
//
// The rule is enforced by construction rather than by documentation.
//
// # Merging
//
// Subject-bearing factors in an All must AGREE; disagreement is a failure, not a
// merge, because it means two different principals were proved. Proofs
// accumulate in evaluation order. The validity interval is the INTERSECTION:
// IssuedAt is the latest contributing value, ExpiresAt the minimum finite
// non-zero one, and a zero ExpiresAt imposes no bound — so a static observation
// such as an allowlist match does not shorten anything.
//
// # Failure is uniform
//
// Every failure returns [ErrUnauthenticated] and a nil Identity. A more specific
// outward error would tell an attacker which factor failed; the detail goes to a
// private [Audit] record carrying a correlation ID, so an operator can diagnose
// a user's report from the attempt ID.
//
// # Secrets
//
// [Secret] redacts structurally: String, Format (every verb), MarshalJSON and
// MarshalText all return a placeholder, so no formatting or encoding path can
// print credential material. No claim is made that bytes are erased from
// memory — Go offers no such guarantee.
package auth
