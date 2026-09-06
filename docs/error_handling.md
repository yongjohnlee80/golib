# Error handling in golib

How errors are produced, wrapped, named, compared and recovered in this
repository. It applies to every package here, and to code that consumes golib.

**The one-sentence version:** an error's **identity** is its contract and its
**message** is prose — so compare with `errors.As` or `errors.Is`, never with
text, and a message may be reworded at any time without breaking anyone.

> This document is **complete on its own** — nothing in it requires another
> source, and you need nothing but this repository to follow it. It is also
> maintained as a workspace-wide convention outside this repo, so if you change
> a rule here, say so in the pull request: the two are kept in step deliberately
> and a one-sided edit is a defect rather than a difference.

---

## The canonical shape

```go
return fmt.Errorf("dial %s after %s: %w", addr, elapsed, errs.ErrTimeout)
```

- The **message** says what happened, specifically, for a person reading a log:
  which address, how long. Write it for a human. It may change tomorrow.
- The **wrapped sentinel** says what it *is*, for code that must act:
  `errors.Is(err, errs.ErrTimeout)` is true whatever the prose says.

A good error does both. A message with no identity forces callers to match
text; an identity with no context tells a human nothing about which call failed.

---

## Layered errors

A condition several packages share lives in `errs`. A package with its own
version of it **defines that by wrapping the shared one**:

```go
// package errs — the shared condition, and nothing about who is closed
var ErrClosed = errors.New("closed")

// package tui — the specific condition, layered on the base
var ErrBackendClosed = fmt.Errorf("(%w: backend is stopped)", errs.ErrClosed)

// package widget — a DIFFERENT condition, same base
var ErrViewClosed = fmt.Errorf("(%w: buffer view closed)", errs.ErrClosed)
```

A caller may now ask either question, and both answers are exact:

| question | answer |
|---|:--:|
| `errors.Is(err, tui.ErrBackendClosed)` | **true** — the specific question |
| `errors.Is(err, errs.ErrClosed)` | **true** — the general question |
| `errors.Is(err, widget.ErrViewClosed)` | **false** — a different condition stays different |

All three hold however deeply the error is wrapped afterwards, and two siblings
sharing a base never answer for each other. This is what lets the four distinct
"closed" conditions in this repository — a finished transaction, a closed RPC
client, a stopped terminal backend, an unmounted view — each keep their own
identity *and* gain a shared one, instead of choosing between a single name
that over-matches and four unrelated names that under-match.

### The base goes in brackets

```
one wrap    : term: write to /dev/tty: (closed: backend is stopped)
three wraps : app: render: session 4: term: write to /dev/tty: (closed: backend is stopped)
```

Everything before the bracket is **where**; everything inside it is **what**.
Keep a base sentinel's own message terse — `"closed"`, not `"errs: closed"` —
because it appears inside the bracket of every error layered on it.

### Prefer one or two layers; three is the maximum

```
errs.ErrClosed                                 layer 1 — often enough on its own
  └── tui.ErrBackendClosed                     layer 2 — the usual case
        └── tui.ErrBackendClosedDuringResize   layer 3 — allowed when needed; the maximum
```

**One layer is a real answer.** If nothing package-specific would change a
caller's behaviour, return `errs.ErrClosed` directly rather than adding a
near-identical name to the API.

**Three is allowed** where a caller genuinely must distinguish a sub-case — do
not contort a design to avoid it. **A fourth is the wrong tool**: the hierarchy
is doing work that a typed error's *fields* should do.

Separately, **context wraps go at boundaries, not at every frame.** A frame that
adds nothing should return the error unchanged, or five callers produce five
near-identical prefixes and the reader learns nothing from four of them.

---

## Where an error belongs

One question decides it:

> **Could a second, unrelated package produce this same condition?**

- **Yes → `errs`.** A broken contract, an unsupported operation, something not
  implemented, a bad argument, an unmet precondition, a closed thing, a timeout.
  **Network and transport conditions are the family most likely to qualify** —
  many packages do I/O, and *closed*, *timed out*, *refused* mean the same thing
  in all of them. A per-package `ErrTimeout` forces a caller that handles
  timeouts to enumerate packages.
- **No → the package that owns the concept.** `dao.ErrTxRolledBack` is a fact
  about `dao`; nothing outside it can mean that.

**Peers only — a consumer wraps, it does not promote.** The word *unrelated* is
load-bearing: the two packages must be **peers**, neither importing the other.

- **Peers.** `dao` and `tui` can both be *closed* and neither owns the other, so
  the base belongs in `errs` and each wraps it.
- **Consumer and producer.** If package A imports package B and refuses for the
  same reason B would, A's refusal *is B's condition seen one layer up*. **A
  wraps B's sentinel.** Nothing moves to `errs`.

Promoting a consumer-shared condition puts it in a package that knows nothing
about the domain, and a reader must then look in `errs` to find out what a
downstream package meant. Applied to peers only, `errs` stays a handful of
conditions; applied to everything shared, it becomes a registry.

