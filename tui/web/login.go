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
//   - **The attach path stays one credential shape.** With a minter, every
//     attach presents a single-use ticket, whatever the user actually proved.
//     Mixing a reusable secret into the same message as a spent one means the
//     replay properties of an attach depend on which field was populated.
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
// It is the ONLY unauthenticated endpoint in the package, so it is written as
// one: Origin and Host guarded like every other route, a bounded body, one
// uniform refusal, and no statement anywhere about whether the subject exists.
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

	var attemptID string
	ctx := auth.WithAttemptSink(r.Context(), func(a auth.Attempt) { attemptID = a.ID })
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
