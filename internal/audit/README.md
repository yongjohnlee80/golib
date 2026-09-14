# The `internal/audit` Guards and Tree Integrity Framework

> **Canonical Architecture Documentation**: The canonical repository-level design narrative and deep-dive documentation lives in [`docs/audit/`](../../docs/audit/README.md). This document serves as the package-local developer runbook, guard inventory, and pipeline reference for `internal/audit`.

`internal/audit` holds repository-wide structural guards that assert invariants of the **source tree itself** rather than the runtime behaviour of any single package. It is test-only: it exports nothing and is imported by nothing. Its whole output is a pass or a failure in the standard test suite (`go test ./...`) executed during local development and CI/CD pipelines.

**The one-sentence version:** A rule that is written down and checked by nobody is not a rule, it is a preference — these guards are the mechanical difference.

Every guard in this package exists because an architectural rule was agreed, written down, and the repository drifted from it anyway. None of them was invented speculatively.

---

## Architecture & CI/CD Pipeline Flow

The guards run as part of the standard `go test ./...` invocation in CI/CD (such as GitHub Actions) and local development. They analyze the codebase using the Go standard library (`go/parser`, `go/ast`, `go/token`) without external dependencies.

```
       Developer Commit / CI/CD Push
                     │
                     ▼
          go test ./... (All Packages)
                     │
                     ├───────────────────────────────┐
                     ▼                               ▼
            Package Unit Tests             internal/audit Guards
      (Behavioral execution tests)         (Source tree structural assertions)
                                                     │
         ┌───────────────────────────────────────────┴───────────────────────────────────────────┐
         │                                                                                       │
         ▼                                           ▼                                           ▼
   Panic Budget & Census                    Comment Budget & Ratchet                    Promotion & Architecture
   - panic_budget_test.go                   - comment_budget_test.go                    - promotion_test.go
   - panic_identity_test.go                 - comment_detectors_test.go                 - routing_docs_test.go
   [Verifies inventory consistency &        [Scans for 14 known pointer                 [Verifies method shadowing &
    caps violation rows at 0]                regexes; ratchets ledger to 0]              routing vocabulary presence]
         │                                           │                                           │
         └───────────────────────────────────────────┼───────────────────────────────────────────┘
                                                     │
                                                     ▼
                                        All Invariants Hold?
                                        ├── YES ──► CI PASS (Build proceeds)
                                        └── NO  ──► CI FAIL (Clear remediation recipe)
```

---

## Why They Are Go Tests

They could have been bash scripts or custom CI linters. They are standard Go tests because:

1. **Integrated Developer Loop**: They run automatically whenever an engineer or CI runner invokes `go test ./...`. There is no auxiliary linter toolchain to install, configure, or keep in sync.
2. **Zero Dependencies**: Every guard is built strictly with the Go standard library (`go/ast`, `go/parser`, `go/printer`, `go/token`, `regexp`, `crypto/sha256`).
3. **Actionable Failure Diagnostics**: Failures arrive in standard `testing.T` reporting format, accompanied by an explicit, actionable diagnosis stating the exact remedy.

---

## The Inventory of Guards

| Guard | Test File(s) | Primary Assertion (Mechanical Check) | Ledger / Testdata | Trigger / Env Var |
| --- | --- | --- | --- | --- |
| **Panic Budget** | `panic_budget_test.go` | Enumerates non-test `panic()` call sites; reconciles inventory against ledger; checks category consistency and enforces that rows classified `violation` are capped at zero | `testdata/panic_budget.txt`<br>`testdata/panic_budget_legacy_unreviewed.txt` | Standard / `GOLIB_PANIC_BUDGET_UPDATE=1` |
| **Panic Identity Controls** | `panic_identity_test.go` | Asserts that the structural panic identity hasher distinguishes divergent control paths while remaining invariant under benign edits (whitespace, comments, unrelated statements) | None (In-code fixtures) | Standard |
| **Comment Budget** | `comment_budget_test.go` | Scans comments for 14 known external coordinate regex patterns; asserts exact match against ledgers ratcheting toward zero | `testdata/comment_budget.txt`<br>`testdata/comment_budget_tests.txt` | Standard |
| **Comment Detectors** | `comment_detectors_test.go` | Proves all 14 pointer regex detectors fire on positive examples and stay silent on negative controls | None (In-code fixtures) | Standard |
| **Promotion Self-Calls** | `promotion_test.go` | Scans embeddable base methods for sibling self-calls on own receiver; forbids LIVE defects and enforces ratcheted LATENT allowlist (currently 24 sites across 20 allowlisted keys) | `testdata/promotion_selfcalls.txt` | Standard |
| **Routing Documentation** | `routing_docs_test.go` | Asserts required routing vocabulary is present across monitored doc surfaces and one obsolete sentence claiming raw handlers run first is absent | None (Scans doc surfaces) | Standard |
| **Comments-Only Check** | `commentsonly_test.go` | Asserts that a comment migration modified zero executable Go code by AST-stripping comments and comparing byte-for-byte against a base revision | None (Compares git tree) | `COMMENTS_ONLY_BASE=<rev>` (Opt-in) |

