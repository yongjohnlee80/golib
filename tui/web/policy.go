package web

import (
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

// PasswordPolicyExample documents the ONLY shape in which password auth should
// reach a WebTUI, as executable code rather than prose.
//
// Password is permitted and is the weakest supported mechanism (ADR-0009 §2.8,
// rev 9). What sits behind the credential is a shell, so:
//
//   - the password factor is wrapped in [auth.Throttle], because a reusable
//     secret with no backoff is an online guessing attack waiting to happen;
//   - it is constrained by a contextual factor, so an allowlist narrows who may
//     even try;
//   - it is the FALLBACK arm of an Any, not the front door.
//
// Compose it yourself if you need something different — this package does not
// refuse a weaker policy, because the policy is the caller's to compose and a
// package that silently second-guessed it would be lying about where the
// decision lives. It does refuse to pretend the weaker shape is equivalent.
//
//	// password is *password.Factor; tracker is an auth.Tracker.
//	throttled, err := auth.NewThrottle(password, tracker)
//	if err != nil {
//	    return err
//	}
//	policy, err := web.RecommendedPolicy(
//	    []auth.Factor{ticket, mtls, sshChallenge, throttled}, // fallback, last
//	    ipAllowlist,                                          // narrows, cannot admit
//	)
func PasswordPolicyExample(
	password auth.Factor,
	tracker auth.Tracker,
	stronger []auth.Factor,
	constrain ...auth.Factor,
) (auth.Policy, error) {
	throttled, err := auth.NewThrottle(password, tracker)
	if err != nil {
		return nil, err
	}
	// Password goes LAST so that a client presenting a stronger credential is
	// authenticated by it, and the weak path is only reached when nothing else
	// was offered.
	return RecommendedPolicy(append(append([]auth.Factor(nil), stronger...), throttled), constrain...)
}
