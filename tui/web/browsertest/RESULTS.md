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

**The gate is therefore NOT satisfied.** Two of three engines are unrun, and per
§2.9 a release with any engine unrun is not a release.

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
