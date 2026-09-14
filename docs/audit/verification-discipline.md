# Verification discipline

The guards in [`internal/audit`](../../internal/audit/README.md) check the source tree. This document
covers the practice they are one instrument of: **how a change in this
repository is proven, rather than merely observed to be green.**

**The one-sentence version:** a passing test is evidence only once you have seen
it fail for the right reason.

Everything here is a rule that exists because its absence shipped a defect. The
examples are real and are named, because a rule with no incident behind it tends
to encode its author's taste.

---

## Part 1 — The mutation matrix

### What it is

A **mutation matrix** is a table of deliberate defects, each paired with the
test that must catch it. You break the code on purpose, one break at a time, and
require the suite to notice.

A green suite tells you the code passes the tests. A mutation matrix tells you
the **tests observe the code** — which is the claim you actually want to make
when you hand work to a reviewer.

### A cell

One row of the matrix. It carries four things, and all four are required:

| Field | Meaning |
| --- | --- |
| **Name** | The defect in plain language: *"restored scrim added ON TOP of the survivor"*. Not "M29" — the number is a label, not a description |
| **Anchor** | The exact source text to replace, which must occur **exactly once** in the file |
| **Mutation** | What to replace it with — the smallest edit that introduces that defect |
| **Test** | The single test expected to fail |

### The protocol

1. **Assert a green baseline first.** Run the named test unmutated and require
   it to pass. A cell whose test always fails "catches" every mutant, and a
   harness that skips this step will report a perfect matrix while proving
   nothing.
2. Apply exactly **one** mutation.
3. Run **only** the named test. Not the suite — a suite run hides which test did
   the catching, and a cell whose defect is caught by some unrelated test is not
   the cell you wrote.
4. Record the outcome.
5. **Restore the file from the in-memory original**, not with `git checkout --`.
   A checkout between mutations silently reverts the fix you just validated
   alongside the mutation.
6. Verify the tree is clean at the end (`git status --porcelain`) before
   reporting anything.

### The outcome vocabulary

Four outcomes, and conflating any two of them corrupts the result:

| Outcome | Meaning | What to do |
| --- | --- | --- |
| **KILLED** | The named test failed | The cell is proven. Move on |
| **SURVIVED** | The named test passed with the defect in place | A finding **about the test**, not the code |
| **BUILD-FAIL** | The mutation did not compile | **Not a kill.** Rework the mutation |
| **NO-ANCHOR** | The anchor text did not occur exactly once | The cell never ran at all |

`BUILD-FAIL` and `NO-ANCHOR` exist as distinct outcomes because both have been
misreported as kills. A classifier that decided by exit code alone counted a
compile error as a catch; one that grepped stderr for `undefined` missed a
`cannot use` error and did the same. Detect them explicitly and by their own
signature, never by "the test did not pass".

### Reading a survivor

**A survivor is a finding about the test, not the code.** The code has the
defect you injected and the suite did not care, so the assertion is not watching
what you thought it was.

Four things a survivor legitimately means, in the order they are usually true:

1. **The test asserts a proxy.** It watches something that co-varies with the
   claim rather than the claim itself.
2. **The assertion is vacuous.** It reads state at a moment when the value is
   the same either way.
3. **The test is missing.** No cell covers that behaviour and one has to be
   written.
4. **The behaviour is unobservable.** The property is guaranteed by something
   other than the line you mutated, so *no* mutation of that line can fail the
   test.

Only the fourth is a reason to remove a cell — and then you record **why** it is
unobservable rather than quietly dropping the row. A worked example from the
modal work:

> `Modal.Dismiss` runs the caller's callback before publishing
> `OverlayDismissedEvent`, and the test asserted that order. The cell survived.
> The order is guaranteed by `Bus.Publish` being **enqueue-only** — a subscriber
> always runs in a later drain than the `Dismiss` that published — rather than
> by the order of those two statements. No mutation of that order can fail the
> assertion. The test was kept with the real reason recorded beside it, and the
> cell was removed.

Contrast a cell that was kept and fixed:

> The wide-cluster clipping guard in the modal card title has **no observable
> effect**, because `Surface` already clips. Measured, not assumed. The guard
> stays — it states the intent at the site that owns it — and its cell was
> removed rather than left passing on nothing.

