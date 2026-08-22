# Browser matrix results

ADR-0009 §2.9 makes a green Chromium/Firefox/WebKit run a release gate. This file
records what has actually been **run**, so that "the matrix exists" is never
mistaken for "the matrix passed".

| Engine | Status | Evidence |
|---|---|---|
| **Chromium** | **PASSED** | CI, 2026-08-22, PR #14 run 32575804138; and 16/16 locally |
| **Firefox** | **PASSED** | CI, 2026-08-22, PR #14 run 32575804138; and 16/16 locally, Firefox 153.0 (playwright firefox v1538). Its first CI run, 32574695356, failed 1/16 on paste — see below. |
| **WebKit** | **PASSED** | CI, 2026-08-22, PR #14 run 32575804138. Cannot run locally: playwright's WebKit needs Ubuntu libraries this Arch host has no package for. |

The required aggregate check, `browser matrix (required)`, is **green** — the first
time it has been, and the first time every engine has run.

CI is wired — `.github/workflows/browser-matrix.yml`, with a single required
`browser matrix (required)` check that fails unless **every** engine passes.

## All three engines have now run (2026-08-22)

The waiver below was cashed in on PR #14, the first CI run of the matrix. The
result: **Chromium and WebKit green, Firefox 15/16**, failing only
`paste emits exactly one PasteEvent with normalized newlines`. After the repair
described here, run 32575804138 is green on all three.

That failure was the **third harness defect** the matrix has produced, and still
**no product defect**. `new ClipboardEvent('paste', { clipboardData: dt })` looks
like it hands the payload over and does not: Firefox ignores the constructor member
and substitutes its own empty `DataTransfer`, so `getData('text')` returned `''`.
The client then *correctly* declined to send an empty paste — the same behaviour the
neighbouring cancelled-paste test asserts — and the spec read that correct
behaviour as a product failure.

The repair verifies that the payload survived construction and shadows the readonly
accessor only where it did not, which leaves Chromium and WebKit dispatching
exactly as they already did. It also fails on the *dispatch* rather than three
lines later on the assertion, because "the payload never reached the page" and "the
client dropped the payload" are different bugs and must not produce the same red.

The composition, dead-key and AltGraph behaviours this file named as the most
likely divergences all passed on Gecko and WebKit unchanged.

## Gate status: SATISFIED (2026-08-22). The v0.3.8 waiver is spent.

The gate is met by evidence, not by exception: every engine has run and passed.
The history is kept below because a waiver that vanishes once it is convenient is
not a record of anything.

**The waiver, as it stood for v0.3.8:**

> Johno accepted merging and tagging v0.3.8 with Chromium green and Firefox and
> WebKit unrun (2026-08-22), after being shown this status explicitly.

**What it cost, and what closing it bought.** For v0.3.8 the text machine was
verified on **one** engine, and the behaviours it depends on are precisely the ones
engines differ over — whether a control is updated before `compositionend`
dispatches, whether a composition-associated `input` arrives in the same task,
whether `getModifierState("AltGraph")` is reported. Gecko and WebKit were the two
most likely to diverge and the two unrun, so a Firefox or Safari user could have
hit a composition or dead-key defect this suite would have caught.

They have now run. **None of those divergences exists**: every composition,
dead-key and AltGraph case passed on Gecko and WebKit unchanged. The only failure
on either engine was a defect in the harness's own paste dispatch, described above.
That is the answer to the question the waiver left open, and it is a better answer
than "probably fine".

## Why this file exists rather than a line in the README

The harness existing and the harness passing are different facts, and only the
second is the gate. Recording just the first is how a gate becomes decorative.

## What the Chromium run already caught

Both were real, and neither was findable from Go:

1. **An empty event log encoded as JSON `null`, not `[]`.** Several §2.9 cases
   assert "emits nothing", so the empty case is the one they exercise most — and
   `null.filter` is a TypeError, which turned a *passing property* into an error.
   The fixture now returns a non-nil empty slice.
2. **`KeyEvent.Kind` is omitted when zero**, so "absent" means `KeyPress`. Worth
   knowing before someone reads the log and concludes a key had no kind.

Neither is a product defect, which is itself informative: the first real-engine
run of the text machine found nothing wrong with the text machine. That is
evidence, not proof — two engines remain.

## Local run log

- **2026-08-22** — chromium, 16/16 passed in 14.3s. Covers §2.9 criterion 7 (per-rune
  emission), 7a (i) (iii) (iv) (v) (vi) (vii) (viii), 7b (multi-codepoint emoji,
  Dead/Unidentified dropped), 7d (buffer drained, constant DOM value size,
  password-like text absent), rule 1 (reserved shortcuts not forwarded), named-key
  codes, paste newline normalization, and §2.6 wide-grapheme containment
  (`span 2` plus `overflow: hidden` measured from the DOM).
- **2026-08-22** — firefox, 16/16 passed in 20.0s, after the paste-dispatch repair.
  Run alone: running two projects in one invocation is unreliable locally because
  the fixture's event log is process-global state shared by both browsers. CI gives
  each engine its own job and its own fixture, which is why it does not see this.
- **2026-08-22** — webkit, passed in CI (PR #14). Not runnable on this host.