---

## Three Anti-Vacuity Patterns, Used Where Applicable

Not every guard uses all three patterns — fixture controls (`comment_detectors_test.go`, `panic_identity_test.go`) have neither ledgers nor exemptions. These patterns are deployed where applicable to prevent silent test erosion and vacuous passes:

### 1. Fails Vacuously Loudly (`minWalked`)
Guards driven by directory walks (`comment_budget`, `panic_budget`, `promotion_test`) enforce a minimum threshold of discovered source files (`minWalked`). A broken walk path or misconfigured filter that matches zero files would otherwise report a falsely clean tree:

```go
if walked < sc.minWalked {
    t.Fatalf("%s scope: walked only %d source files, expected at least %d; "+
        "the walk is broken and this budget would report a clean repository", ...)
}
```

Similarly, the panic census fails if no panic sites are discovered, and the comment-migration check refuses to pass on an empty git diff.

### 2. Exact Ledger Matching vs. Review-Only Ratchet
Ledger-backed guards assert **exact equality** with recorded counts, rather than a ceiling. If an improvement reduces the count in a file, the guard fails until the ledger is lowered:

```
FAIL: path/to/file.go: 2, budgeted 4 — lower it to 2
```

Exact equality is mechanically enforced by the test. However, the requirement that numbers **"may only fall / shrink"** is a **binding code-review policy** rather than an automated git-diff check: the tests compare against the ledger on disk, so code review enforces that contributors do not raise counts or re-add lines.

### 3. Named Premise / Liveness Exemptions
Exemptions are never granted by generic patterns. Where exemptions exist (specifically in `comment_budget` for `comment_budget_test.go`):
- **Named by exact path**: The exemption identifies the exact relative file path.
- **Bound to a premise identifier**: Requires a top-level AST symbol (`pointerPatterns`) declared in that file, verifying the file still defines the detectors.
- **Liveness-checked**: The exempted file must still trigger a violation. If it becomes clean, the guard fails until the dead exemption is deleted.

---

## Detailed Guard Specifications

### 1. Panic Budget (`panic_budget_test.go`)

Enforces golib's API contract: panics are permitted at construction time (fail-fast initialization before anything is active) and for catastrophic programmer invariants. Panics are forbidden for runtime API misuse where an error return is expected.

#### Panic Categories
- `construction`: Reached only during value construction before runtime use (e.g. invalid layout constraint or configuration).
- `invariant`: Internal method consistency check for programmer errors (e.g. nil child, unmounted node access).
- `repanic`: Re-raising a recovered panic after cleanup.
- `contract`: Deliberate opt-in panic documented explicitly in the API contract.
- `unreachable`: Code branches mathematically impossible under the type system.
- `violation`: **Capped at 0**. Runtime panics reachable with valid types where an error return belongs.
- `unreviewed`: Legacy unclassified backlog (**Closed at 0**).

#### Structural Identity
A panic site's identity is defined as:
```
file + function path + SHA-256 fingerprint(panic expression + AST control ancestors) + ordinal
```
Function literals receive per-scope ordinal segments (`$1`, `$2`) so moving a panic across closure boundaries alters its identity. Line numbers are excluded to prevent churn on unrelated comments or code shifts.

