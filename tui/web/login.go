package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/auth/token"
	"github.com/yongjohnlee80/golib/logger"
)

// Login turns a weak credential into the standard one.
//
// # Why a separate route rather than a password in the WS hello
//
// The obvious implementation is a form whose values go into the hello alongside
// the ticket, and it is worse in four ways that all point the same direction:
//
//   - **A password never becomes an attach credential.** The attach path accepts
//     tickets, verified certificate chains and SSH signatures; a password
//     converts into the first of those and is never presented to it directly.
//     The ticket it converts into is single-use and expires in 30 seconds, and
//     it is accepted only behind the separately-enforced Origin allowlist — the
//     ticket itself carries no origin binding, and earlier revisions of this
//     comment wrongly said it did (lector r5).
//     (An earlier version of this comment said every attach presents a ticket,
//     which is simply untrue — mTLS and the SSH challenge attach on their own.
//     The real invariant is narrower and is the one that matters: a reusable
//     secret is not among the things the attach path will accept — lector r4.)
//   - **The password crosses once, to a route that does nothing else.** It never
//     touches session creation, frame delivery or the event stream, so no bug in
//     those paths can be reached while a password is in flight.
//   - **Lockout lives where the guessing happens.** The throttle wraps the login
//     policy, so backoff is per-login rather than entangled with the attach
//     policy that mTLS and SSH signatures also use.
//   - **A captured hello cannot contain a password.** The worst a replayed hello
//     yields is a spent ticket.
//
// This is a change in shape from ADR-0009 §2.8's original phrasing, which
// contemplated password as an attach mechanism. Recorded as rev 11.
//
// # What this route must therefore be
//
// It is the only unauthenticated endpoint in the package that PROCESSES A
// CREDENTIAL — the page is unauthenticated too, but it takes nothing and returns
// nothing sensitive — so it is written accordingly: Origin and Host checked IN
// THIS HANDLER as well as by [Handler.Guard], a bounded body, one uniform
// refusal, and no statement anywhere about whether the subject exists.
//
// The internal check is not redundancy. An earlier version relied entirely on
// Guard, which [Handler.Mount] applies — so a caller who mounted the exported
// handler directly got an unguarded login endpoint, and a direct call with an
// attacker Host and Origin minted a ticket for a correct password (lector r4).
// The doc comment claimed the route itself carried those controls; it did not.
// For an endpoint that turns a password into a credential, the check belongs
// where the handler is, not only where someone remembered to wrap it.
type loginRequest struct {
	Subject  string `json:"subject"`
	Password string `json:"password"`
}

type loginResponse struct {
	// Ticket is a single-use, short-lived credential for the WS attach. It is
	// returned in a RESPONSE BODY rather than a redirect or a URL, so it never
	// enters browser history, a Referer, or an access log.
	Ticket string `json:"ticket"`
}

// AttemptHeader carries the authentication attempt ID back to the client.
//
// The value is random per attempt and says nothing about the outcome or the
// account, which is what makes it safe to hand out — and given that every
// refusal is otherwise identical, it is the only thing a user can quote to an
// operator.
const AttemptHeader = "X-Auth-Attempt"

// maxLoginBody bounds the request. A login body is two short strings; anything
// larger is not a login attempt.
const maxLoginBody = 4 << 10

// loginTicketTTL is how long a minted ticket may be redeemed.
//
// Deliberately tiny: the client uses it on the next line of JavaScript. A longer
// window buys nothing and widens the period in which a leaked response body is
// worth stealing.
const loginTicketTTL = 30 * time.Second

