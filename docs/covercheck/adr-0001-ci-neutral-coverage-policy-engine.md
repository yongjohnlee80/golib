---
type: adr
created: 2026-09-12
updated: 2026-09-12
revision: 0
status: proposed
project: golib
author: lector
requested-by: johno
sources:
  - "Johno 2026-09-12: create a reusable golib package for GitHub and other CI/CD coverage checks, compare coverage changes for files in a PR, and enforce configurable Go coverage thresholds [DECISION — this ADR]"
  - "Go coverage profile format emitted by go test -coverprofile and consumed by go tool cover"
tags: [golib, covercheck, ci, test-coverage, pull-request, adr, status:proposed]
---

# ADR 0001 — CI-neutral Go coverage policy engine

**Tags:** `type:adr` `status:proposed` `repo:golib` `area:ci` `area:test-coverage` `feature:covercheck`

**Abstract:** Add a standard-library-only `covercheck` package and companion CLI that compare Go coverage profiles across a pull request, measure changed-code coverage, enforce explicit policy, and run unchanged across GitHub and other CI systems.

- **Date:** 2026-09-12
- **References:** Go `-coverprofile`; pull-request base/head Git revisions
- **Scope:** `github.com/yongjohnlee80/golib/covercheck`, `cmd/covercheck`, and a proving integration in golib CI

## Context

Go repositories can emit statement coverage through `go test -coverprofile`, but the standard tooling reports one revision at a time. A pull-request gate needs to answer several different questions without conflating them:

1. Did total, package, or a changed file's coverage increase or decrease relative to the PR base?
2. Are the executable blocks touched by the patch sufficiently covered at the PR head?
3. Does the PR satisfy an absolute minimum and a regression budget?

`git diff` alone answers where source changed; it cannot establish a coverage regression. A regression requires a base profile and a head profile produced with equivalent package selection and test settings. Conversely, two profiles alone cannot identify patch coverage; that requires changed line ranges from the diff.

The package is intended for reuse by other Go repositories. It therefore cannot assume GitHub Actions, call a forge API, require a token, own branch protection, or mutate a caller's checkout. Golib's conventions additionally require a standard-library-only core, small adaptable seams, typed failures, package documentation, and explicit behavior rather than hidden global state.

## Decisions

1. **Ship a reusable `covercheck` library and a thin `cmd/covercheck` executable.**

   **Why:** Go callers can import analysis and policy primitives, while repositories wanting only a CI gate can install a versioned command without adding the library to their application's dependency graph.

   **Cost / tradeoff:** The public library API and CLI contract both require compatibility care.

2. **The core analyzes explicit inputs; it does not execute tests, check out revisions, fetch Git refs, or call CI APIs.** Inputs are the base coverage profile, head coverage profile, and changed-file ranges. The CLI may read files and invoke `git diff` when the caller requests Git discovery, but test orchestration remains in the repository workflow.

   **Why:** Running arbitrary test commands and mutating Git state from a library would be surprising, security-sensitive, and difficult to make portable. Explicit artifacts also make the analyzer deterministic and directly testable.

   **Cost / tradeoff:** A consumer workflow needs a few orchestration steps for the two revisions.

3. **Use Go's textual coverage profile as the v1 interchange format.** Parse `set`, `count`, and `atomic` profiles with the standard library; introduce no new module dependency.

   **Why:** `go test -coverprofile` emits this format, and integration coverage can be converted to it with `go tool covdata textfmt`. A small parser is sufficient and preserves golib's zero-dependency core.

4. **Normalize profile names to repository-relative paths before comparison.** The analyzer receives an explicit module path/root mapping; the CLI may derive the root module path from `go.mod`. Unknown or ambiguous mappings fail loudly.

   **Why:** Go profiles normally contain import-path-prefixed filenames while Git diffs contain repository-relative filenames. Silent suffix matching can associate the wrong file in a repository containing repeated directory names.

5. **Report statement-weighted statistics at total, package, and file scope.** Each result carries covered statements, total statements, percentage, and—where both revisions contain the entity—the percentage-point delta.

   **Why:** Percentages alone hide denominator changes when a PR adds or removes code. Counts make the result auditable.

6. **Define changed-code coverage as coverage of head-profile blocks whose source range intersects an added or modified head-side diff line.** A block is counted once even when several changed lines intersect it. The block's complete `numStmt` weight is used.

   **Why:** The coverage profile locates blocks and statement counts but does not locate every statement within a multi-line block. Block intersection is deterministic and conservative without pretending to possess line-level statement data the profile does not contain.

   **Cost / tradeoff:** A one-line edit inside a multi-statement block attributes the whole block to changed code. The report must name this metric “changed blocks” or document the approximation; a future AST-backed mode may refine it without changing full-file regression semantics.

7. **Make policy independent of measurement.** Functional options configure minimum total, package, file, and changed-code coverage plus maximum total and per-file regression. Unset policies are disabled. Matching overrides and exclusions are explicit data.

   **Why:** Callers can render or store measurements without enforcing them, and policy evolution does not contaminate parsing.

8. **Use explicit edge semantics.** New files have no base delta and are evaluated by head/changed thresholds. Deleted files do not fail head thresholds. Renames use the Git rename mapping. Files with no executable coverage blocks report `N/A`, not zero. Missing changed production files are reported rather than silently omitted. Because absence from a profile alone cannot distinguish an uninstrumented executable file from a declaration-only file, the CLI classifies head source with the standard-library Go parser and passes that evidence into analysis; library callers without source access may leave executability unknown for conservative handling.