### Rules

- **Name the reverted line and the red message** when you report a kill. "36/36
  killed" with no evidence is a number; "reverting `h.Stack.Remove(top)` made
  `TestClosingAStackedDialog…` fail on the grid comparison" is a claim someone
  can check.
- **One defect per cell.** A mutation that changes two things kills for a reason
  you cannot attribute.
- **Mutate every call site you add a guard to.** A guard proven to *fire* is not
  a guard proven to be *consulted* — a sweep guard was once shown to work and
  then shown never to be reached.
- **A cell must observe the defect.** Ask what *else* would pass this assertion.
  If the answer is "quite a lot", the cell is measuring the wrong thing.
- **Boundary cells probe `LastValid + 1`.** See [Part 2](#1-probing-a-boundary-at-a-far-value).
- **Report the matrix with the change.** The reviewer gets the cell names, the
  outcomes, and the story of every survivor — including the ones you fixed.

### Mechanics

The matrix is typically driven by an ad-hoc runner script rather than by hand, so that the restore step cannot be forgotten and outcomes are classified uniformly. Below is a schematic pattern for such a temporary runner:

```python
CELLS = [
  ("M29 restored scrim added ON TOP of the survivor", f"{W}/overlay.go",
   "\t\t\th.Stack.Remove(top)\n\t\t\th.Stack.Add(h.scrim)\n\t\t\th.Stack.Add(top)\n",
   "\t\t\th.Stack.Add(h.scrim)\n",
   "TestClosingAStackedDialogRestoresTheBackdropBeneathTheOneBelow"),
  # …
]

for name, path, old, new, test in CELLS:
    src = open(path).read()
    if src.count(old) != 1:
        record(name, "NO-ANCHOR"); continue          # the cell never ran
    open(path, "w").write(src.replace(old, new, 1))
    status = run(test)                                # PASS / FAIL / BUILD-FAIL
    open(path, "w").write(src)                        # restore from memory
    record(name, "KILLED" if status == "FAIL" else status)
```

The `src.count(old) != 1` check is not defensive padding. An anchor that matches
zero times or twice means the cell silently did nothing, and without the check
it reports as a kill or a survivor at random.

### Worked example

The L4 modal work, second pass. Ten cells, all killed:

| Cell | Mutation | Killed by |
| --- | --- | --- |
| Stacked scrim not restored to the survivor | `if false && top.wantScrim` | whole-screen comparison |
| Restored scrim added on top of the survivor | drop the remove/re-add pair | whole-screen comparison |
| Dismissed modal left in the host's list | drop the slice splice | whole-screen comparison |
| Placement axes swapped for the right-hand corners | `x, y = 0, maxY` | corner subtest |
| Bottom placements not pushed to the bottom edge | `x, y = 0, 0` | corner subtest |
| `WithModalStyle` drops the association | `_ = s` | scrim cell attributes |
| `ModalStyle.WithTitle` mutates the receiver | assign and return `s` | immutability loop |
| `NewModalStyle` leaves the border underived | `border: style.Style{}` | derivation assertion |
| `PlacementSide.String` names no declared side | shift the first case | enum naming table |
| `PlacementAlign.String` collapses centre onto start | return `"start"` | enum naming table |

Three cells share one assertion, and deliberately so. The three ways the
backdrop restore fails are *separately invisible* — a backdrop never restored
just looks undimmed, one restored above the dialog just looks blank, and a stale
list entry only shows up on the next open. Pinning the entire rendered screen
catches all three; three narrower assertions would each have needed a different
proxy.

---

## Part 2 — The recurring failure modes

These are the mistakes that have actually recurred, with what each one looked
like and the fix that stuck.

### 1. Probing a boundary at a far value

**Recurred three times in one pull request** — pointer policy, gesture state,
modal placement.

```go
if PointerPolicy(200).Valid() { t.Error(...) }   // proves almost nothing
```

An off-by-one bound rejects `200` exactly as readily as a correct bound does, so
this cannot see the edge move. Probe the **first** invalid value, and assert the
last valid one is accepted:

```go
if !PlacementLeft.Valid() || (PlacementLeft + 1).Valid() {
    t.Error("PlacementSide.Valid does not bound the declared set at its edge")
}
```

### 2. A vacuous assertion — reading state at the wrong moment

An "armed while disabled" check that pressed *and released* the button, then
read `Armed()`. It is false either way after a release, so the assertion passed
for both the correct and the broken widget.

Read the state **while the condition holds** — with the button still down — or
drive the state directly with a setter.

### 3. Using a lane-B sync as a lane-A barrier

`h.sync()` proves the program lane drained. It cannot prove that an event
injected into the **input** lane was dispatched, so a test that injects and then
syncs is asserting against whatever happened to have arrived.

The fix is an ordered sentinel in the same lane: inject a second event whose
effect can only occur after the first was dispatched — for example activating a
sibling — and wait on that.

### 4. Reading loop-owned state from the test goroutine

Three tests raced under `-race` because they read widget fields directly. **The
tests were wrong, not the widget.** Component state is owned by the event loop,
and that ownership is exactly what lets widget authors write mutex-free Go.

Go through the loop:

```go
var armed bool
h.onLoop(func() { armed = b.Armed() })
```

### 5. Trusting an instrument you have not validated

- A repository sweep with **~90% false positives**, reported as findings.
- Its replacement, which introduced a **false negative** through a hand-written
  allowlist nobody checked.
- A documentation audit that was **case-sensitive** and failed a correct
  document.
- A mutation classifier that read a kill as a build failure by grepping for the
  wrong word.

An instrument blind to a class reports that class **passing**, never *unknown*.
Before trusting a reading, show the instrument producing the opposite reading on
a case you constructed — a positive control. Every guard in `internal/audit` has
one built in; see [the three properties](README.md#the-three-properties-every-guard-here-has).

### 6. Reporting a number from memory

A coverage delta quoted as `+0.26pp` when the measurement said `+0.29pp`. It
changed no decision, and it was still wrong, and there was no reason for it to
be: **re-run every gate fresh at the exact head being reported**, and quote what
that run printed.

### 7. A field that is written and never read

Two occurrences in one pull request, both found by the reviewer rather than by
me. A struct field assigned on every state change and consulted by nothing reads
as working machinery.

Grep every new field for a reader before pushing. A field with no reader is
either dead or a bug — usually the second.

### 8. A comment asserting a relation that nothing checks

The panic budget once carried a comment claiming a certain swap "would no longer
match". Nothing in the code observed that relation, and the swap matched fine.
Indicative prose — *X is Y minus Z*, *this cannot drift* — is a **claim**. Either
something checks it or it is decoration that will outlive its truth.

---

## Part 3 — The gates a change passes

Every gate below runs at the **exact head being submitted**, not at "roughly
that head", and the reported result is that run's output.

### The suite

```sh
gofmt -l .
go vet ./...
go test ./... -race -count=2 -timeout 20m
```

`-count=2` is deliberate: a suite run once is a suite whose flakes are invisible,
and the second run adds scheduling pressure that catches the common ordering
races.

`internal/audit` runs as part of `./...`; there is no separate audit step to
remember.

### The coverage gate

`cmd/covercheck` compares a base profile against a head profile. CI runs it with:

| Flag | Value | Meaning |
| --- | --- | --- |
| `--minimum-changed` | 70 | at least 70% of the blocks the patch touched must be covered |
| `--maximum-total-regression` | 1 | total coverage may not fall more than 1pp |
| `--maximum-file-regression` | 1 | **no single file** may fall more than 1pp |
| `--require-changed-files` | — | a patch touching Go files must produce changed-block data |

Reproduce it locally with the **same flags** — a locally-relaxed gate is not a
rehearsal of the gate that will run.

The per-file limit is the one that catches real gaps, and it is not an
accounting rule. A file at 100% that grows by a large, less-covered function has
usually grown a path no test reaches. The right response is to find out which
path and test it; adding an exemption records that nobody looked.

> A worked case: `tui/widget/overlay.go` regressed 12.07pp when modal stacking
> landed. The uncovered blocks were the backdrop hand-back to a dialog
> underneath and `TopModal`'s empty answer — two behaviours a single dialog never
> reaches. Both got tests, both got mutation cells, and the file returned to
> 100%.

### The order

Run the gates **before** the push, not after. The push is the claim; the gates
are what make it true.
