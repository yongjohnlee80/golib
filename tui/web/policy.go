package web

import (
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/auth"
)

// RecommendedPolicy composes the shape ADR-0009 §2.8 recommends: any ONE
// identity-bearing mechanism, optionally constrained by a contextual factor.
//
// Pass nil for any mechanism you do not use. At least one is required — an empty
// policy is a construction error from [auth.NewPolicy], not a permissive
// default.
//
// The `constrain` factors are contextual (an IP allowlist, say). They can NARROW
// who may attempt but can never satisfy the policy alone; ADR-0001 §2.2.2
// enforces that structurally, so a mistake here is a construction error rather
// than a quiet weakening.
func RecommendedPolicy(mechanisms []auth.Factor, constrain ...auth.Factor) (auth.Policy, error) {
	leaves := make([]auth.Node, 0, len(mechanisms))
	for _, f := range mechanisms {
		if f != nil {
			leaves = append(leaves, auth.Leaf(f))
		}
	}
	if len(leaves) == 0 {
		return nil, auth.ErrNoIdentityProof
	}
	root := auth.Any(leaves...)
	if len(constrain) > 0 {
		all := make([]auth.Node, 0, len(constrain)+1)
		for _, f := range constrain {
			if f != nil {
				all = append(all, auth.Leaf(f))
			}
		}
		if len(all) > 0 {
			root = auth.All(append(all, root)...)
		}
	}
	return auth.NewPolicy(root)
}

// PasswordPolicyExample builds the LOGIN policy for password authentication.
//
// Password is permitted and is the weakest supported mechanism (ADR-0009 §2.8
// rev 9, reshaped in rev 11). What sits behind the credential is a shell, so:
//
//   - the password factor is wrapped in [auth.Throttle], because a reusable
//     secret with no backoff is an online guessing attack waiting to happen;
//   - it is constrained by a CONTEXTUAL factor, so an allowlist narrows who may
//     even try. A contextual factor cannot satisfy a policy alone — ADR-0001
//     §2.2.2 enforces that structurally — so this narrows without adding a way
//     in. An identity factor is refused here for exactly that reason.
//
// # This builds Config.LoginPolicy, not Config.Policy
//
// It takes no "stronger mechanisms" parameter. An earlier version did, and the
// arms it accepted were unreachable: [Handler.ServeLogin] projects only a subject
// and a password, so a ticket, certificate or SSH signature placed in this policy
// could never be presented to it (lector r3). Tickets, mTLS and SSH belong on
// [Config.Policy] via [RecommendedPolicy]; this is the front door that mints the
// ticket that policy accepts.
//
//	login, err := web.PasswordPolicyExample(passwordFactor, tracker, ipAllowlist)
//	attach, err := web.RecommendedPolicy([]auth.Factor{ticket, mtls, sshChallenge})
//
// This package does not refuse a weaker policy — the policy is the caller's to
// compose, and a package that silently second-guessed it would be lying about
// where the decision lives. It refuses only to pretend a weaker shape is
// equivalent, and to hand back a shape whose name promises more than it does.
func PasswordPolicyExample(
	password auth.Factor,
	tracker auth.Tracker,
	constrain ...auth.Factor,
) (auth.Policy, error) {
	// The constraint is REQUIRED, not merely recommended: this helper's purpose
	// is to be the recommended shape, and a version that accepted none produced
	// a policy weaker than its name promised. A caller who genuinely has no
	// contextual factor should compose [RecommendedPolicy] directly and own that
	// decision explicitly.
	if len(constrain) == 0 {
		return nil, errors.New("web.PasswordPolicyExample: a contextual constraint is " +
			"required — use RecommendedPolicy directly to build an unconstrained " +
			"password policy deliberately")
	}
	for _, f := range constrain {
		if f == nil {
			return nil, errors.New("web.PasswordPolicyExample: nil constraint")
		}
		// The constraint must actually be CONTEXTUAL. Checking only for non-nil
		// accepted an identity factor, which would satisfy the policy on its own
		// and so add a second way in rather than narrowing the first — the
		// opposite of what this parameter promises (lector r2).
		if f.Kind() != auth.FactorContextual {
			return nil, fmt.Errorf("web.PasswordPolicyExample: %T is %v, not a contextual "+
				"factor — a constraint must narrow who may attempt, not add another "+
				"way in", f, f.Kind())
		}
	}
	throttled, err := auth.NewThrottle(password, tracker)
	if err != nil {
		return nil, err
	}
	return RecommendedPolicy([]auth.Factor{throttled}, constrain...)
}
