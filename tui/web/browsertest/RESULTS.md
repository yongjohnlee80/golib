# Browser matrix results

ADR-0009 §2.9 makes a green Chromium/Firefox/WebKit run a release gate. This file
records what has actually been **run**, so that "the matrix exists" is never
mistaken for "the matrix passed".

| Engine | Status | Evidence |
|---|---|---|
| **Chromium** | **16/16 PASSED** | local, 2026-08-22, Chrome Headless Shell 151.0.7922.34 (playwright chromium-headless-shell v1234) |
| Firefox | **NOT RUN** | needs `npx playwright install firefox` |
| WebKit | **NOT RUN** | needs `npx playwright install webkit` |

CI is wired — `.github/workflows/browser-matrix.yml`, with a single required
`browser matrix (required)` check that fails unless **every** engine passes. **No
CI run has executed yet**, because the workflow lands with this commit; GitHub
reported zero check runs for the branch before it.

## Gate status: WAIVED for v0.3.8 by Johno, 2026-08-22

Two of three engines are unrun. Per §2.9 that means the gate is **not satisfied**,
and rather than quietly tagging around it the waiver is recorded here:

> Johno accepted merging and tagging v0.3.8 with Chromium green and Firefox and
> WebKit unrun (2026-08-22), after being shown this status explicitly.

What that costs, stated so nobody has to reconstruct it later: the text machine is
verified on **one** engine. The behaviours it depends on are precisely the ones
engines differ over — whether a control is updated before `compositionend`
dispatches, whether a composition-associated `input` arrives in the same task,
whether `getModifierState("AltGraph")` is reported. Gecko and WebKit are the two
most likely to diverge, and they are the two unrun. A Firefox or Safari user may
hit a composition or dead-key defect this suite would have caught.

The gate is **not** removed: `.github/workflows/browser-matrix.yml` still requires
all three, so the next CI run on this branch or on main will report Firefox and
WebKit for the first time. Running them is the first item of follow-up work, not a
someday.

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
- Firefox — not run.
- WebKit — not run.
