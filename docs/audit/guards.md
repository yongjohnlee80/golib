# The guards in detail

One section per guard: what it is for, how it works, what its ledger means,
what constrains it, and — stated plainly, because a guard trusted further than
it goes is worse than none — what it cannot see.

Start at [`README.md`](README.md) for the inventory and the failure recipes.

---

## 1. The panic budget

**Files:** `panic_budget_test.go`, `testdata/panic_budget.txt`,
`testdata/panic_budget_legacy_unreviewed.txt`
**Tests:** `TestPanicBudget_InventoryMatchesTree`, `_UnreviewedIsFrozen`,
`_ViolationsDoNotGrow`, `_ClassificationIsMeaningful`

### Purpose

golib's development contract permits a panic at **construction** time — fail
early, before anything is running — and forbids one on API misuse a caller can
reach at runtime with valid types, where the contract is an error return.

That rule was written down and nothing checked it, so the tree accumulated
panic sites whose categories nobody had established. This guard enumerates
every `panic()` call in non-test code, forces each to carry a human-read
category and reason, and caps the category that breaks the contract at zero.

### What it is, precisely

Debt scaffolding **plus** a classified core — and today the scaffolding is
retired. All 165 sites carry a category a human read; the unreviewed backlog is
closed and the freeze file is empty. Calling the whole thing "classified" would
once have been too strong, and an earlier draft of the file did.

### The categories

| Category | Meaning | Count |
| --- | --- | ---: |
| `construction` | Reached only while building a value, before it is in use. Explicitly permitted | 101 |
| `invariant` | A runtime method, but the panic reports a **programmer** error no data input can cause — a nil child, an index out of range, an operation called from the wrong lifecycle phase | 60 |
| `repanic` | Re-raising a recovered panic after cleanup | 2 |
| `contract` | Reachable at runtime, and the API **documents** the panic as deliberate behaviour — opt-in, stated at the option that enables it, ratified in review | 2 |
| `unreachable` | Guards a case the type system should make impossible | 0 |
| `violation` | Reachable at runtime with valid types, where the contract should be an error return | **0 — capped** |
| `unreviewed` | Enumerated, not yet read. Permitted only for frozen legacy identities | 0 |

`contract` is not a euphemism for `violation`, and the distinction is not
academic: this file once classified `tui/queue.go`'s overflow panic as a LIVE
violation "so a library crashes its host process", from reading `queue.go`
alone. The option that enables it says *apps preferring fail-fast crash
detection over memory growth opt in here*, the default is unlimited, and
`App.Post` repeats it. A budget that cannot tell a documented opt-in from a
defect will send someone to "fix" a behaviour a consumer chose. A `contract`
row must cite **where** the panic is documented, or the loader rejects it.

