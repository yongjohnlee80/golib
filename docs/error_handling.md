# Error handling in golib

How errors are produced, wrapped, named, compared and recovered in this
repository. It applies to every package here, and to code that consumes golib.

**The one-sentence version:** **error comparison and detection must never rely
on message text.** An error's *identity* — a sentinel or a type — is its
contract; its *message* is prose for a person. Compare with `errors.As` or
`errors.Is`, and a message may then be reworded at any time without breaking
anyone.

**The message must still be excellent.** `Error()` should say exactly what
failed, where, and with what values. These are not a trade-off, and a terse
message is not a step toward identity — it is just a worse message. Never strip
detail from an error to discourage matching on it; the fix for text-matching is
at the comparison, never at the message.

> This document is **complete on its own** — nothing in it requires another
> source, and you need nothing but this repository to follow it. It is also
> maintained as a workspace-wide convention outside this repo, so if you change
> a rule here, say so in the pull request: the two are kept in step deliberately
> and a one-sided edit is a defect rather than a difference.

---

## The canonical shape

```go
return errs.Wrap(errs.ErrTimeout, "dial %s after %s", addr, elapsed)
// dial 10.0.0.1:5432 after 3s (timeout)
```

`errs` provides three templates, and between them they cover every shape in this
document. **Use them rather than retyping the format** — a format that is
retyped at every call site is a format that drifts.

| | for | produces |
|---|---|---|
| `errs.Wrap(base, format, args…)` | a call site reporting a failure | `where, specifically (what)` |
| `errs.WrapCause(base, cause, format, args…)` | the same, when an underlying error must stay recoverable | `where (what): cause` |
| `errs.Sentinel(base, detail)` | declaring a package's layered condition | `what: detail` |

`Sentinel` carries **no brackets of its own** — the bracket belongs to `Wrap`,
which puts exactly one pair around whatever identity a call site reports,
however deep the layering goes. All three **panic on a nil base**, because an
error with no identity is the one thing this package exists to prevent, and
building one quietly would hide the mistake at the only moment it is cheap to
find.

### Wrapping is not required

A downstream package is under **no obligation** to wrap. If your message would
say nothing the base does not already say, **return the base**:

```go
return ErrNoIdentity                            // yes
return errs.Wrap(ErrNoIdentity, "no identity")  // no — says it twice
```

A wrapper that restates its base costs a layer, a line of output and a name, and
tells the reader nothing. The message earns its place with the part the identity
cannot carry: which address, which file, which id. Either return the upstream
error, or add something — never wrap for the sake of wrapping.

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

### Two layers is the ceiling here; the third belongs to the consumer

```
errs.ErrClosed                         layer 1 — often enough on its own
  └── tui.ErrBackendClosed             layer 2 — the ceiling IN THIS REPOSITORY
        └── app.ErrRenderAborted       layer 3 — the CONSUMER's, if they want it
```

**One layer is a real answer.** If nothing package-specific would change a
caller's behaviour, return `errs.ErrClosed` directly rather than adding a
near-identical name to the API.

**Two is the ceiling in golib.** A library cannot know what distinctions its
consumers need, and a third layer added here is a guess at a decision that
belongs downstream — one that becomes API the moment it ships. Wanting a third
*inside* golib means the hierarchy is doing work a typed error's *fields* should
do.

**The consumer owns the third layer**, and chooses freely between:

```go
// wrap — keeps our identity and adds theirs; both questions answer true
var ErrRenderAborted = fmt.Errorf("(%w: render aborted)", tui.ErrBackendClosed)

// map by assignment — adopts our identity under their name, adding no layer
var ErrShutdown = tui.ErrBackendClosed
```

Assignment is an *alias*, not a redeclaration (rule 7): `errors.Is` answers true
in both directions. A second `errors.New` would answer **false**. Their own
error reaching three layers is fine — the budget is per-repository.

The **two-layer bound is Johno's ruling**; the identity-vs-context distinction
that follows is the maintainer's reading of it, not a second ruling — and the
bound is **a review rule, not an instrument**, since no test enforces depth.

Separately, **context wraps go at boundaries, not at every frame.** A frame that
adds nothing should return the error unchanged, or five callers produce five
near-identical prefixes and the reader learns nothing from four of them.

---

### Siblings or a refinement? Identity follows behaviour

