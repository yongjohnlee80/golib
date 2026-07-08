# tui/term

The real ANSI terminal driver behind `tui.Backend` (ADR-0002): raw mode,
alternate screen, a spec-driven DEC ANSI input parser, kitty + legacy key
decoding, SGR mouse, bracketed paste, a DA1-fenced startup capability probe,
resize handling on unix and Windows, and panic-safe terminal restoration.

This is the **only** golib package that imports `golang.org/x/term` and
`golang.org/x/sys` (ADR-0001 §2.2). Everything above it — `tui`, `tui/style`,
`tui/widget` — is stdlib-only and tests against the in-memory
`tui.TestBackend`, never against this driver.

```bash
go get github.com/yongjohnlee80/golib/tui/term
```

```go
backend, err := term.Open()
if err != nil { ... }           // ErrNotTerminal when stdin/stdout is not a TTY
if err := backend.Start(ctx); err != nil { ... }
defer backend.Stop()            // idempotent; restores on error AND panic paths

for ev := range backend.Events() {
    switch ev := ev.(type) {
    case tui.KeyEvent:   ...
    case tui.MouseEvent: ...
    case tui.ResizeEvent: ...
    }
}
if err := backend.Err(); err != nil { ... } // reader failure vs clean close
```

In practice the runtime (`tui.App`, ADR-0005) owns this whole lifecycle; apps
construct the backend and hand it over.

## Options

```go
term.WithTTY(in, out)          // default os.Stdin, os.Stdout
term.WithProbeTimeout(250*time.Millisecond) // clamped [50ms, 1s]
term.WithEscTimeout(35*time.Millisecond)    // legacy ESC disambiguation hold
term.WithoutAltScreen()        // inline mode: never enter ?1049
term.WithoutMouse()            // never enable ?1002/?1006
term.WithEnv(lookup)           // env seam for the capability pre-seed
```

## The capability probe