// ServeLogin authenticates a password and mints an attach ticket.
//
// Enabled only when [Config.LoginPolicy] and [Config.Issuer] are both set;
// otherwise it 404s, so a deployment that did not ask for password auth does not
// expose a login endpoint at all.
func (h *Handler) ServeLogin(w http.ResponseWriter, r *http.Request) {
	hardeningHeaders(w.Header(), "-")
	w.Header().Set("Cache-Control", "no-store")

	// The handshake controls, applied HERE and not only by Guard. See the type
	// comment: without this, mounting the exported handler directly yields an
	// unguarded login endpoint.
	if err := h.cfg.checkHandshake(r); err != nil {
		logHandshakeDenial(h.log, r, err)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if h.cfg.LoginPolicy == nil || h.cfg.Issuer == nil {
		// Not "403 disabled": a deployment without password auth should look
		// like one that never had the route.
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// http.MaxBytesReader, and then EXACTLY ONE value followed by EOF.
	//
	// io.LimitReader plus a single Decode was not a bound at all: Decode stops at
	// the end of the first JSON value, so a correct-password object followed by
	// 8 KiB of junk decoded fine, never hit the limit, and minted a ticket
	// (lector r3). MaxBytesReader makes the read itself fail past the cap, and
	// requiring EOF makes trailing bytes a rejection rather than something nobody
	// looked at.
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBody)
	dec := json.NewDecoder(r.Body)
	var req loginRequest
	if err := dec.Decode(&req); err != nil {
		h.denyLogin(w, r, "", errors.New("malformed login body"))
		return
	}
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		h.denyLogin(w, r, "", errors.New("trailing content after the login body"))
		return
	}
	if req.Subject == "" || req.Password == "" {
		// Refused without consulting the policy: an empty field is not an
		// attempt, and running the policy on one would put a pointless entry in
		// the lockout counters.
		h.denyLogin(w, r, "", errors.New("incomplete login"))
		return
	}

	// The pending-login budget, checked BEFORE the credential is verified.
	//
	// Parked logins bound a different resource than live sessions (§2.12.4), so
	// they get their own budget: counting them against MaxSessions is what made a
	// reconnect at full capacity impossible, since a reconnect must log in before
	// it can reattach.
	if h.pending != nil && !h.pending.enter() {
		logger.Notice(h.log, sessionAudit{Kind: "login-denied", Reason: "pending-login limit"})
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return
	}
	// The slot this request holds. It stays anonymous unless a handoff ends up
	// owning it, in which case whoever settles that handoff returns it — see
	// gate.hold. The previous version simply stopped returning the slot on a
	// successful login and nothing ever gave it back, so the ninth login 503'd
	// forever (lector r1 on PR #14).
	//
	// leased is one-way: once a handoff owns this slot the KEY is the sole unit of
	// accounting, and gate.release is idempotent per key. Flipping it back would
	// let the deferred leave() decrement a slot another request had meanwhile
	// taken.
	leased := false
	if h.pending != nil {
		defer func() {
			if !leased {
				h.pending.leave()
			}
		}()
	}

	var attemptID string
	ctx := auth.WithAttemptSink(r.Context(), func(a auth.Attempt) { attemptID = a.ID })
	// A per-REQUEST slot for state the credential check produces. Verify holds the
	// credential and is the only place that can allocate upstream state, but the
	// handoff is not known until the ticket is minted below — so the move happens
	// in two steps within one request rather than through a shared key (§2.12.3).
	stash := &Stash{}
	ctx = withStash(ctx, stash)
	authReq := &auth.Request{
		Peer: peerFromRequest(r),
		Credentials: map[string]auth.Secret{
			"subject":  auth.NewSecret(req.Subject),
			"password": auth.NewSecret(req.Password),
		},
		Metadata: map[string][]string{},
	}
	if v := r.Header.Values("Origin"); len(v) > 0 {
		authReq.Metadata["Origin"] = v
	}

	// Anything the credential check allocated is released unless it is parked.
	//
	// A factor allocates during Verify because that is the only place holding the
	// credential, but the login can still fail afterwards — a later factor in the
	// policy refuses, the ticket fails to mint, the park is full. Each of those
	// used to return having allocated an upstream session nothing would ever close.
	// A no-op once the value has been taken into a park.
	defer stash.discard()

	identity, err := h.cfg.LoginPolicy.Authenticate(ctx, authReq)
	if err != nil {
		h.denyLogin(w, r, attemptID, err)
		return
	}

	secret, err := h.cfg.Issuer.Issue(identity.Subject, loginTicketTTL, true)
	if err != nil {
		logger.Warning(h.log, err, sessionAudit{Kind: "login", Subject: identity.Subject,
			Reason: "ticket issue failed"})
		// The credential was correct, so this is our failure, not theirs — and it
		// must not read as a rejected password.
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	// The handoff is derived from the ticket, so this package stores nothing and
	// only the ticket holder can compute it (§2.12.1).
	handoff := HandoffID(secret.Reveal())
	if h.onLogin != nil {
		// THE LEASE GOES IN FIRST, before the hook that parks.
		//
		// A parked entry can be settled the instant it exists — by its own expiry
		// timer, by a Sweep, by a concurrent Close. If that settle lands before the
		// lease is installed it finds no key and does nothing, and the lease
		// installed a moment later is a ghost: no park entry will ever settle it, so
		// it sits on the budget until the backstop expiry. Lector r3 on PR #14
		// reproduced exactly that — parked=0, held=1.
		//
		// Installing first inverts the race harmlessly: a settle that arrives early
		// finds the key and returns the slot, and the rollback below is idempotent.
		if h.pending != nil {
			leased = h.pending.hold(handoff, h.now().Add(h.pendingHold))
		}
		if err := h.onLogin(handoff, identity, stash); err != nil {
			// The caller could not record the login, so the login did NOT succeed.
			// Returning the ticket anyway would hand out a credential for state
			// that does not exist — so the ticket is REVOKED before the refusal.
			// Leaving it in the store left a usable credential behind a 503
			// (lector r1 on PR #14).
			if rerr := h.cfg.Issuer.Revoke(secret.Reveal()); rerr != nil {
				logger.Warning(h.log, rerr, sessionAudit{Kind: "login-denied",
					Subject: identity.Subject, Reason: "ticket revoke failed"})
			}
			// The hook failed, so nothing is parked and nothing will ever settle
			// this handoff: return the lease here. Idempotent — a settle may already
			// have returned it — and `leased` deliberately stays true so the
			// deferred leave() cannot decrement a second time.
			if leased {
				h.pending.release(handoff)
			}
			logger.Warning(h.log, err, sessionAudit{Kind: "login-denied",
				Subject: identity.Subject, Reason: "handoff not recorded"})
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		// A hook that took nothing parked nothing — the stock auth/password factor
		// cannot stash — so no settle will ever come for this handoff and the lease
		// goes back now rather than waiting out the backstop.
		if leased && !stash.claimed() {
			h.pending.release(handoff)
		}
	}
	logger.Info(h.log, sessionAudit{Kind: "login", Subject: identity.Subject, ID: attemptID})

	w.Header().Set("Content-Type", "application/json")
	// The response body carries a credential, so nothing may cache it — already
	// set above, restated here because this is the one response where it matters.
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(loginResponse{Ticket: secret.Reveal()})
}

// denyLogin returns the one uniform refusal.
//
// Every failure — malformed body, unknown subject, wrong password, locked out —
// produces an identical status and body. The attempt ID is the only varying part,
// and it is random and outcome-independent, so it says nothing about which of
// those happened while still giving a user something to quote.
func (h *Handler) denyLogin(w http.ResponseWriter, r *http.Request, attemptID string, cause error) {
	logger.Notice(h.log, sessionAudit{Kind: "login-denied", ID: attemptID,
		Reason: cause.Error()})
	_ = r
	if attemptID != "" {
		w.Header().Set(AttemptHeader, sanitizeHeader(attemptID))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
}

// compile-time proof the issuer surface used here is the token package's.
var _ = (*token.Issuer)(nil)