A `violation` row must additionally begin `LIVE: ` or `LATENT: `. That token
replaced a numeric reachability column — see [what it cannot
see](#what-it-cannot-see).

### Identity: what makes two panics "the same site"

    file + function path + fingerprint(panic expression + guarding condition) + ordinal

The fingerprint is 16 hex digits (64 bits) of SHA-256, and its grammar is
validated on load so a truncated or hand-edited value cannot silently match
nothing.

This is the **fourth** identity design in the file. Each earlier one was
defeated by a mutation rather than by reading, and the sequence is worth
knowing because it is the argument for the current complexity:

1. **file + function + ordinal** was *positional*, not identifying. Exchanging
   two panic messages inside one function left both keys intact, so a reason
   written for one branch silently attached to the other and every arm stayed
   green. Reproduced on `dao.New`'s first two panics. That draft even carried a
   comment claiming such a swap "would no longer match" — a relation asserted
   in prose that nothing in the code observed.
2. **Adding a fingerprint of the panic expression** did not fix it. The
   exchange leaves the set of (function, expression) pairs intact, so both rows
   survive and each recorded reason still matches its own message — while that
   message now sits behind a different condition.
3. **Adding only the innermost guard** still missed outer guards, then-vs-else
   polarity, the switch subject, every expression after the first in a
   multi-expression case, and select arms. `if x { if y {…} }` and
   `if !x { if y {…} }` collided.
4. **The full structural ancestor path, with the edge taken.** Every control
   statement between the enclosing function declaration and the panic, plus a
   per-scope index for each function literal.

The principle underneath: *a category is a claim about the control path that
reaches a panic. If the path is not in the identity, the claim can migrate to
code it was never written about.*

**Line numbers are deliberately not identity.** Keying on them meant one
comment added atop `tui/app.go` churned 13 rows, and an inventory that churns
on edits nobody made teaches its readers to regenerate without reading.

**Closure indices are per scope.** A function literal is numbered among its
*siblings* — the literals whose nearest enclosing block is the same block — in
source order, 1-based. Indexing among every literal in the enclosing
declaration renumbered every later one when a closure was added anywhere
earlier; indexing within the immediate parent expression gave every literal
`$1`, because a `defer func(){}()` parent contains exactly one.

### The ledger

`testdata/panic_budget.txt`, one tab-separated row per site:

    file <TAB> func path <TAB> fingerprint <TAB> ordinal <TAB> category <TAB> reason

```
tui/context.go	Context.CapturePointer	13e438869921685b	1	invariant	…
```

`testdata/panic_budget_legacy_unreviewed.txt` freezes the identities that were
already unreviewed when the guard was adopted. It is **empty**, and that makes
the rule stronger rather than switching it off: `_UnreviewedIsFrozen` takes a
different branch when the freeze is empty, and fails on *any* unreviewed row at
all rather than tolerating the listed ones.

Two subtleties in that test, both of which were holes in earlier drafts:

- Pinning only the **count** let a new unread panic in by spending one legacy
  classification — net unchanged, every arm green. Identity, not arithmetic,
  has to be frozen.
- Checking only that unreviewed rows were a **subset** of the freeze left a
  stale grandfather slot: classify a row, leave its identity in the freeze
  file, and the slot stays available for something else later. The two sets
  must be **exactly equal**.

`maxViolations` is `0`, and it is checked in both directions — a fixed
violation that leaves the constant slack fails too, because a budget with room
in it stops constraining anything. The constant's history is kept in the source
because the number moved in both directions and only the last move was a fix:
`4 → 3` was a **reclassification** (the queue overflow, above), `3 → 4` was a
reviewed decision that traded a new panic path for not guessing an escaping
rule, and `4 → 0` was `dao.Str` being removed entirely.

### Regenerating

```sh
GOLIB_PANIC_BUDGET_UPDATE=1 go test ./internal/audit/
```

This rewrites the inventory, **carrying over** the category and reason of every
row whose identity is unchanged, marking anything new as `unreviewed`, and then
failing on purpose so you cannot regenerate and push in one motion.

**Read the diff.** A changed fingerprint means the panic expression or its
guard changed, so its recorded reason must be re-read rather than carried over.
That is not a chore the tool imposes; it is the entire point of the tool.

### What it cannot see

- **Whether a panic is correct.** It forces every site to carry a category,
  every violation to carry a verdict with evidence, and every new site to be
  classified rather than grandfathered. It does not read the code's intent.
- **Reachability.** An earlier draft carried a numeric "callers" column. It
  double-counted every qualified call, merged every unrelated symbol sharing a
  spelling (an unrelated struct field named `RunTx` moved `dao.RunTx` from 0 to
  1), and could not see consumers at all — `dao.RunTx` has no non-test
  reference inside golib and 18 in autodb. A corrupted aggregate that churns on
  unrelated edits is worse than no number, so the column is gone and its job is
  done by the `LIVE:`/`LATENT:` verdict a human records.
- It covers every `.go` file in the repository **including** the nested
  `dao/bigquery` module, which `go test ./...` from the root does not reach.

---

## 2. The panic identity controls

**File:** `panic_identity_test.go`
**Tests:** `TestPanicIdentity_Cases`, `_ClosureIndexIsPerScope`,
`_FingerprintGrammar`

### Purpose

These are the **controls for the panic budget's identity function**, committed
rather than run once in a shell. Every earlier design of that function was
defeated by a mutation nobody had written down, so the mutations live here now.

Each case states two snippets and whether they must collide.

### The two directions, and why both matter

A case expecting **different** identities guards against a category migrating
onto code it was never written about. A case expecting the **same** identity is
just as important: it pins what may be edited freely. Without those, the honest
implementation would be to hash the whole file and force a re-read on every
commit — technically sound, and it would be regenerated unread within a week.

| Must **differ** | Because |
| --- | --- |
| Outer guard flipped (`if x` → `if !x`) | Only the innermost guard used to be hashed |
| then/else swapped | The edge taken is part of the control path, not decoration |
| Switch subject changed | The tag decides which case is reached |
| Second expression of `case 1, 2:` → `case 1, 3:` | Only the *first* case expression used to be hashed |
| `default:` vs a matched case | A default arm is a different path |
| `select` arm changed | Select arms were not represented at all |
| Loop condition changed | A loop is a control ancestor like any other |
| `range` subject changed | Range's subject is part of the path |
| `panic("a  b")` vs `panic("a b")` | Whitespace normalisation reached **inside** literals and collided them |
| Message reworded | A reworded panic must retire its row so the reason is re-read |
| Panic hoisted out of a closure | A closure is its own scope; moving out changes reachability |

| Must **match** | Because |
| --- | --- |
| `if x  &&  y` vs `if x && y` | The printer normalises syntax; only literal text is preserved verbatim |
| Unrelated statements added before the panic | Line shifts must not churn rows, or people regenerate without reading |

The literal-whitespace case deserves a note. An earlier `render` ran
`\s+` → `" "` over the printed output, which reached inside string literals —
and the comment above it *claimed tokens were not normalised*, a false
statement about its own code. The fix was to delete the rewrite: `printer.Fprint`
already normalises syntactic whitespace, because it re-prints from the AST,
while a string literal keeps its raw token text.

---

## 3. The comment budget

**Files:** `comment_budget_test.go`, `testdata/comment_budget.txt`,
`testdata/comment_budget_tests.txt`
**Tests:** `TestCommentBudget` (production), `TestCommentBudget_Tests`

### Purpose

A comment that explains **why** must be readable on its own. It has to stay
true if every design record, review thread and pull request vanished, because
the next engineer reading the file has none of them open.

A comment whose content is a *pointer* — a design-record number, a section
anchor, a review round, a reviewer's name — explains nothing to that reader. It
names a document they cannot see, and it was only ever legible to its author on
the day they wrote it.

A pointer is still allowed, but as the **last line** and only to something a
reader of this repository can actually open: another file here, or public
documentation on the web. Never a private document.

### Mechanism

The guard parses every `.go` file with `parser.ParseComments`, walks the comment
groups, and counts **comment lines** carrying at least one of the 14 pointer
patterns. A line is counted once however many patterns it matches, so the
number reads as "lines to rewrite".

`//go:` directives are skipped: a directive is an instruction to a tool, not
documentation.

A file that cannot be parsed **fails the walk** rather than being skipped.
Skipping it silently would make "unparseable" a way out of this test —
including for a file that stops parsing by accident.

### Two scopes, two ledgers

| Scope | Ledger | Walks | State |
| --- | --- | --- | --- |
| `production` | `testdata/comment_budget.txt` | non-`_test.go` | empty; 307 files frozen at zero |
| `tests` | `testdata/comment_budget_tests.txt` | `_test.go` only | empty; 254 files frozen at zero |

They are separate because they were migrated separately, and folding the
finished production budget into the unfinished test one would have put a number
back into a file whose whole statement is that it has none. They are asserted
by separate tests so a failure names which half regressed.

Test comments were deliberately excluded from the production migration, on the
condition that the exclusion be **tracked rather than dropped**. This second
scope is that tracking, and it is a real ratchet rather than a note.

A pointer in a test is not automatically the same defect as one in production
code, and saying so is the argument for migrating them at all. A test comment
reading `criterion 9` or `ADR-0013 §3.1` is trying to say *which requirement
this test pins* — genuinely useful information, wearing a form the reader
cannot resolve. The migration keeps the information and drops the coordinate:
state the requirement, so the test says what it is defending instead of naming
a document that defends it.

### The ratchet

Numbers **may only fall**. The budget is **exact**, not a ceiling: a file whose
real count is *below* its budget line fails, because an improvement that is not
recorded can be silently undone later. A file that reaches zero has its line
deleted and is frozen there permanently — a file not listed in the ledger
cannot gain a pointer.

Both ledgers are now empty. Adding a line back is **undoing the migration**,
not recording work in progress.

The total is deliberately **not stored** in the ledger. It is derived data that
two migration rungs both had to touch, so it conflicted on every parallel pass
over a number neither rung actually disagreed about. The test computes and logs
it, which removes the conflict class instead of resolving it repeatedly.

### The one exemption

`internal/audit/comment_budget_test.go` itself, in the `tests` scope. Its
pointer "hits" *are* the shapes the detectors match, quoted in prose to document
what each one catches. Rewriting them would delete the documentation of the rule
to satisfy the rule.

It is held to three conditions, all checked:

1. **The file exists.** An exemption naming a missing file excuses whatever is
   written under that name next.
2. **Its premise still holds** — the file still declares the top-level
   identifier `pointerPatterns`, so it is still the file that *defines* the
   detectors rather than one that inherited the path.
3. **It is live** — the file actually carries a pointer. A clean exempted file
   fails, and the fix is to delete the entry.

Condition 2 is checked by **parsing**, not by `strings.Contains`, and that is
the whole point. The first version used `Contains`, which is vacuous for the one
file that matters: `comment_budget_test.go` declares its own exemption, so the
premise string appears in it no matter what the premise is. A mutation replacing
`pointerPatterns` with a name nothing declares still **passed**, because the
replacement was itself now written in the file being searched. A declaration
name cannot be faked by a string literal.

### What it cannot see

The count is **per file**, so deleting one pointer and adding another in the
same file nets to zero and passes. Identity-level tracking was considered and
rejected as too churn-prone for a thousand sites whose text was being rewritten
anyway. The protection that matters is the zero-freeze, and with both budgets
empty every file in the repository is now under it.

---

## 4. The comment detectors

**File:** `comment_detectors_test.go`
**Test:** `TestCommentDetectors`

### Purpose

A detector that matches nothing reports its whole class as **clean**, which is
worse than not having it: an absent instrument gets attention, a present and
blind one turns "nobody checked" into "checked and fine".

The first version of the comment budget was blind to bare review coordinates
and to criterion numbers, and reported a repository-wide total that was simply
too low.

### Mechanism

Every one of the 14 detectors must have a fixture, in **both** directions:

- **positive** — lines that must be counted, *and counted by that detector
  itself*. A line matched only by some other detector means the class is caught
  by accident and would go silent if that other detector changed.
- **negative** — lookalikes that must be counted by **no** detector.
  Sensitivity without specificity would let a detector pass by matching
  everything.

The correspondence is checked in both directions too: a detector without a
fixture is untested, and a fixture naming a detector that no longer exists is
cover for nothing.

### The detectors, and the lookalikes they must not catch

| Detector | Catches | Must not catch |
| --- | --- | --- |
| `design-record-number` | `ADR-0013`, `ADR 0075` | "the ADR process", "ADRIAN" |
| `design-record-slug` | `golib-dao-0020` | "golib-dao is the package family" |
| `section-anchor` | `§2.3`, `§ 4.1` | "section 2 of this file"; **`"§"` as test data** |
| `review-round` | `MF2`, `nit 4` | "MFA", "the knit is intentional" |
| `reviewer-as-authority` | a reviewer's name | "the gold path is the happy one" |
| `amendment-number` | `Amendment 6` | "amendments are tracked elsewhere" |
| `pull-request-number` | `PR #34` | "see issue tracker" |
| `matrix-coordinate` | `row 4:` | "the address bar row 4 of the table" |
| `review-coordinate` | `(r3)`, `r2 review` | "register r1", "r3c is a cell name" |
| `criterion-number` | `criterion 16`, `criteria 3` | "the criterion is stated above" |
| `review-must-fix` | `must-fix`, `must-fixes` | "this must fix the ordering" |
| `kb-document-citation` | `(KB convention …`, `(KB` at line end | **"a buffer of 64 KB"** |
| `kb-requirement-number` | `security-core-hardening R4/R7` | "the R4 register" |
| `document-revision` | `rev 3` | "reverse the order", "revision control" |

Two of those negatives are load-bearing rather than decorative:

- **`§` as data.** A real comment in `tui/widget` reads *an East-Asian-ambiguous
  cluster — `"§"` is width 1 by default*: the glyph is the input whose width the
  cursor arithmetic has to agree about. An early detector matched a bare `§`
  and flagged it. Rewriting that line to satisfy the detector would have deleted
  the only statement of what the test feeds in — which is exactly the cost a
  false positive carries here. The detector now requires a digit: a bare `§` is
  not a coordinate.
- **`64 KB`.** `KB` is matched only when followed by a document name, or as
  `(KB` at end of line where a citation wraps. Matching bare `KB` at end of line
  would flag a buffer size.

Each pattern is deliberately narrow. This guard's job is to be **right about
what it flags**, not to catch every possible phrasing, because a false positive
costs a reader's trust in the whole instrument.

The reviewer-name list is **closed** and the fleet grows — extend it rather
than assume it is complete.

---

## 5. The promotion self-call guard

**Files:** `promotion_test.go`, `testdata/promotion_selfcalls.txt`
**Test:** `TestPromotionSelfCalls`

### Purpose

Go has no virtual dispatch. When a method on an embeddable base type calls
another method **on its own receiver**, it calls the *base's* version — even
when the type that embedded it has overridden that method.

So a dialect that embeds a base and overrides `QuoteIdent` still gets the base's
quoting inside any base method that quotes internally. The SQL comes out quoted
by the wrong engine's rules, the build is green, every test that exercises the
overriding dialect through its own methods passes, and the defect surfaces as a
query that fails against the real database — several layers away from the line
that caused it.

This is not hypothetical. `dao.GenericDialect.BuildUpsertSuffix` quotes with its
own receiver's `QuoteIdent`, and two of the four dialects override `QuoteIdent`.
It is harmless today only because those same two also override
`BuildUpsertSuffix`, so they never reach the base's body. It is one override
away from being a real bug, and nothing in the compiler or the test suite would
say so.

**The fix at each site** is to take the collaborator as a **parameter** rather
than reach for the receiver — pass the dialect, so the caller's own
`QuoteIdent` does the work. `dao.StandardUpsertSuffix` is the worked example.

### Live versus latent

- **LIVE** — some embedder overrides the callee but *inherits* the caller. That
  embedder is running the wrong method today. A live finding **fails
  regardless of the allowlist**, because an allowlist is for accepted debt and a
  live defect is not debt.
- **LATENT** — correct now, wrong the moment someone overrides one more method.
  Listed in the allowlist rather than forgotten.

### The signature check

An embedder overrides the callee only when its signature **matches**. A
same-name method with a different signature is a wrapper that supplies extra
arguments, and the base's self-call cannot resolve to it. Matching on name alone
reported those wrappers as defects — which it did, on `TextArea.cellsAt`, before
this was fixed.

### The ledger

`testdata/promotion_selfcalls.txt`, one line per distinct
`file <TAB> Base.caller -> callee`. Currently 20 keys covering 24 call sites
(one caller can reach the same sibling more than once):

- `tui/container_multi.go` — 5
- `tui/stack.go` — 2
- `tui/widget/textbuffer.go` — 13

None is live. The list may only **shrink**, and it is exact: a line whose site
is gone fails, so an improvement has to be recorded in the same change.

---

## 6. The routing documentation audit

**File:** `routing_docs_test.go`
**Tests:** `TestRoutingDocsDescribeTheCurrentModel`,
`TestRoutingDocsDoNotClaimRawHandlerFirst`

### Purpose

The architectural documents drifted silently for three layers. The events
tutorial still told readers that every key goes straight to `HandleEvent` and
bubbles when that returns `false`, which stopped being true the moment
resolvers landed. Nothing failed, because nothing checked. A reader following
it wrote widgets against a routing model the runtime no longer had.

### Mechanism

This is deliberately a **vocabulary check**, not a prose review. It cannot tell
whether an explanation is good; it can tell whether a surface still mentions the
stage it is required to describe, which is the failure that actually happened.

Each entry names a document, the terms it must carry, and — in a `why` field
printed on failure — the reason *that* surface is responsible for *those* terms.

| Surface | Must mention |
| --- | --- |
| `tui/doc.go` | `HandleAction`, `Activatable`, `resolver`, `gesture`, `CaptureRaw`, `CaptureGesture`, `not a third transport lane` |
| `tui/README.md` | the same — it carries the same architecture diagram and must not contradict it |
| `tui/tutorial/04-events-focus-keys.md` | `HandleAction`, `Activatable`, `resolver`, `Pointer policy`, `gesture recogniser`, `CaptureRaw`, `CaptureGesture` |
| `tui/widget/doc.go` | `Button`, `tui.Activatable`, `ActivationAvailability`, `ControlActivatedEvent` |
| `tui/widget/README.md` | the same — it carries the same inventory |

The match is **case-insensitive**. These are prose surfaces and the documents
legitimately emphasise with capitals (`NOT a third transport lane`); a
case-sensitive check would fail a document that says the right thing, which is
worse than not checking. The first version of this guard was case-sensitive and
did exactly that.

The second test bans the **specific sentence that was wrong, in the specific
place it was wrong**, so the correction cannot be reverted by someone restoring
the older and simpler-sounding explanation:

```
"Every key event goes to the **focused** node first. If its `HandleEvent`"
```

### What it cannot see

Everything about quality. A document can satisfy every term and still explain
the model badly, or mention `CaptureRaw` in a sentence that gets it backwards.
The guard defends against *silent omission* after a routing change, which is
the drift that occurred; it is not a substitute for reading the document.

---

## 7. The comments-only check

**File:** `commentsonly_test.go`
**Test:** `TestCommentsOnlyChange` — **opt-in, skips by default**

### Purpose

A comment migration must not touch code. That sounds like something you can
simply be careful about; it is not. Rewriting comments at scale is done with a
tool, and a tool that operates on a slightly wider unit than intended edits code
without anyone noticing:

- operating on whole **lines** rather than the text after `//` rewrites code
  that happens to contain the pattern;
- "tidying" empty parentheses turns `backend.Events()` into `backend.Events` —
  it compiles, and it reads almost right;
- operating **line by line** on a construct that wraps across lines leaves half
  of it behind, and a balance check still passes because the remainder is
  balanced.

All three happened during the tui migration. The first was caught by the
compiler, the second by a parenthesis count someone thought to run, the third
only by reading the output — and it still shipped three fragments
(`concern.2)`, `log.8.2)`) that a reviewer found.

### Mechanism

It catches all three in one pass by asking the only question that matters:
**strip every comment from both revisions, and the remaining code must be
byte-identical.** If it is not, the tool touched code, whatever it was aiming
at.

Stripping is done by parsing *without* `ParseComments` and re-printing from the
AST, so formatting differences cannot produce a false positive either.

### Why it is opt-in

It is a **migration tool**, not a guard. It needs a base revision to compare
against and there is no meaningful default for one, so it skips unless told:

```sh
COMMENTS_ONLY_BASE=origin/main go test ./internal/audit/ -run TestCommentsOnlyChange -v
```

Run it before pushing any comment-migration change, in any repository — it
depends on nothing golib-specific.

An empty diff is a **failure**, not a pass: `no .go files differ from <base>;
there is nothing to check, which is not a pass`. Added and deleted files are
skipped, since there is nothing to compare them against.
