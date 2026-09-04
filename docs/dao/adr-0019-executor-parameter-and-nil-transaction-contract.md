# ADR-0019 — `golib/dao`: the executor parameter — `On(nil)` as contract, and which guardrails stay downstream

- **Status:** **Proposed** — §2.1 and §2.2 are *implemented on this branch* and
  record a ruling already made downstream (Johno, 2026-09-01, in the
  `golib-dao` KB convention's "executor parameter" section, upstream ask 4);
  §2.3, §2.4 and §2.5 are the decisions this ADR asks to ratify. Authored by
  kimmy-vision from the autodb `golib/dao` upstream check-back task
  (`2026-08-31-golibdao-analyze-whether-the-keyset-sweep-pager-should-move-upstream-check-back-task`).
- **Date:** 2026-09-05
- **Module:** `github.com/yongjohnlee80/golib`
- **Supersedes:** nothing. **Documents** an existing, load-bearing behaviour of
  `Schema.On` from ADR-0005 §4 as refined by ADR-0015; adds no API and changes
  no code path.
- **Related:** ADR-0005 (transactions), ADR-0015 (transaction/connection
  ownership — "you do not pass connections"), ADR-0009 (`WithQueryContext`
  precedence), ADR-0018 (pinned connections). KB: the `golib-dao` convention's
  "The executor parameter — one helper shape for pinned and pool" section and
  code-review §13 (Pattern 4), which this ADR's §2.5 answers. KB ADR-0084 /
  ADR-0085 (autokb, proposed 2026-09-05) for the §2.3 second-consumer check.

## 1. Context

Downstream (autodb) landed two guardrail files deliberately written to be
transferable upstream — a keyset sweep pager and a join-or-begin transaction
helper — and a ruling that the executor is always passed explicitly:

```go
func (s *Service) helper(tx *dao.Transaction, …) error {
    return s.store.X.On(tx).…   // the caller's transaction — or the pool
}
```

That shape has exactly one prerequisite: **`On(nil)` must mean "no transaction
is held — use the pool"**. It does, and has since at least v0.2.2
(`dao/query_dao.go` `handle()` returns `d.conn` when `d.tx == nil`), but
`Schema.On`'s doc comment says only "bound to a transaction". The contract is
real and undocumented, which is the worst of both: consumers depend on it while
a maintainer reading only the doc comment would be entitled to "fix" it into a
panic.

The check-back task asked four questions. The measurements are in §3; the
answers are §2.

## 2. Decision

### 2.1 A nil `*Transaction` passed to `Schema.On` is CONTRACT, and is documented as such

Normative:

- `On(nil)` returns a DAO **equivalent to `Schema.DAO()`**: every statement, and
  the writer returned by `DAO.Batch()`, executes on the connection pool
  (autocommit).
- `On` **never panics** on a nil transaction and **never begins one of its own**.
- The equivalence is total, not partial: same schema, same connection, same
  effective hook set, same `WithQueryContext` handling. The only branch `On`
  takes on a non-nil transaction — inheriting `tx.ctx` when no explicit query
  context was given (ADR-0009 §2.3) — has nothing to inherit when there is no
  transaction.
- There are **two** fallthrough sites, and both are the contract:
  `queryDAO.handle()` for statements and `queryDAO.Batch()` for the batch
  writer, which resolves its executor separately.

This is the guarantee that makes an executor **parameter** work: one signature
serves a caller inside `RunTx` (pass the transaction; every statement is pinned
to it) and a caller outside one (pass `nil`; it runs on the pool), with no
conditional and no `X`/`XTx` dual variants. It is also why the contract must be
stated rather than merely observed — it is the load-bearing assumption of every
such helper in every consumer.

Documented at `Schema.On` (with the helper shape as a runnable doc example),
cross-referenced from `Schema.DAO`, marked at both internal fallthrough sites,
and **locked by tests** (§4). A doc comment is not a lock; a test is.

### 2.2 Panic-on-nil is REJECTED

An earlier suggestion was for `On(nil)` to panic as API misuse. Rejected, for
four reasons, recorded here so it is not re-proposed:

1. **It would destroy the one-signature pattern.** Every executor-parameter
   helper would need `if tx != nil` around two calls that are already the same
   call — the boilerplate this contract exists to delete, reintroduced at every
   call site (§3 measures 14 hand-written instances of exactly that).
2. **It contradicts golib's error philosophy** (`golib` convention, Errors):
   "No `panic` on API misuse reachable at runtime with valid types; construction
   validates and may panic at `New` (fail early), queries/operations return
   errors." A nil `*Transaction` is a valid typed value reaching a query
   operation.
3. **It would be a breaking change** to a behaviour consumers already depend on
   — pinned as far back as v0.2.2 — with no deprecation path, in the exact
   shape the never-break-consumers policy exists to prevent.
4. **`OnCtx` already sets the precedent** and documents it: "or an unbound pool
   DAO when ctx carries none". `On` behaves the same way and merely failed to
   say so.

### 2.3 The keyset sweep pager stays DOWNSTREAM for now

`meta.Sweep` (autodb `core/meta/pager.go`) is a good API and transfers cleanly —
it imports only `context`, `errors`, `fmt` and `dao`. It is nonetheless **not
upstreamed in this ADR**, on the project's own rules:

- **The second-consumer test fails.** No other `golib/dao` consumer hand-rolls a
  sweep (§3). The `Limit`/`Offset` pagination in ddex-server and ddex-validator
  is a caller-bounded HTTP list endpoint — a page requested by a client, not an
  unbounded drain — and the downstream convention already excludes that class
  from the pager by name.
- **`golib` forbids speculative surface**: "Don't declare sentinels or exported
  API that nothing uses — dead surface is debt with a doc comment." Upstreaming
  today would add an exported generic function with **one** call site in **one**
  consumer, which would keep working unchanged if we do nothing.
- A native dao version would also be a **different** API, not a move: the schema
  already knows its id column, so `Key`/`ByKey`/`KeyOf` could collapse into it.
  Designing that against one call site risks freezing the wrong shape.

**On `autokb`, the named upcoming consumer.** The assignment behind this task
asked for the upstream question to be settled "before the autokb
implementation", so it was checked rather than assumed. As of 2026-09-05 autokb
is **proposed, not built** (KB ADR-0084 / ADR-0085, authored today), it does
build on `golib/dao`, and its stated golib prerequisite is **`golib/fs`
(Local/GCS/SFTP) — not a dao change**. Its storage design (§4.1: SQLite +
FTS5 + binary-quantized vectors) specifies **no keyset sweep**; the closest
candidates, `kb.reindex` and the Hamming-distance similarity scan, are a VFS
walk and a vector scan respectively, neither of which is the
position+LIMIT+predicate shape over an id-bearing table that `Sweep` exists for.
So autokb does not yet supply the second consumer, and upstreaming *in
anticipation* of it would be designing against a document rather than a call
site.

**Re-open trigger (explicit, so this is a decision and not a shrug), any one of:**

1. An autokb (or other consumer) design or implementation that specifies a
   bounded scan over a table with a unique monotonic id — a `kb.reindex`
   incremental pass or a vector re-quantization sweep are the plausible ones.
2. A second sweep in autodb that `meta.Sweep`'s current API does not fit.
3. A third consumer hand-rolling position+LIMIT+predicate at all.

At that point the native-`Schema` variant of the third bullet above is the design
to evaluate, not a verbatim lift. Until then autodb's copy is the reference
implementation and stays where its tests are. Whoever implements autokb's
storage layer should read this section before hand-rolling a sweep — that is the
moment this decision is meant to be revisited, and the pager already exists to
copy from.

### 2.4 Join-or-begin (`MustTx`) stays DOWNSTREAM, and needs a name before it moves

`meta.MustTx` — join the caller's transaction when non-nil, begin and own one
via `RunTx` when nil — is the natural companion to `RunTx` and would sit well in
`dao`. Two blockers, both fixable, neither resolved today:

- **Nothing calls it.** Zero production call sites in autodb (§3), and its
  siblings in the other consumers are the `On(nil)` conditionals of §3, which
  §2.1 deletes without any new function. Same dead-surface rule as §2.3.
- **`Must` is the wrong prefix for a function that returns an error.** golib
  philosophy #8 is "ecosystem-normal shapes"; in the stdlib `Must*`
  (`template.Must`, `regexp.MustCompile`) means *panics instead of returning an
  error*. The downstream name is inherited from LabelManager's
  `pkg/transaction.MustTx` and is fine in place, but minting it as public golib
  API would ship a stdlib-contradicting name permanently. `dao.JoinTx` or
  `dao.RunTxJoining` are candidates; the choice belongs with whoever brings the
  first real caller.

### 2.5 dao does NOT gain a runtime Pattern-4 re-entrance guard

The companion question was whether dao should gain a dev-build guard that fires
when code holding a pinned resource re-enters the pool (code-review §13's
Pattern 4 — the shape behind autodb's migration-runner deadlock at
`pool_max_conns=1` and its split-connection `BEGIN`). Under the 2026-09-01
ruling the target narrowed to `OnCtx`-under-held-transaction.

**dao cannot observe that condition**, and the reason is structural rather than
a matter of effort:

- `RunTx` hands `fn` a `*Transaction`, not a context carrying one
  (`dao/transaction.go`); `WithTx` is opt-in. The defect is precisely a helper
  that receives a **bare** context, so by construction the context dao would
  inspect holds nothing to find. Where the transaction *is* in the context,
  `OnCtx` already binds to it and there is no defect to catch.
- The only other available signal — "a transaction is live on this `DataConn`" —
  does not mean the caller holds it. dao's own contract says a `Transaction` is
  single-goroutine and that **background work uses an unbound `schema.DAO()`**,
  so concurrent pool use while a transaction is open elsewhere is correct,
  endorsed, and indistinguishable at the seam from the defect. A guard keyed on
  it would fire on correct code — the worst kind of instrument, since a
  false-positive guard gets disabled and then observes nothing at all.
- Distinguishing them needs goroutine identity, which Go does not offer and
  which a library has no business synthesizing from stack scraping.

Enforcement therefore stays where it can see the caller: the executor-parameter
shape of §2.1 (which makes the defect inexpressible — a helper that takes `tx`
cannot silently draw from the pool) plus code-review §13 at review time. A
static analyser over consumer code is a legitimate future option; a dao runtime
guard is not, and this ADR closes the question rather than leaving it open.

## 3. Evidence

Measured 2026-09-05 at `golib` `origin/main` `8dfb0ab` (v0.5.7). Consumers of
`github.com/yongjohnlee80/golib/dao`, excluding golib's own tree: **autodb**
(v0.5.7), **ddex-server** (v0.2.2), **ddex-validator** (v0.2.1). LabelManager is
*not* a consumer — it uses `monstercat/golib` with a different DAL.

| # | Question | Measurement | Verdict |
|---|---|---|---|
| 1 | Does a second consumer hand-roll a sweep? | ddex-server `control/control.go:135,148,172,197,221,226,239,308` and ddex-validator `control/control.go:125–198` use `Limit(l).Offset(o)` where `l, o` come from `page(r)` — HTTP `?limit`/`?offset`, capped by `maxLimit`. No unbounded drain loop in either. golib's own tree has no `dao` consumer outside `dao/`. | **No second consumer** → §2.3 |
| 2 | Does the pager transfer verbatim? | Yes — `core/meta/pager.go` imports only `context`, `errors`, `fmt`, `dao`. No drift found. But a native version would differ (the schema knows its id column), so the transfer being possible is not an argument that this shape is the right upstream one. | Transfers; shape unsettled → §2.3 |
| 3 | Do consumers hand-roll the `On(nil)` conditional? | **14 instances across the two other consumers**, all semantically identical to `On(tx)`: five per-schema selectors each in ddex-server `store/entities.go:228,234,240,246,252` and ddex-validator `store/entities.go:163,169,175,181,187`, plus an inline closure each at `store/resolve.go:37` and `:94` in both. ddex-server's `store/resolve.go:34` even documents the intent — "tx may be nil to run on the pool" — while implementing it by hand. The nil path is live, not defensive: `ingest_test.go:469`, `resolve_test.go:127,157` and `choreography_test.go:268` pass a literal `nil`. | **Cost is real and measured** → §2.1 |
| 4 | Was the contract available when they were written? | Yes. At `v0.2.2` — the version ddex-server pins — `dao/query_dao.go:58–63` `handle()` already returned `d.conn` for a nil transaction, and `On`'s doc comment already said only "bound to a transaction". Every one of the 14 was unnecessary the day it was written. | The silence, not the behaviour, is the defect → §2.1 |
| 5 | How much does the sweep get used? | `meta.Sweep`: **1** production call site (autodb `core/exec/txretention.go:54`) plus its own suite. `meta.MustTx`: **0** production call sites anywhere; definition and ownership-matrix tests only. | Both fail the dead-surface bar → §2.3, §2.4 |
| 6 | Never-break-consumers audit | §2.1/§2.2 add no exported symbol, change no signature, and alter no code path — doc comments and tests only, so there is nothing for an implementor to re-implement. Enumerated at this checkout HEAD including the nested `dao/bigquery` module (invisible from the module cache): no interface in this ADR's scope changes. | **Additive-only** |

## 4. Implementation notes

Shipped on `dao-on-nil-contract`:

- `dao/schema.go` — `Schema.On` documents the nil contract, the never-panics /
  never-begins guarantee, and the helper shape as a doc example; `Schema.DAO`
  cross-references it.
- `dao/query_dao.go` — `handle()` gains a doc comment naming the fallthrough as
  contract and pointing at the locking tests; the `Batch()` executor line is
  marked as the same contract.
- `dao/on_nil_test.go` — five tests (six with subtests): the pool routing and
  begin-count reading, a non-nil control, `On(nil)`/`DAO()` equivalence on both a
  bare and a hooked+debug schema, the batch writer's separate executor path, and
  `WithQueryContext` survival. White-box, stdlib `testing`, no server.

**Mutation matrix**, run per-mutation with the unmutated suite proven green
first, each anchor asserted to match exactly once, and each test run in its own
binary so a panicking mutant cannot mask its siblings (code-review §15):

| Mutation | Caught by | Other cells |
|---|---|---|
| M1 `handle()`: nil tx → error | `RunsOnThePoolAndBeginsNothing` | all pass |
| M2 `handle()`: nil tx → panic | `RunsOnThePoolAndBeginsNothing` | all pass |
| M3 `Batch()`: nil tx → `initErr` | `BatchFlushesToThePool` | all pass |
| M4 `On()`: ignore the tx argument | `NonNilStillBindsToTheTransaction` | all pass |
| M5 `On()`: nil tx drops the explicit ctx | `HonorsWithQueryContext` | all pass |
| M6 `On()`: nil tx drops the hooks | `IsEquivalentToDAO` | all pass |

6/6 caught, one cell each, no cross-firing — the matrix is diagonal, so each
test observes its own claim rather than the package's general health. M1 and M3
initially failed to **compile** (a missing import); a non-compiling mutant proves
nothing, so they were fixed and re-run rather than scored or dropped.

`go build ./...` and `go test ./...` are clean at the branch head
(0 failures, whole module).

## 5. Acceptance

1. `Schema.On`'s doc comment states the nil-transaction contract, the
   never-panics/never-begins guarantee, and the executor-parameter shape. ✅
2. Both fallthrough sites (`handle()`, `Batch()`) are marked as contract in
   code, so the guarantee is visible where it would be edited. ✅
3. Tests fail if the fallthrough becomes a panic, an error, or a silent
   `BEGIN`, and fail if `On(nil)` stops matching `DAO()` — each proven by a
   mutant, against a green baseline. ✅
4. No exported API added, no signature changed, no behaviour changed. ✅
5. §2.2's rejection is recorded with its reasons. ✅
6. §2.3–§2.5 record explicit decisions with re-open triggers, not deferrals. ✅
7. Ratification by Johno for §2.3, §2.4, §2.5. **Pending.**

## 6. Consequences

- The executor-parameter shape becomes a *supported* pattern rather than a
  downstream convention resting on an undocumented behaviour. Consumers can
  delete their `if tx != nil` selectors: 14 in the two ddex repos, whose next
  golib bump could drop ~90 lines with no behaviour change (46 per repo: a
  32-line selector block in `store/entities.go` and two 7-line closures in
  `store/resolve.go`). That cleanup is
  those repos' own work, not this ADR's.
- A future maintainer who wants `On(nil)` to fail now has to argue with an ADR
  and six red tests instead of an ambiguous comment. That is the entire point.
- autodb keeps `meta.Sweep` and `meta.MustTx` as the reference implementations,
  with the §2.3 re-open trigger recorded. Neither the pager question nor the
  naming question is lost — they are answered "not yet, and here is what would
  change the answer."
- Pattern 4's enforcement is settled at the review + API-shape layer, so no one
  re-proposes a runtime guard that cannot distinguish the defect from correct
  concurrent use.

## 7. Follow-up, out of scope here

A **per-connect hook** — pgxpool's `AfterConnect`, and a connector seam for
go-sql-driver — is a separate, real upstream candidate, noted downstream at
autodb `core/exec/dsn.go:31`: it would let a consumer verify or set session
grammar once per physical session instead of per execution. It is a new seam on
the driver surface (golib philosophy #2), not a doc clarification, so it needs
its own ADR and its own second-consumer argument. Filed as a follow-up task, not
folded in here.
