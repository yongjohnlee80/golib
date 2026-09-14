# The `internal/audit` guards

`internal/audit` holds repo-wide guards that assert properties of the **source
tree itself** rather than the behaviour of any one package. It is test-only: it
exports nothing and is imported by nothing. Its whole output is a pass or a
failure in the suite everybody already runs.

**The one-sentence version:** a rule that is written down and checked by nobody
is not a rule, it is a preference — these guards are the difference.

Every guard here exists because a rule was already written down, was already
agreed, and the tree drifted from it anyway. None of them was invented
speculatively. Each section in [`guards.md`](guards.md) names the drift that
produced it.

> This document is complete on its own. Everything it refers to is in this
> repository and can be opened from here.

## Why they are Go tests

They could have been CI scripts. They are tests because:

- They run in the suite everyone already runs — `go test ./...` — with no new
  pinned CI step to keep in sync and no second place to look when something
  fails.
- They add no dependency. Every guard is standard-library-only, mostly
  `go/ast` and `go/parser`.
- A failure arrives in the same shape as every other failure, with a message
  that says what to do about it.

## The inventory

| Guard | Test(s) | Asserts (Mechanical Check) | Ledger |
| --- | --- | --- | --- |
| [Panic budget](guards.md#1-the-panic-budget) | `TestPanicBudget_*` (4) | Non-test `panic()` calls match human-reviewed inventory; rows classified `violation` capped at zero | `testdata/panic_budget.txt`, `testdata/panic_budget_legacy_unreviewed.txt` |
| [Panic identity controls](guards.md#2-the-panic-identity-controls) | `TestPanicIdentity_*` (3) | Identity hashing function distinguishes control paths and ignores benign edits | — |
| [Comment budget](guards.md#3-the-comment-budget) | `TestCommentBudget`, `TestCommentBudget_Tests` | Comments contain zero non-exempt matches for the 14 known external coordinate regex patterns; ledgers match exactly | `testdata/comment_budget.txt`, `testdata/comment_budget_tests.txt` |
| [Comment detectors](guards.md#4-the-comment-detectors) | `TestCommentDetectors` | Each of the 14 pointer detectors fires on a real example and stays silent on a lookalike | — |
| [Promotion self-calls](guards.md#5-the-promotion-self-call-guard) | `TestPromotionSelfCalls` | Zero LIVE sibling self-calls on embeddable base receivers; LATENT debt bounded to allowlisted keys | `testdata/promotion_selfcalls.txt` |
| [Routing docs](guards.md#6-the-routing-documentation-audit) | `TestRoutingDocs*` (2) | Monitored doc surfaces contain required routing vocabulary and omit obsolete sentence claiming raw handlers run first | — |
| [Comments-only change](guards.md#7-the-comments-only-check) | `TestCommentsOnlyChange` | A comment migration changed zero executable syntax by AST-stripped byte comparison. **Opt-in; skips by default** | — |

Snapshot of guard census at exact commit `ba602e2`:

```
production comment budget: 0 pointer lines in 0 files (destination: 0), 307 files walked
tests comment budget:      0 pointer lines in 0 files (destination: 0), 254 files walked
panic census:              165 sites across 55 files
categories:                construction=101 contract=2 invariant=60 repanic=2 (165 of 165 classified)
violations:                0 (0 LIVE, 0 LATENT)
unreviewed:                0 — the backlog is closed
promotion self-calls:      24 sites, 20 allowlisted keys, 0 live
```

## Running them

```sh
go test ./internal/audit/            # all of them, as CI does
go test ./internal/audit/ -v         # with the census and budget totals
go test ./internal/audit/ -run TestPanicBudget
```

Two are not ordinary guards and take an environment variable:

```sh
# Re-enumerate the panic sites after adding, moving or rewording one.
GOLIB_PANIC_BUDGET_UPDATE=1 go test ./internal/audit/

# Before pushing a comment-only migration: prove no code moved.
COMMENTS_ONLY_BASE=origin/main go test ./internal/audit/ -run TestCommentsOnlyChange -v
```

## Three anti-vacuity patterns, used where applicable

These are not stylistic preferences, but patterns applied where applicable (fixture-only guards like comment detectors and panic identity controls need neither ledgers nor exemptions). Each one is the fix for a way an earlier draft of a guard reported a clean tree while the tree was dirty:

**1. It fails vacuously loudly.** Every guard driven by a directory walk (`panic_budget`, `comment_budget`, `promotion_test`) asserts a floor on what the walk found, because a walk that finds nothing reports a clean repository:

```go
if walked < sc.minWalked {
    t.Fatalf("%s scope: walked only %d source files, expected at least %d; the "+
        "walk is broken and this budget would report a clean repository", ...)
}
```

The panic census does the same (`"census found no panic sites at all — the instrument is broken, not the tree clean"`), and the comments-only check refuses to pass on an empty diff (`"there is nothing to check, which is not a pass"`).

**2. Its ledger is exact, not a ceiling (with review-enforced ratchet).** A budget file records the number a file currently has, and the guard fails when the real number is **lower** as well as higher:

```
FAIL: path/to/file.go: 2, budgeted 4 — lower it to 2
```

Exact matching is mechanically enforced by the test: an improvement that is not recorded in the same change fails until the ledger is lowered. However, the requirement that counts **may only fall** is a **binding code-review policy** rather than an automated git-history check, because tests compare against the file on disk. Deleting a line freezes that file at zero forever.

**3. Its exemptions are by name, held to a premise, and checked for liveness.** A shape-based excuse ("any comment containing a quoted pointer") is one a future site can satisfy by accident. Where an exemption exists (currently only in `comment_budget` for `comment_budget_test.go`), an exemption must:
- Name the exact relative file path.
- State a **premise identifier** declared as a top-level AST symbol in that file (`pointerPatterns`), proving it still defines the detectors.
- Be **live**: the file must still carry a violation; an exemption with nothing left to exempt fails the test until removed.


## What to do when one fails

Every guard's failure message states the remedy. In order of what usually
happened:

| Failure | Almost always means | Do |
| --- | --- | --- |
| `N panic site(s) missing from testdata/panic_budget.txt` | You added a panic, or **reworded an existing one** so its fingerprint changed | Classify it. Regenerate, then read the diff and write the reason |
| `N inventory row(s) point at sites that no longer exist` | You removed or moved a panic | Regenerate and read the diff |
| `N file(s) are frozen at zero and are no longer clean` | A comment you wrote cites something the reader cannot open | Rewrite the comment to state the rule. Do not add a budget line |
| `N budget line(s) no longer match` | You improved a file and did not record it | Lower the number, or delete the line if it reached zero |
| `N new self-call(s) on an embeddable base` | A base method reached for its own receiver | Take the collaborator as a parameter |
| `<doc> does not mention "<term>"` | You changed routing and the docs did not follow | Update the document, not the term list |

The one failure that is **not** a request to update a ledger:

```
N LIVE promotion defect(s) — an embedder is running the base's method today
```

That is a real defect in shipped code. The allowlist does not cover it and
cannot be made to: an allowlist is for accepted debt, and a live defect is not
debt.

## Adding a guard

The bar is not "this would be nice to check". It is:

1. **Name the drift that already happened.** Every guard in this package cites
   the specific defect that motivated it, usually with the file and the shape.
   A guard with no incident behind it tends to encode its author's taste.
2. **Prove the instrument observes.** Write the fixtures — the thing it must
   catch *and* the lookalike it must ignore. `comment_detectors_test.go` and
   `panic_identity_test.go` exist for exactly this, and both were written
   *after* an earlier version of their guard was found to be blind.
3. **Give it a vacuity floor (if walking the tree).** A guard that searches
   directories must assert a minimum threshold on discovered files or census
   items, so a broken path or parser failure cannot report a falsely clean tree.
4. **Make its ledger exact (if ledger-backed).** A ledger must assert exact
   equality with recorded counts. Reductions must be committed immediately, and
   increases are forbidden by review policy. (Fixture-only controls require
   neither ledgers nor exemptions).
5. **Mutation-test it.** A guard is code, and a guard that passes when its
   subject is broken is worse than no guard. See
   [`verification-discipline.md`](verification-discipline.md).

## Related guards that live elsewhere

Not everything of this kind is in `internal/audit`. Two guards live with the
package they constrain, because they need that package's types:

- **`dao/dialect_surface_test.go`** freezes the `Dialect` interface to its six
  methods, and bans capability predicates on **any exported interface** — a
  method matching `^(Supports[A-Z]|Has[A-Z]|Is[A-Z]\w*Supported)|Supported$|Enabled$`.
  A capability is an interface you type-assert, never a boolean every
  implementor has to answer. Despite living in `dao/`, that second check walks
  the **whole repository** (with the same vacuity floor, `walked < 100`), which
  is why it is what forced `tui/widget`'s `ActivationEnabled` to become
  `ActivationAvailable`. The ban is scoped to interface *methods* on purpose:
  the same spelling as a free function is the blessed discovery form —
  `SupportsIntrospection` and friends all probe a type assertion, and the
  difference is that a method makes every implementor answer while a function
  asks the type system.
- **`cmd/covercheck`**, run by the `coverage policy` CI job, is a coverage gate
  rather than a source-tree guard, but it belongs in the same mental category:
  a mechanical check standing in for a rule nobody would otherwise enforce. Its
  thresholds are in [`verification-discipline.md`](verification-discipline.md#the-coverage-gate).