When one condition looks like a special case of another, ask **whether the
caller's permitted next action differs**. If it does, they are siblings over the
shared base; if not, the second name is not needed at all.

The worked case, from `parse`: unfinished input and wrong input are both "the
source is not acceptable", so making `ErrUnterminated` a refinement of
`ErrSyntax` looks natural. It is wrong — unfinished input is **resumable** (send
more text) and a syntax error is not. If unterminated also answered `ErrSyntax`,
an interactive caller's *give up on syntax errors* handler would fire on input
that merely needed another line.

The cost of siblings — "any bad input" takes two questions — is a convenience
cost the shared base already absorbs, since `errs.ErrInvalidArgument` **is** that
question in one `Is` call.

The reading that produces a refinement describes the *source*. Identity is for
the *caller*, so it follows behaviour, not taxonomy.

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
   panic(errs.Fatal{Op: "tui: Mount", Rule: "tree mutation inside Layout or Render"})
   ```
   and whatever recovers it uses `errors.As` to read the fields. Rule 2 applies
   hardest here: a recovered value flattened with `%v` loses everything.

   **Spell the `errors.As` target as a value**, because rule 6b makes these
   value types and the usual `*T` idiom then fails **silently**:
   ```go
   var f errs.Fatal;  errors.As(err, &f)   // yes — matches
   var f *errs.Fatal; errors.As(err, &f)   // NO  — false, with no error
   ```

6b. **An error type is a VALUE type — give it value receivers.** `Error()` must
   never be reachable on a nil reference, and the way to guarantee that is to
   leave no reference to be nil. With a pointer receiver, `*T` is the only
   spelling that implements `error`, which makes the typed nil the *easy*
   mistake: `var e *T; return e` yields a non-nil `error` holding a nil pointer,
   so `err != nil` is true, `Error()` panics, and `Is` answers for a value
   nobody constructed. Guarding each method against nil hides that state behind
   a caveat; a value receiver removes it, and the zero value renders as prose.
   Pin it with a compile-time assertion — `var _ error = errs.Fatal{}` stops
   compiling the moment someone switches back.

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

9. **Avoid `errors.Join` unless you need several independent failures.** A
   joined error answers `errors.Is` true for *every* branch at once, so *"what
   is this?"* stops having one answer and identity dispatch becomes
   order-dependent; `errors.As` returns only the first match and drops the rest.
   Legitimate for a cleanup that must attempt every step and report all of them.
   Never as a way to attach a sentinel to a cause — that is one error with an
   identity and a cause, and `%w` already says it:
   ```go
   fmt.Errorf("close %s: %w", name, errs.ErrClosed)  // yes — one identity
   errors.Join(errs.ErrClosed, cause)                // no  — two, ambiguous
   ```

10. **Do not log and return.** Handle it or return it; doing both puts one
   failure in the record twice and a reader cannot tell it was one event.

11. **A message may be reworded at any time.** That freedom is what identity
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

A better instrument asks whether the matched text appears inside a string the
upstream **declares** as an error. That narrowed 14 apparent couplings to 2.

**It was also wrong.** Writing the fix and running it against a live target
showed the error at those sites was the consumer's own — a `fmt.Errorf` wrapping
the underlying failure, never carrying the upstream sentinel. The words matched
because two layers independently described the same condition in the same
English: a far more convincing false positive than a common word, because the
text matched for a real reason and still meant nothing.

**A static instrument can only ever tell you what the library DECLARES.**
Whether *this* error carries that identity is decided by what the code
constructs at runtime, so no amount of refinement gets there — the second
instrument was strictly better than the first and was still the wrong *kind* of
tool. That is the useful form of the lesson, because "my pattern was too broad"
would have suggested a narrower pattern.

**The settling move is to EXECUTE the dependency, not to match on it.** Three
lines against a live target, and it failed in under a second:

```go
if !errors.Is(err, dao.ErrTxRolledBack) { t.Errorf(…) }
```

**And beware the shape of the false positive.** It matched for a real reason and
still meant nothing: two layers independently described the same condition in
the same English. That is not noise — it is *convergent design*, and it is
exactly the coincidence that survives every filter you build. **The better your
filter, the more confident you are when one gets through.**

The honest count was **zero**. Assume your first number is too high until
something has run.

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