**Test the condition, never the name.** Two packages using the same *word* for
different situations must stay separate — consolidating them would make
`errors.Is` answer true across unrelated failures, which is worse than the
duplication it appears to fix.

---

## The rules

1. **Return errors. Panic only for a broken contract** — a caller doing what the
   API documents as illegal, where continuing would corrupt state rather than
   merely fail. Anything reachable through valid use is a returned error.

2. **Wrap with `%w`. Never `%v`.** `%v` renders the wrapped error into text and
   destroys everything `errors.As` would have returned — while leaving any
   sentinel wrapped alongside it answering `errors.Is` **true**. The check
   passes, the caller concludes nothing was lost, and the payload is gone.
   *The failure looks like success.*

3. **Anything a caller might branch on has an exported identity** — a sentinel
   or a type, declared once in the package that owns the concept. An
   `errors.New` inline at a `return` produces an error nobody can name, so the
   only way to react to it is to match its text.

4. **Compare with `errors.As` for typed errors** — it *recovers the value*,
   which is the whole reason the type exists — and **`errors.Is` for
   sentinels**. Never with text, in production *or* in tests.

5. **Translate third-party error text at the boundary, once.** A driver's
   `"duplicate column"` becomes a first-party sentinel where that driver is
   adapted — never matched at a call site deep in the program.
   `dao.Dialect.TranslateError` is the model.

6. **A panic carries a value, not a bare string,** wherever a `recover()` sits
   above it on some call path:
   ```go
   panic(&errs.Fatal{Op: "tui: Mount", Rule: "tree mutation inside Layout or Render"})
   ```
   and whatever recovers it uses `errors.As` to read the fields. Rule 2 applies
   hardest here: a recovered value flattened with `%v` loses everything.

7. **Cross-repository errors live upstream.** One source of truth; never
   re-declared in a consumer. If two packages must share a sentinel, one
   **assigns the other's value** — `var A = pkg.B` — never a second
   `errors.New`, which would make `errors.Is` answer false across the pair.

8. **The message adds context; it does not restate the identity.**
   ```go
   fmt.Errorf("migrate %s: %w", name, ErrLocked)   // yes — what was being done
   fmt.Errorf("locked: %w", ErrLocked)             // no  — says what the sentinel says
   ```
   Information a caller *needs* goes in a field, not only in the message.

9. **Do not log and return.** Handle it or return it; doing both puts one
   failure in the record twice and a reader cannot tell it was one event.

10. **A message may be reworded at any time.** That freedom is what identity
    buys — and code that breaks when a message changes was already wrong.

---

## Reviewing

- `err.Error()` anywhere but logging, formatting or a user-facing message is a
  finding.
- A new `strings.Contains(err.Error(), …)` is a finding — **including in
  tests**. Tests are downstream too.
- **A negative assertion on message text is the worst case.** When the text
  changes it does not merely become unreliable, it **inverts**: it stops
  proving anything and starts passing silently, so a real regression reports
  green. A positive assertion at least fails loudly.
- An inline `errors.New` at a return that a caller might branch on is a finding.
- Inherited violations are recorded as an **exact per-file budget that may only
  fall** — not a total, which lets an improvement be silently undone, and not a
  hard failure on existing debt, which gets the check disabled. New violations
  fail; inherited ones ratchet.

### Finding the couplings that are real

Before rewriting assertions, measure which of them actually depend on someone
else's text. **A substring hit is not a coupling.** `agent:gold-man`'s first
instrument grepped an upstream's whole source for each matched string and
reported fourteen — including `"cap"` in 186 files, `"server"` in 127 and
`"SET"` in 26. That would have meant rewriting forty assertions to fix four
problems that did not exist.

**The right test: does the matched text appear inside a string the upstream
DECLARES as an error** — an `errors.New` or `fmt.Errorf` — rather than anywhere
in its source? Measured that way against golib's 257 distinct declared error
texts, autodb's 40 distinct matched strings yielded **two** real couplings, both
the same sentinel, and two coincidences where ordinary English appeared in an
unrelated message.

**And separate the two problems, because they have different urgency:**

- **A cross-repo coupling breaks on SOMEONE ELSE'S rewording.** You cannot see
  it coming and cannot prevent it. Fix these first; there are usually very few.
- **An internal coupling breaks on YOUR OWN rewording.** It is still a
  violation of R3/R4 and still worth fixing, but you control when it breaks.
  This is the large, less urgent half.

Presenting them as one number makes the urgent handful invisible inside the
patient majority.

## Do not

- Match on a third-party library's or a driver's message anywhere but a boundary
  adapter.
- Export a message constant so callers can compare against it. That is the same
  coupling with extra steps — export the **error**.
- Flatten a wrapped error with `%v` when any caller might want it back.
- Put a document reference inside an error or panic message. A string literal is
  code, and a pointer at something the reader cannot open helps nobody.