```
       Source File AST
             │
             ▼
     Find panic(...)
             │
             ├── Enclosing Function & Scope Path ($1, $2)
             ├── AST Control Ancestors (if, else, switch, select)
             └── Panic Expression Syntax
                     │
                     ▼
          SHA-256 Fingerprint (64-bit Hex)
                     │
                     ▼
             Structural Identity ───► Match testdata/panic_budget.txt
```

---

### 2. Panic Identity Controls (`panic_identity_test.go`)

Verifies that the fingerprinting algorithm correctly distinguishes semantic control path changes while remaining stable across benign formatting edits.

- **Sensitivity**: Inverts outer `if` conditions, swaps `then`/`else` branches, changes `switch` tags, or alters `select` arms. Asserts that fingerprints **diverge**.
- **Stability**: Inserts comments, rewords whitespace outside literals, or adds unrelated statements. Asserts that fingerprints **remain identical**.

---

### 3. Comment Budget (`comment_budget_test.go`)

Enforces that codebase comments are self-contained and explain the *why*, *invariants*, and *tradeoffs* in plain language without relying on cryptic references to external or private documents.

#### Scopes
- **Production Scope (`testdata/comment_budget.txt`)**: All non-test `.go` files. Frozen at 0.
- **Tests Scope (`testdata/comment_budget_tests.txt`)**: All `_test.go` files. Frozen at 0.

#### 14 Narrow Detectors
Flags references such as design record numbers, section anchors, review round tokens, reviewer names as authority, ticket/issue numbers, matrix coordinates, and knowledge base document citations.

```
  Comment Line ──► [14 Narrow Regex Detectors]
                          │
            ┌─────────────┴─────────────┐
            ▼                           ▼
      No Match Found               Match Found
      (Self-contained)                  │
                                        ▼
                            Increment File Violation Count
                                        │
                                        ▼
                           Compare with Exact Budget
                           ├── Count > Budget: REGRESSION (Fail)
                           ├── Count < Budget: RATCHET STALE (Fail)
                           └── Unlisted & Count > 0: FROZEN ZERO (Fail)
```

---

### 4. Comment Detectors Test (`comment_detectors_test.go`)

Test fixture suite verifying the sensitivity and specificity of each of the 14 pointer detectors:
- **Positive Fixtures**: Real violation strings that must trigger the specific detector.
- **Negative Fixtures**: Legitimate lookalikes (such as `// a buffer of 64 KB` or `// "§" as data rune`) that must trigger zero detectors.
- **Bidirectional Completeness**: Every declared detector must have fixtures, and every fixture must map to a declared detector.

---

### 5. Promotion Self-Call Guard (`promotion_test.go`)

Detects Go method receiver shadowing on embedded base types. Because Go uses static method dispatch:
```
OuterStruct (Embeds Base)                BaseStruct
+-----------------------+              +-----------------------+
| - Overrides Callee()  |              | - Caller()            |
| - Inherits Caller()   |              | - Callee()            |
+-----------------------+              +-----------------------+
            │                                      │
            │ outer.Caller()                       │
            └─────────────────────────────────────►│ Runs Base.Caller()
                                                   │ Calls b.Callee() on Base receiver!
                                                   │           │
                                                   │           ▼
                                                   │  [Base's Callee()] <─── DEFECT!
                                                   │  (Outer's override ignored!)
```

#### Defect Classification
- **LIVE**: An outer type embeds the base, overrides the callee, but inherits the caller. Shipped code runs the wrong method today. **Strictly 0; never allowlisted**.
- **LATENT**: A base method calls a sibling on its own receiver, but current embedders either override both or inherit both. Tracked in `testdata/promotion_selfcalls.txt`. The allowlist may only shrink.

---

### 6. Routing Documentation Guard (`routing_docs_test.go`)

Guards against documentation drift in event routing architecture. Mechanically asserts that `tui/doc.go`, `tui/README.md`, `tui/widget/doc.go`, `tui/widget/README.md`, and `tui/tutorial/04-events-focus-keys.md` contain required routing vocabulary and omit the specific obsolete sentence claiming raw handlers run before resolution.