9. **Keep the executable CI-neutral.** It writes a text table by default and supports Markdown and JSON. Exit `0` means policy passed, `1` means a valid report failed policy, and `2` means inputs/configuration/operation were invalid. GitHub annotations or PR comments are outside the core and outside the initial release.

10. **Prove the package first in golib's own pull-request workflow.** Base and head profiles use the same Go version, package expression, cover mode, and test flags. The workflow selects explicit base/head SHAs instead of relying on a CI provider's default checkout semantics.

## Acceptance requirements

- **A1 — Package surfaces.** `covercheck` has `doc.go`, README, exported-symbol documentation, and a stable library example; `cmd/covercheck` is installable with `go install github.com/yongjohnlee80/golib/cmd/covercheck@<version>`.
- **A2 — Profile parsing.** Valid `set`, `count`, and `atomic` profiles parse; malformed mode/header, coordinate, statement-count, and execution-count inputs return typed, line-addressable errors.
- **A3 — Duplicate blocks.** Profiles produced across package runs are combined deterministically; a block is covered when any occurrence has a positive count and is never double-weighted.
- **A4 — Path identity.** Import-path filenames normalize to exact repository-relative paths. Unknown, escaping, or ambiguous paths fail rather than suffix-match.
- **A5 — Statistics.** File, package, and total statistics are statement-weighted and expose covered/total counts plus percentage. Zero-statement entities render `N/A`.
- **A6 — Revision comparison.** Entities present in both profiles report signed percentage-point delta and count changes; new and deleted entities are classified explicitly.
- **A7 — Diff parsing.** Added and modified head-side ranges, deletions, and Git-detected renames are represented. Multiple hunks and quoted paths are covered by tests.
- **A8 — Changed blocks.** Head profile blocks intersecting changed head lines are counted once with their full statement weight; unchanged blocks and deleted-only hunks are excluded. Tests pin boundary and multi-line-block behavior.
- **A9 — Missing coverage evidence.** A changed non-test `.go` file absent from the head profile is visible in the report and can fail policy; it never disappears as if no change occurred.
- **A10 — Policy.** Minimum total/package/file/changed coverage and maximum total/per-file regression are independently optional, support exclusions/overrides, and produce structured violations naming actual and required values.
- **A11 — Determinism.** Results and violations have stable ordering independent of map iteration, profile entry order, or diff hunk order.
- **A12 — Output and exit contract.** Text, Markdown, and JSON represent the same result. CLI exits distinguish pass (`0`), policy failure (`1`), and operational/configuration failure (`2`).
- **A13 — Platform neutrality.** Analysis performs no network calls and imports no GitHub/GitLab SDK. The core has no third-party dependencies and does not invoke Git or Go subprocesses.
- **A14 — Safety.** The CLI never changes Git state. Git discovery is read-only and every invoked revision is explicit in diagnostic output.
- **A15 — Golib proving integration.** Golib CI generates equivalent base/head profiles from explicit pull-request revisions, runs the check with read-only repository permissions, and exposes one branch-protectable job result.
- **A16 — Reuse proof.** Documentation contains provider-neutral shell usage and a GitHub Actions example; all provider-specific behavior is orchestration around the same executable.
- **A17 — Verification.** Focused tests cover every parser/policy edge above, `go vet ./...` passes, and the standardized golib harness produces a clean commit-stamped ledger before review.

## Files touched / created

| File | Action | Purpose |
|---|---|---|
| `covercheck/*.go` | create | Coverage model, profile and diff parsing, comparison, and policy evaluation |
| `covercheck/*_test.go` | create | Unit, edge, and negative-control coverage |
| `covercheck/doc.go` | create | Package contract |
| `covercheck/README.md` | create | Installation and library usage |
| `cmd/covercheck/main.go` | create | Portable CI executable |
| `docs/covercheck/adr-0001-ci-neutral-coverage-policy-engine.md` | create | Canonical repository design record |
| `.github/workflows/go.yml` | update | Proving PR integration after the analyzer is complete |

## Alternatives considered

1. **Configure an existing hosted coverage service.** Fast to adopt, but it introduces an external account/data path and does not produce the reusable golib API requested.
2. **Adopt an existing standalone Go coverage gate.** This can satisfy common thresholds quickly, but delegates the exact path identity, changed-block semantics, output contract, and compatibility policy to another project. It remains a valid fallback if maintaining this package stops being worthwhile.
3. **Implement only a GitHub Action.** Rejected because the desired package must also work in other CI providers and as an imported Go library.
4. **Store a rolling baseline artifact instead of running the base revision.** Cheaper per PR, but stale or missing artifacts can compare against the wrong base. Revisit after direct base execution is measured in real CI.
5. **Infer regression from the head profile and Git diff alone.** Impossible: neither artifact contains base execution data.
6. **Use YAML configuration in v1.** Rejected because YAML requires a new dependency or a substantial hand-written parser. CLI flags and optional JSON retain a standard-library-only module.

## Open flags for future

- Revisit cached baseline artifacts if running base tests materially slows pull requests.
- Add multi-module/workspace discovery only after a real consumer requires it; v1 targets one Go module per invocation.
- Add native `covdata` directory input when integration-test consumers appear; conversion to text is adequate initially.
- Consider an AST-backed exact changed-statement mode if block-intersection approximation produces actionable false positives.
- Add provider adapters for annotations or PR comments as separate packages only when plain output and job summaries prove insufficient.
- Acceptance remains Johno's decision; changing an accepted decision requires a superseding ADR.
