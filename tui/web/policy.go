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
// # Password is a TICKET MINTER, not an attach mechanism (rev 11)
//
// A password policy belongs on [Config.LoginPolicy], not [Config.Policy]. The
// login route authenticates it and returns a single-use ticket; the WebSocket
// then attaches with that ticket like any other client. See [Handler.ServeLogin]
// for why that shape is better than putting a password in the attach path — in
// short, the attach path keeps ONE credential shape, the password crosses once to
// a route that does nothing else, lockout lives where the guessing happens, and a
// captured hello is worth a spent ticket rather than a reusable secret.
//
// So the policy this helper builds is the LOGIN policy. Putting it on
// Config.Policy would make a password an attach credential, which is the thing
// the split exists to prevent.
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
	// The constraint is REQUIRED here, not merely recommended.
	//
	// The helper's whole purpose is to be the recommended shape, and it
	// previously accepted zero constraints while its documentation described one
	// — so it produced a policy weaker than it claimed (lector r1). A caller who
	// genuinely has no contextual factor should compose [RecommendedPolicy]
	// directly and own that decision explicitly, rather than get it from a
	// function whose name says the constraint is present.
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
		// accepted an identity factor, which would satisfy the Any on its own and
		// so add a second way in rather than narrowing the first — the opposite
		// of what this parameter promises (lector r2).
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
	// Password goes LAST so that a client presenting a stronger credential is
	// authenticated by it, and the weak path is only reached when nothing else
	// was offered.
	return RecommendedPolicy(append(append([]auth.Factor(nil), stronger...), throttled), constrain...)
}