The runtime event routing sequence enforced across documentation is the **bounded per-node walk**:
1. **Target Selection**: Key/UserEvent targets the confined focused node (within active trapping scope ceiling); pointer input targets the hit-test node; `CaptureRaw` targets the active capture node (direct dispatch without bubbling); `CaptureGesture` routes directly to the active gesture recognizer.
2. **Per-Node Sequence (`routeToNode` on `n`)**:
   - **Pointer Policy Gate**: If pointer-derived and effective pointer policy is `PointerDisabled`, skips `n` entirely and continues to parent.
   - **Action Resolution**: First match over consumer resolvers, then defaults.
   - **Semantic Action Dispatch**: Delivers to `ActionHandler.HandleAction`. If unhandled and action is concrete `ActivateAction` (or `*ActivateAction`), falls back to `Activatable.Activate` and publishes `ControlActivatedEvent`.
   - **Raw Delivery**: If no action was produced or the action went unhandled, delivers raw `HandleEvent` to the same node `n` (semantic delivery comes first).
3. **Walk Continuation**: If unconsumed, bubbles up `n.parent` up to the active trapping scope ceiling (`confinement()`).
4. **Post-Walk Fallbacks**: Global key bindings for unconsumed key events; eligible unconsumed primary press falls through to the Gesture Recognizer (only if target is `Activatable`, availability is active, and pointer policy is enabled).

---

### 7. Comments-Only Change Check (`commentsonly_test.go`)

A specialized validation tool for mass comment rewrites.
- **Invocation**: `COMMENTS_ONLY_BASE=origin/main go test ./internal/audit/ -run TestCommentsOnlyChange -v`
- **Mechanism**: Parses AST for both base commit and working tree, strips all comments (`f.Comments = nil`), formats code via `printer.Fprint`, and asserts byte-for-byte equivalence.
- **Defects Prevented**: Accidental line deletion, punctuation stripping, or regex replacement of code containing comment-like substrings.

---

## Developer Runbook & Failure Remediation

```sh
# Run all audit guards
go test ./internal/audit/

# Run with verbose output (shows census and budget totals)
go test -v ./internal/audit/

# Run a specific guard
go test ./internal/audit/ -run TestPanicBudget
go test ./internal/audit/ -run TestCommentBudget
go test ./internal/audit/ -run TestPromotionSelfCalls
```

### Remediation Matrix

| Failure Message | Root Cause | Required Action |
| --- | --- | --- |
| `N panic site(s) missing from testdata/panic_budget.txt` | You added a new panic or reworded an existing panic expression | Classify the site (`construction`, `invariant`, etc.). Run `GOLIB_PANIC_BUDGET_UPDATE=1 go test ./internal/audit/`, inspect `git diff`, and provide justification. |
| `N inventory row(s) point at sites that no longer exist` | You removed, hoisted, or refactored a panic site | Run `GOLIB_PANIC_BUDGET_UPDATE=1 go test ./internal/audit/` and verify the deletion in `git diff`. |
| `N file(s) are frozen at zero and are no longer clean` | A newly written or edited comment contains an external coordinate / pointer | Rewrite the comment in plain language to describe the requirement, invariant, or tradeoff directly. Do NOT add a budget line. |
| `N budget line(s) no longer match: ... lower it to X` | You cleaned up comments in a file with an existing budget | Lower the count in `testdata/comment_budget*.txt` to X (or delete the line if X == 0). |
| `N LIVE promotion defect(s) — an embedder is running the base's method today` | A base method reached for its own receiver, shadowing an embedder's override | Refactor the base method to accept the collaborator as an explicit parameter (e.g. pass the dialect interface). |
| `<doc> does not mention "<term>"` | Architecture or widget docs drifted from runtime reality | Update the documentation surface to accurately explain the routing/widget model. Do NOT remove the term requirement. |
| `N file(s) had their CODE changed by a comments-only change` | A comment migration tool accidentally edited executable Go syntax | Inspect `git diff` for deleted commas, altered parens, or truncated lines. Restore code to byte-identical state. |

---

## References & Deep Dives

For further historical background, detailed design narratives, and verification discipline:
- [The Guards in Detail (guards.md)](../../docs/audit/guards.md)
- [Verification Discipline & The Mutation Matrix (verification-discipline.md)](../../docs/audit/verification-discipline.md)
- [Documentation Overview (docs/audit/README.md)](../../docs/audit/README.md)