`Start` resolves `tui.Capabilities` by **live probing** — never terminfo
(ADR-0001 §2.4 #4). One batched write:

| Query | Sets |
|---|---|
| DECRQM ?2004 | `BracketedPaste` |
| DECRQM ?2026 | `SyncOutput` (synchronized output brackets in Flush) |
| DECRQM ?2027 | `UnicodeCore` (grapheme-cluster semantics) |
| DECRQM ?2048 | `InBandResize` |
| DECRQM ?1006 | `Mouse` (tri-state: `TriYes` only on a verifiable answer) |
| XTGETTCAP RGB;Smulx | `ColorProfile` upgrade, `Undercurl` |
| OSC 10 / OSC 11 | `DefaultFG` / `DefaultBG`, `DarkBackground` |
| CSI ? u | `KittyKeyboard` |
| **DA1 (`CSI c`)** | the fence — terminates the probe |

Every real terminal answers DA1 and replies arrive in request order, so
healthy terminals never pay the probe timeout; silence at the fence marks a
capability unsupported (`Mouse` stays `TriUnknown` — an attempted enable is
never reported as support). `ColorProfile` is pre-seeded from `$COLORTERM` /
`$TERM` and only ever **upgraded** by replies, never downgraded by silence.
When OSC 11 goes unanswered, `DarkBackground` is **assumed true** — the
documented, fixed fallback.

## Input

One DEC ANSI parser (vt100.net's 13-state machine, `:` sub-parameters,
incremental across arbitrary read boundaries) decodes every platform's input
byte stream — on Windows, `ENABLE_VIRTUAL_TERMINAL_INPUT` makes the console
speak VT into stdin, so there is no second input path.

- **Kitty keyboard** (flags 1+2 pushed when probed): `CSI u` decoding with
  alternates, press/repeat/release, and no ESC ambiguity.
- **Legacy fallback**: CSI/SS3 arrows and function keys, the xterm
  `1 + bitmask` modifier encoding, Alt as ESC prefix, Ctrl as `byte & 0x1F`.
  A lone ESC is held `WithEscTimeout` (default 35ms) and delivered as the
  Escape key if no continuation arrives — disabled entirely under kitty.
- **Mouse**: SGR encoding only (`?1002` + `?1006`) — press, release, drag
  motion, wheel, with shift/alt/ctrl bits.
- **Paste**: `?2004` brackets become a single `tui.PasteEvent` with CR/CRLF
  normalized to `\n`; embedded escapes are captured literally; an
  unterminated paste is flushed on Stop, not dropped.
- **Resize**: SIGWINCH (unix) / a 250ms console poll (Windows), both
  re-querying fresh size; mode-2048 in-band reports take over when probed.
  Events are emitted ordered and un-coalesced — the App intake coalesces.

## Output

`Flush(diff)` follows ADR-0003's anti-flicker rules: hide cursor during
paint (R1), overwrite in place — never clear-then-redraw (R2), the whole
frame as **one `Write`** (R3), and mode-2026 synchronized-update brackets
when supported (R4). CUP is emitted only on discontinuity, SGR only on
attribute change, and after a risky cluster (ZWJ/VS16/flag emoji) on a
non-mode-2027 terminal the cursor is re-anchored absolutely. Cursor
operations are latched and land inside the same write. An empty diff with
unchanged cursor state writes zero bytes.

## Restoration guarantees

`Stop` runs under a `sync.Once` and restores in reverse order of
acquisition: kitty pop, mode disables, cursor shape/visibility/SGR reset,
alternate-screen exit — one final write — then termios / console-mode
restore, then the reader is joined and `Events()` closed. `App.Run` defers
`Stop`, so error returns and panics restore the terminal before propagating.
`Start(ctx)` cancellation mid-probe discards partial capability replies,
unwinds acquired state, and returns `ctx.Err()`.

## Platform matrix (ADR-0002 §5.9)

Expected negotiation per the ADR-0002 research dossier. Entries marked *(to
verify)* await a manual pass on real hardware; the probe degrades
per-capability, so a wrong expectation costs a feature, never corruption.

| Terminal | Kitty kbd | 2026 sync | 2027 unicode | 2048 resize | Mouse (SGR) | Paste | Truecolor |
|---|---|---|---|---|---|---|---|
| kitty (Linux) | yes | yes | yes | yes | yes | yes | yes |
| foot | yes | yes | yes | yes *(to verify)* | yes | yes | yes |
| ghostty | yes | yes | yes | yes | yes | yes | yes |
| tmux (on modern outer) | no *(passes through per outer; to verify)* | yes | tmux 3.4+ | no | yes | yes | with `tmux-256color` + RGB |
| macOS Terminal.app | no | no | no | no | yes | yes | no (256) |
| Windows Terminal | yes | yes | no *(to verify)* | no ([#19618](https://github.com/microsoft/terminal/issues/19618)) | yes | yes | yes |
| legacy conhost | no | no | no | no | no | no | 1703+ |

Legacy conhost degrades to: no mouse, no paste brackets, no sync — keys and
cell rendering still work through VT processing (floor: Windows 10 1809+,
`ErrConsoleTooOld` below that).

## File layout

| File | Contents |
|---|---|
| `term.go` | `Backend`, `Open`, `Start`/`Stop`, reader goroutines, teardown |
| `options.go` | `Option` set, sentinels, tunables |
| `parser.go` | the DEC ANSI state machine (pure, I/O-free) |
| `decoder.go` | actions → tui events; kitty/legacy keys, mouse, paste, probe replies |
| `probe.go` | the DA1-fenced capability probe + env pre-seed |
| `flush.go` | the frame emitter (R1–R4) + latched cursor ops |
| `tty_unix.go` / `tty_windows.go` | raw mode, VT modes, size query, read unblock |
| `resize_unix.go` / `resize_windows.go` | SIGWINCH / console size poll |

## License

[MIT](../../LICENSE)
