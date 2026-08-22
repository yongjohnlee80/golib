# browsertest — the required browser matrix

ADR-0009 §2.9 makes a real Chromium/Firefox/WebKit run a **release gate**, not an
optional extra. The reason is specific: the capture-element text machine depends
on behaviours the specs decline to promise — whether an engine updates a control
*before* dispatching `compositionend`, how a dead key sequence surfaces, whether
`getModifierState("AltGraph")` is reported at all — and **synthetic dispatch is
exactly what would hide a divergence.** A suite that fabricates events tests the
fabrication.

So this harness drives real engines through Playwright against a real
`tui/web` server.

## Running it

```
cd tui/web/browsertest
npm install
npx playwright install --with-deps chromium firefox webkit
npm test
```

The Go side is started by the harness itself (`server.go` builds a fixture
server on an ephemeral loopback port and prints its URL plus a ticket).

## What it covers

Every sequence from ADR-0009 §2.9 that a Go test cannot observe:

| Case | ADR |
|---|---|
| ordinary typing emits one event per rune | 7 |
| composition, `input` **before** `compositionend` | 7a (i) |
| composition, `input` **after** `compositionend` | 7a (ii) |
| `compositionend` with empty `data` but a staged value → commits | 7a (iii) |
| genuine cancellation → emits nothing | 7a (iv) |
| composed `x` then a separately typed `x` → **two** insertions | 7a (v) |
| cancelled paste then typing → the typed character survives | 7a (vi) |
| paste with no trailing `input` → exactly one `PasteEvent` | 7a (vii) |
| composition-associated `input` in a LATER task → no duplicate | 7a (viii) |
| dead-key sequence, non-US layout, multi-codepoint emoji | 7b |
| `Dead`/`Unidentified` dropped with no phantom event | 7b |
| the capture buffer is empty after every path | 7d |
| a long typing stream leaves the DOM value size constant | 7d |
| reserved shortcuts are not forwarded | 2.9 rule 1 |

## Status

**NOT YET GREEN ON ALL THREE ENGINES.** See `RESULTS.md`, which records exactly
which engines have been run and which have not. A release with any engine unrun
is not a release (§2.9), and this file will not claim otherwise.
