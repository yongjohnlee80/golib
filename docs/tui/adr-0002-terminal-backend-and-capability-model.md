
# ADR-0002 — golib/tui: Terminal Backend & Capability Model

**Tags:** `type:adr` `status:accepted` `owner:shared` `repo:golib` `area:tui` `area:term` `kind:backend` `kind:capabilities` `kind:input` `platform:windows`

**Abstract:** Defines the `tui.Backend` driver seam (Ratatui-Backend-shaped: lifecycle, size, cell-diff flush, latched cursor ops, capability report, single event channel), the deterministic in-memory `tui.TestBackend`, and the concrete `tui/term` ANSI implementation — raw mode on unix and Windows, the DEC ANSI input parser, legacy + kitty key decoding, SGR mouse, bracketed paste, alternate screen, the DA1-fenced startup capability probe, resize handling on both platforms, and panic-safe terminal restoration.

> **Authored natively in the KB** (jarvis, 2026-07-08) as part of the golib/tui design
> dossier. golib/tui keeps its own 0001–0007 ADR numbering; the `golib-tui-` filename
> prefix namespaces it against this KB's auto-agents ADR sequence. Navigation hub:
> golib-tui. Umbrella ADR: golib-tui-0001-overview-and-architecture.

- **Status:** **Accepted** (Johno, 2026-07-08) — revision 1 (Lector r1 `change_requested` → r2 `approved_with_amendments`, all amendments folded).
- **Date:** 2026-07-08
- **Reviewed by:** lector, 2026-07-08 — agents/lector/reviews/2026-07-08-golib-tui-adrs-review.md (r1); agents/lector/reviews/2026-07-08-golib-tui-adrs-rereview.md (r2, `approved_with_amendments`)
- **Module:** github.com/yongjohnlee80/golib
- **Supersedes:** none (greenfield)
- **Related:** ADR-0001 (umbrella — layering, dependency policy, probe-don't-database
  decision this ADR implements), ADR-0003 (cell buffer — defines `Cell` and produces the
  diff `Flush` consumes; the one-write rule constrains this ADR's emitter), ADR-0005
  (runtime — the App loop is the sole consumer of `Events()` and drives `Flush`),
  ADR-0006 (styling — `Capabilities` feeds `style.Color` resolution). golib server
  ADR-0006 (transport scaffold — the `Run(ctx)`/teardown lifecycle discipline the
  backend's Start/Stop mirrors).

> **Revision history.** rev 1 (2026-07-08): folded Lector r1 `change_requested` —
> (1) must-fix #1: capability/color model unified with ADR-0006 — `ColorProfile` enum
> replaces the bare `Truecolor bool`, `DarkBackground bool` added with a documented
> assume-dark fallback, `Mouse` becomes tri-state `Tri` backed by a DECRQM ?1006 probe
> row (§2.2, §2.6, §2.7); (2) must-fix #2: input queue ownership resolved — the backend
> emits ordered, **un-coalesced** events only, the App intake stage (ADR-0005) owns all
> coalescing/overflow policy, and `Backend` gains `Err() error` (§2.1, §2.8, §2.9,
> §2.10); (3) should-fix: default probe timeout raised 100 ms → 250 ms (§2.4, §2.6);
> (4) nit: `Start(ctx)` cancellation-during-probe semantics specified — abort, discard
> partial replies, restore, return `ctx.Err()` (§2.1, §2.6); (5) nit:
> `TestBackend.Inject` overflow now errors, the event buffer is configurable, and
> `SetErr` was added (§2.3); (6) Lector's Q1–Q5 answers annotated in §6.

> **Self-containment contract.** This ADR is implementable with no prior context beyond
> the golib conventions (golang, golib) and the sibling ADRs listed under Related.

## 1. Context

ADR-0001 fixes two load-bearing decisions this ADR must realize: portability lives in the
`Backend` seam (§2.4 #5), and capability negotiation is live probing, never terminfo
(§2.4 #4). Everything terminal-facing is new code — golib has zero existing terminal/ANSI
surface — so this ADR is the full specification of the lowest layer: what a terminal *is*
to the rest of the framework, and how `tui/term` implements it on unix and Windows.

The evidence base (research dossier, 2026-07-08) constrains the design:

- The Go standard library has no raw mode, no terminal detection, no size query
  (https://go.dev/doc/go1.25). `golang.org/x/term` v0.44.0 provides exactly
  `IsTerminal`/`MakeRaw`/`GetState`/`Restore`/`GetSize`
  (https://pkg.go.dev/golang.org/x/term). On Windows, std `syscall` lacks
  `SetConsoleMode` entirely — output VT mode requires `x/sys/windows`
  (https://pkg.go.dev/syscall?GOOS=windows).
- x/term's Windows `MakeRaw` sets `ENABLE_VIRTUAL_TERMINAL_INPUT` on stdin but **never
  touches stdout** (https://raw.githubusercontent.com/golang/term/master/term_windows.go);
  the driver must set output VT mode itself.
- `$TERM` lies, terminfo is stale, and tmux re-emits with its own capability set. Modern
  stacks (Neovim, vaxis — go.rockorager.dev/vaxis) probe the live terminal at startup
  with a DA1 fence; tcell's embedded terminfo DB is the legacy road.
- Legacy key encoding cannot represent Ctrl+I vs Tab, key release, or ESC-vs-sequence
  without a timeout — the reasons the kitty keyboard protocol exists
  (https://sw.kovidgoyal.net/kitty/keyboard-protocol/).
- Ratatui's `Backend` trait is the canonical minimal driver surface, and its
  `TestBackend` proves the seam pays for itself in deterministic tests
  (https://docs.rs/ratatui/latest/ratatui/backend/trait.Backend.html).

### 1.1 Goals

- **G1** — One small `tui.Backend` interface such that component and runtime code never
  names a platform: real terminal, test double, and future SSH/web drivers all satisfy it.
- **G2** — Modern input from day one: kitty keyboard flags 1+2 with graceful legacy
  fallback, SGR mouse, bracketed paste — decoded by a single spec-driven parser shared
  across unix and Windows.
- **G3** — A startup capability probe with a hard, bounded latency budget (250 ms
  worst-case default, one batched write; the DA1 fence returns far earlier on healthy
  terminals), producing a `Capabilities` value the rest of the stack treats as ground
  truth.
- **G4** — Guaranteed terminal restoration on every exit path — normal stop, error,
  panic — with `sync.Once` discipline (precedent: `server/ws/ws.go` teardown).
- **G5** — Windows Terminal parity via VT input mode; documented floor Windows 10 1809+.
- **G6** — `tui.TestBackend` deterministic enough that a full interaction script asserts
  cell-exact frames in CI with no PTY.
- **G7** — x-repo dependencies (`x/term`, `x/sys`) confined to `tui/term` (ADR-0001 §2.2);
  the `Backend` interface and `TestBackend` live in stdlib-only `tui`.

### 1.2 Non-goals

- **N1** — terminfo parsing or an embedded capability database (ADR-0001 N4).
- **N2** — Pixel-aware backend methods (window pixel size, sixel/kitty graphics). The
  seam permits adding them later; v1 is cells only (ADR-0001 N1).
- **N3** — xterm `modifyOtherKeys` as a third keyboard tier (see §4.4).
- **N4** — Support for pre-VT Windows consoles (Win10 before 1809, legacy conhost APIs as
  a rendering path). Probe-degraded features on old conhost are acceptable; a non-VT
  output path is not built.
- **N5** — Multiple simultaneous backends per App, terminal multiplexing, or session
  persistence.

## 2. Decision

### 2.1 The `tui.Backend` interface

Defined in package `tui` (stdlib-only). Ratatui-Backend-shaped
(https://docs.rs/ratatui/latest/ratatui/backend/trait.Backend.html): a cell-diff sink, an
event source, and a capability report behind one lifecycle.

```go
package tui

// Backend is the driver seam between the runtime and a concrete terminal
// (ADR-0001 §2.4 #5). The App owns exactly one Backend for its lifetime.
type Backend interface {
	// Start acquires the device: raw mode, VT modes, alternate screen,
	// the capability probe, and the input reader goroutine. It blocks
	// until the probe fence resolves or the probe timeout expires
	// (bounded, 250ms by default; §2.6), then returns. Cancelling ctx
	// during the probe aborts it, discards partial capability replies,
	// restores the terminal, and returns ctx.Err() (§2.6).
	Start(ctx context.Context) error

	// Stop restores the terminal completely and stops the reader
	// goroutine. Idempotent (sync.Once). Safe from deferred
	// panic-recovery paths. After Stop returns, Events() is closed.
	Stop() error

	// Size reports the current cell-grid size.
	Size() (Size, error)

	// Flush applies one frame's cell diff plus any latched cursor-state
	// changes as a SINGLE buffered write (the one-write rule, ADR-0003
	// §2.5/§2.9). An empty diff with unchanged cursor state writes zero
	// bytes. The diff is ordered row-major by the caller (ADR-0003).
	Flush(diff []CellUpdate) error

	// Cursor state is LATCHED, not immediate: these record desired state
	// which the next Flush emits, so a frame is always one write.
	ShowCursor()
	HideCursor()
	SetCursor(x, y int)
	SetCursorShape(s CursorShape)

	// Capabilities reports the negotiated profile. Constant after Start.
	Capabilities() Capabilities

	// Events is the single, ordered, UN-COALESCED event source: key,
	// mouse, paste, resize, focus — the backend only decodes and emits.
	// Fed by the backend's one reader goroutine (§2.9); closed by Stop.
	// The App's intake stage consumes it exclusively and owns ALL
	// coalescing and overflow policy (ADR-0005).
	Events() <-chan Event

	// Err reports the terminal error that stopped the reader or failed
	// the probe. Valid once Events() is closed or Stop has returned; nil
	// after a clean Stop. The App loop calls Err when Events() closes to
	// distinguish clean shutdown from reader failure (ADR-0005).
	Err() error
}

// CellUpdate is one dirty cell in a frame diff. Cell is ADR-0003's
// grapheme-cluster cell.
type CellUpdate struct {
	X, Y int
	Cell Cell
}

type CursorShape uint8

const (
	CursorShapeDefault CursorShape = iota // terminal's configured default
	CursorShapeBlock                      // DECSCUSR 1/2
	CursorShapeUnderline                  // DECSCUSR 3/4
	CursorShapeBar                        // DECSCUSR 5/6
)
```

> **Rev 1 (Lector must-fix #2).** `Err()` added and `Events()` re-specified as ordered
> and un-coalesced. The backend's job ends at decoding: the App intake stage (ADR-0005,
> amended in parallel) pulls promptly and owns every coalescing/overflow decision, and
> reads `Err()` after channel close to distinguish clean stop from reader failure.

Latched cursor ops are a deliberate deviation from Ratatui (whose cursor ops write
immediately): immediate writes would break ADR-0003's flicker rule R3 (whole frame = one
`Write`). The runtime calls cursor methods during layout/render and everything lands in
the same syscall as the cell diff.

### 2.2 The `Capabilities` model

```go
// ColorProfile is the canonical color-capability tier. ADR-0006 resolves
// style.Color against exactly this enum — it is the field style
// resolution consumes; the raw probed colors below are supporting data.
type ColorProfile uint8

const (
	ProfileMono      ColorProfile = iota // no color
	ProfileANSI16                        // the 16 base SGR colors
	ProfileANSI256                       // indexed 256 (SGR 38;5)
	ProfileTrueColor                     // 24-bit RGB (SGR 38;2)
)

// Tri is a three-valued capability answer for features whose support can
// be genuinely unknowable. An optimistic request is NEVER reported as
// support — capability honesty.
type Tri uint8

const (
	TriUnknown Tri = iota // requested/attempted; no verifiable answer
	TriNo                 // verifiably unsupported
	TriYes                // verifiably supported
)

// Capabilities is the negotiated feature profile of the attached terminal,
// resolved once during Start by live probing (§2.6) — never from terminfo
// (ADR-0001 §2.4 #4). Degradation is per-capability, not per-$TERM.
type Capabilities struct {
	ColorProfile   ColorProfile // env pre-seed + XTGETTCAP "RGB" (§2.6); consumed by ADR-0006
	KittyKeyboard  bool         // kitty progressive enhancement; flags 1+2 pushed when true
	SyncOutput     bool         // DEC private mode 2026 (synchronized output)
	InBandResize   bool         // mode 2048 in-band resize reports
	UnicodeCore    bool         // mode 2027 grapheme-cluster semantics (consumed by ADR-0003)
	BracketedPaste bool         // mode 2004
	Mouse          Tri          // SGR mouse — TriYes only on a verifiable DECRQM ?1006 answer
	Undercurl      bool         // XTGETTCAP "Smulx" (styled underlines; ADR-0006)

	// DarkBackground derives from the OSC 11 reply (relative luminance of
	// DefaultBG < 0.5). The unknown-fallback is DOCUMENTED AND FIXED: when
	// the query goes unanswered, ASSUME DARK — the statistically safer
	// default for terminals. ADR-0006 adaptive colors key off this bit.
	DarkBackground bool

	// Raw probed default colors, retained for style resolution and
	// diagnostics; Known=false when the OSC query went unanswered.
	DefaultFG ProbedColor
	DefaultBG ProbedColor
}

type ProbedColor struct {
	R, G, B uint8
	Known   bool
}
```

`ColorProfile` derivation (pre-seeded from the environment, §2.6; upgraded by probe
replies, never downgraded by silence): `ProfileTrueColor` when
`$COLORTERM ∈ {truecolor, 24bit}` (https://github.com/termstandard/colors) or XTGETTCAP
confirms `RGB`; else `ProfileANSI256` when `$TERM` contains `256color`; else
`ProfileANSI16`. `ProfileMono` is reserved for `TERM=dumb`-class devices (which normally
fail `Open`'s TTY check anyway) and explicit future options.

> **Rev 1 (Lector must-fix #1).** The capability/color model is unified with ADR-0006:
> `ColorProfile` — the enum ADR-0006's `style.Color` resolution consumes — replaces the
> bare `Truecolor bool`; `DarkBackground` is a first-class derived field with a
> documented assume-dark fallback (raw `DefaultFG/BG` retained); and `Mouse` becomes
> tri-state — a merely-requested, unverifiable enable is `TriUnknown`, never boolean
> `true` (this also folds Lector's answer to this ADR's Q4).

A flat value struct (not per-capability methods) because — unlike `dao.Dialect`'s
predicate style (golib-dao-0008-read-mostly-no-transaction-driver-contract §2.2) —
this is a *report* produced once by probing, not a contract drivers implement; a struct
copies freely into `Surface` style-resolution context (ADR-0003 §2.4) and `TestBackend`
constructs arbitrary profiles for tests.

### 2.3 `tui.TestBackend`

Lives in package `tui` (stdlib-only), mirroring Ratatui's `TestBackend` and tcell's
`SimulationScreen`: a deterministic, PTY-free in-memory terminal.

```go
func NewTestBackend(w, h int, opts ...TestBackendOption) *TestBackend

func WithTestCapabilities(c Capabilities) TestBackendOption // default: everything on
func WithTestEventBuffer(n int) TestBackendOption           // default 1024

// Scripted input: enqueues events onto the Events() channel in call order.
// Returns an error when the script exceeds the configured buffer instead
// of blocking the test goroutine (fail loud).
func (b *TestBackend) Inject(evs ...Event) error

// SetErr scripts the terminal error surfaced by Err() after the channel
// closes — for driving ADR-0005's reader-failure loop paths in tests.
func (b *TestBackend) SetErr(err error)

// Resizes the grid, invalidates it, and posts a ResizeEvent — exactly the
// externally observable behavior of a real resize.
func (b *TestBackend) InjectResize(w, h int)

// Assertions.
func (b *TestBackend) Snapshot() [][]Cell               // deep copy of the grid
func (b *TestBackend) String() string                   // grid as text, row per line
func (b *TestBackend) CursorPos() (x, y int, visible bool)
func (b *TestBackend) Flushes() int                     // Flush count — write-count assertions
```

Semantics: `Start` and `Stop` only manage the channel; `Flush` applies the diff to the
grid, records the latched cursor state, and increments the flush counter. `Flush`
**panics** if a diff would leave an orphaned wide-cell half (ADR-0003 §2.3 invariant) —
fail loud in tests, where the panic is a test failure with a coordinate in the message.
`Events()` is buffered (default 1024, `WithTestEventBuffer`); `Inject` returns an error
on overflow rather than blocking or silently dropping. This is the backend behind
ADR-0001 acceptance criterion 3 (full interaction script in CI, no PTY).

> **Rev 1 (Lector nit).** `Inject` no longer claims "never blocks": the buffer is
> configurable and overflow returns an error. `SetErr` was added alongside must-fix #2's
> `Err()` so reader-failure loop paths are scriptable without a real terminal.

### 2.4 `term.Open` and terminal acquisition

```go
package term // tui/term — the ONLY package importing x/term, x/sys (ADR-0001 §2.2)

// Open validates the TTY and builds the backend WITHOUT touching terminal
// state; all mode changes happen in Start so a constructed-but-unstarted
// backend is inert. Fails loud, typed (golib convention).
func Open(opts ...Option) (*Backend, error)

type Option func(*config)

func WithTTY(in, out *os.File) Option                    // default os.Stdin, os.Stdout
func WithProbeTimeout(d time.Duration) Option            // default 250ms (rev 1), clamped [50ms, 1s]
func WithEscTimeout(d time.Duration) Option              // legacy ESC disambiguation, default 35ms
func WithoutAltScreen() Option                           // inline mode: never enter ?1049
func WithoutMouse() Option                               // never enable ?1002/?1006
func WithEnv(lookup func(string) (string, bool)) Option  // default os.LookupEnv; test seam

var (
	ErrNotTerminal   = errors.New("term: file descriptor is not a terminal")
	ErrClosed        = errors.New("term: backend is stopped")
	ErrConsoleTooOld = errors.New("term: console lacks VT processing (Windows 10 1809+ required)")
)
```

`Open` errors with `ErrNotTerminal` when `term.IsTerminal` fails on either fd. `Start`
performs, in order: (1) raw mode, (2) enter alternate screen (`CSI ?1049 h`, unless
`WithoutAltScreen`), (3) start the reader goroutine, (4) run the capability probe (§2.6),
(5) enable negotiated modes (§2.5, §2.7). Every step's undo is registered before the step
runs, so a failure mid-`Start` restores exactly what was acquired (`errors.Join` on the
undo results — the `server/scaffold.go` teardown discipline).

**Unix raw mode.** `term.MakeRaw(fd)` — which clears `OPOST` (the emitter writes explicit
`\r\n`… irrelevant in practice since the renderer cursor-addresses everything) and clears
`ISIG`: **Ctrl-C arrives as byte 0x03 and is a `KeyEvent`, not a signal**. The framework
never re-raises SIGINT; quit policy belongs to the app/runtime (ADR-0005).
(https://pkg.go.dev/golang.org/x/term — MakeRaw termios semantics per term_unix.go.)

**Windows raw mode + output VT.** `term.MakeRaw` on stdin sets
`ENABLE_VIRTUAL_TERMINAL_INPUT` and clears line/echo/processed input — but never touches
stdout (https://raw.githubusercontent.com/golang/term/master/term_windows.go). `Start`
therefore additionally sets, via `windows.SetConsoleMode` on the stdout handle:

```
ENABLE_VIRTUAL_TERMINAL_PROCESSING (0x0004) | DISABLE_NEWLINE_AUTO_RETURN (0x0008)
```

with graceful degradation: if setting both fails with `ERROR_INVALID_PARAMETER`, retry
with `ENABLE_VIRTUAL_TERMINAL_PROCESSING` alone (older builds reject
`DISABLE_NEWLINE_AUTO_RETURN`); if even that fails, `Start` returns `ErrConsoleTooOld` —
there is no non-VT rendering path (N4). Saved input+output modes are restored on Stop.
(https://learn.microsoft.com/en-us/windows/console/console-virtual-terminal-sequences)

**The Windows model (dossier §7).** With `ENABLE_VIRTUAL_TERMINAL_INPUT`, the console
encodes keys, and Windows Terminal encodes mouse/paste, as VT sequences **into the stdin
byte stream** — so there is exactly **one** input parser (§2.5) on every platform; no
`INPUT_RECORD` decoding path exists in this design. Supported floor: **Windows 10 1809+**
(ConPTY; VT output since 1511, truecolor since 1703). Legacy conhost lacks mouse (any
mode), bracketed paste, and modes 2026/2027 — the probe simply reports those capabilities
absent (the mode flags false, `Mouse` as `TriNo`/`TriUnknown`) and the framework degrades
per-capability; Windows Terminal passes the full modern profile. `$WT_SESSION` is used only as a heuristic pre-seed, never as authority.

### 2.5 Input parsing and key decoding

**Parser.** A byte-at-a-time, incremental implementation of Paul Flo Williams' DEC ANSI
parser state machine (https://vt100.net/emu/dec_ansi_parser): the ~13 states (ground,
escape, escape_intermediate, csi_entry/csi_param/csi_intermediate/csi_ignore, osc_string,
dcs_entry/dcs_param/dcs_intermediate/dcs_passthrough/dcs_ignore, sos_pm_apc_string) with
byte-class transitions, ESC-from-anywhere restart, CAN/SUB abort. Extensions over the
1990s model, per modern practice: `:` is accepted as a **sub-parameter separator** inside
CSI params (kitty keys, SGR colon-form truecolor in DECRQSS replies); parameter storage
allows 32 params × 4 sub-params, saturating (excess ignored, sequence still consumed).
The parser is a pure `func (p *parser) feed(b byte) (actions…)` state machine with no I/O
— exhaustively table-testable, including sequences split across arbitrary read
boundaries.

**Legacy key decoding** (https://invisible-island.net/xterm/ctlseqs/ctlseqs.html):
arrows `CSI A–D` / `SS3 A–D` (DECCKM application mode both accepted); Home/End
`CSI H/F` + `CSI 1~/4~`; Insert/Delete/PgUp/PgDn `CSI 2~/3~/5~/6~`; F1–F4 `SS3 P–S`,
F5–F12 `CSI 15~…24~`; the xterm modifier encoding `CSI 1;m X` / `CSI k;m ~` with
`m = 1 + bitmask(shift=1, alt=2, ctrl=4, meta=8)`; Alt as ESC prefix; Ctrl+letter as
`byte & 0x1F`. The known-unfixable legacy collisions (Ctrl+I=Tab, Ctrl+M=Enter,
Ctrl+[=ESC, no key release) are documented on `KeyEvent` and are precisely what kitty
mode removes.

**Kitty keyboard protocol** (https://sw.kovidgoyal.net/kitty/keyboard-protocol/):
detection is `CSI ? u` fenced by DA1 in the probe (§2.6). When supported, `Start` pushes
**flags 1+2** (`CSI > 3 u` — disambiguate escape codes + report event types) and `Stop`
pops (`CSI < u`). Decoding handles `CSI unicode-key-code:alternates ; modifiers:event u`,
producing `KeyEvent{Kind: KeyPress|KeyRepeat|KeyRelease}`; legacy terminals only ever
produce `KeyPress`. Flags 4/8/16 are not pushed in v1 (associated-text and all-keys modes
are additive later).

**ESC disambiguation.** Only when kitty mode is **not** active: a lone ESC is held for
`WithEscTimeout` (default 35 ms, the vim `ttimeoutlen` 10–50 ms band) and delivered as an
ESC key if no continuation byte arrives. On kitty terminals the timeout path is
**disabled entirely** — flag 1's purpose. The slow-SSH misfire risk (split packets →
spurious ESC) is documented on the option; raising the timeout is the user remedy.

### 2.6 The startup capability probe

One batched write, one fence, one deadline — no terminfo (ADR-0001 §2.4 #4).

**Pre-seed (no I/O):** `$COLORTERM ∈ {truecolor, 24bit}` seeds
`ColorProfile = ProfileTrueColor` (https://github.com/termstandard/colors); a `$TERM`
containing `256color` seeds `ProfileANSI256`; `$TERM_PROGRAM`/`$WT_SESSION` prefix
heuristics may also pre-seed the profile. Pre-seeds are only ever **upgraded** by probe
replies, never downgraded by probe silence.

**Probe batch** — all queries in a single `Write`, in this order, then read until fence:

| # | Query | Sequence | Sets |
|---|-------|----------|------|
| 1 | DECRQM 2004 | `CSI ? 2004 $ p` | `BracketedPaste` |
| 2 | DECRQM 2026 | `CSI ? 2026 $ p` | `SyncOutput` |
| 3 | DECRQM 2027 | `CSI ? 2027 $ p` | `UnicodeCore` |
| 4 | DECRQM 2048 | `CSI ? 2048 $ p` | `InBandResize` |
| 5 | DECRQM 1006 | `CSI ? 1006 $ p` | `Mouse` (tri-state; rev 1) |
| 6 | XTGETTCAP | `DCS + q 524742 ; 536D756C78 ST` (hex "RGB;Smulx") | `ColorProfile`, `Undercurl` |
| 7 | OSC 10 | `OSC 10 ; ? ST` | `DefaultFG` |
| 8 | OSC 11 | `OSC 11 ; ? ST` | `DefaultBG`, `DarkBackground` |
| 9 | Kitty query | `CSI ? u` | `KittyKeyboard` |
| 10 | **DA1 fence** | `CSI c` | terminates the probe |

DECRPM replies `CSI ? Pd ; Ps $ y` count as supported when `Ps ∈ {1, 2, 3}` (set / reset
/ permanently set — the mode exists), unsupported on `0`
(https://gist.github.com/christianparpart/d8a62cc1ab659194337d73e399004036). For the
`Mouse` row the mapping is tri-state (rev 1): an answer with `Ps ∈ {1, 2, 3}` →
`TriYes`, `0` → `TriNo`, silence at the fence → `TriUnknown` — a
requested-but-unverified enable is never reported as support. Every real
terminal answers DA1, and answers arrive in request order — so **when the DA1 reply
arrives, all still-unanswered probes are marked unsupported and Start proceeds
immediately**. The `WithProbeTimeout` deadline (default 250 ms — rev 1, raised from
100 ms: the fence means healthy terminals never pay it, while 100 ms falsely downgraded
ordinary high-latency SSH sessions) is only the backstop for terminals/multiplexers that
never answer DA1; expiry marks everything unanswered as unsupported. This is the vaxis/Neovim probing model (go.rockorager.dev/vaxis). The
reader goroutine is already running during the probe: probe replies are consumed by the
decoder, while any *user* input typed early is queued and delivered on `Events()` after
`Start` returns — nothing is lost.

**Cancellation.** If `ctx` is cancelled while the probe is in flight, `Start` aborts the
probe immediately, **discards all partial capability replies** (a partially-negotiated
profile is never observable), unwinds the already-acquired state through the §2.10 undo
chain (raw mode, alt screen, VT modes restored), and returns `ctx.Err()`.

> **Rev 1 (Lector should-fix + nit).** Default probe timeout raised 100 ms → 250 ms
> (Lector's Q2 answer: the DA1 fence spares healthy terminals; 100 ms falsely downgraded
> high-latency SSH), the DECRQM ?1006 row added for verifiable tri-state `Mouse`
> (must-fix #1), and the `Start(ctx)` cancellation-during-probe semantics above made
> explicit.

**Mode enablement after the fence** (still inside `Start`, one write): `CSI ?2004 h` if
`BracketedPaste`; `CSI ?1002 h` `CSI ?1006 h` unless `WithoutMouse` or `Mouse == TriNo`
— the enable may be attempted on `TriUnknown`, but `Capabilities().Mouse` keeps
reporting `TriUnknown` in that case (SGR encoding is mandatory; `?1005` UTF-8 and X10
encodings are never used); `CSI > 3 u` if `KittyKeyboard`; `CSI ?2048 h` if
`InBandResize`. Mode 2026 needs no enablement — it is emitted per-frame as brackets by
`Flush` when `SyncOutput` (ADR-0003 §2.5).

### 2.7 Mouse, paste, and alternate screen

- **Mouse:** `?1002h` (button-event tracking incl. drag) + `?1006h` (SGR encoding:
  `CSI < b ; x ; y M/m`, decimal, unbounded coordinates, distinct release). Button bits
  decoded per xterm: 0–2 button, +4 shift, +8 meta, +16 ctrl, +32 motion, +64 wheel
  (https://invisible-island.net/xterm/ctlseqs/ctlseqs.html). Support is verified by the
  DECRQM ?1006 probe row (§2.6, rev 1): `Capabilities().Mouse` is `TriYes`/`TriNo` on an
  answer and `TriUnknown` when the enable was merely attempted. `?1003h` (any-motion) is
  not enabled in v1 — hover tracking floods the channel for no v1 widget need.
- **Bracketed paste:** `?2004h`; content framed `ESC[200~ … ESC[201~` becomes one
  `PasteEvent{Text}` with CR and CRLF normalized to `\n`. A `200~` opener inside an
  unterminated paste is treated as literal text; an unterminated paste is flushed as a
  paste on Stop rather than dropped.
- **Alternate screen:** `?1049h` on Start / `?1049l` on Stop (save-cursor + switch +
  clear). Supported by conhost. `WithoutAltScreen` yields inline mode for
  REPL-embedded UIs; the runtime then treats the region below the cursor as the canvas
  (sizing details owned by ADR-0003/0005).

### 2.8 Resize handling

Division of labor (rev 1): the backend's job is **fresh truth, in order** — on every OS
notification it **re-queries** the size (`term.GetSize` / console API) rather than
trusting the stale notification payload, and emits an ordered `ResizeEvent` on
`Events()`. The backend performs **no coalescing**: latest-wins collapsing of resize
storms (SIGWINCH bursts during drags —
https://github.com/manaflow-ai/cmux/issues/3831) is owned entirely by the App's intake
stage (ADR-0005), which guarantees that intermediate sizes are never laid out and the
**final size always renders**. Post-resize rendering is a full clear + repaint, never a
diff across sizes (ADR-0003 §2.6).

> **Rev 1 (Lector must-fix #2).** Coalescing moved out of the backend: rev 0 had the
> backend collapsing notifications into a latest-size cell, which contradicted
> ADR-0005's intake-owned queue policy. The backend now only re-queries and emits.

- **Unix:** `os/signal.Notify(ch, syscall.SIGWINCH)` in a file guarded by
  `//go:build !windows` — SIGWINCH does not exist on GOOS=windows
  (https://pkg.go.dev/syscall?GOOS=windows). Because a KVM switch or detached
  multiplexer can eat SIGWINCH, the size is also re-checked on focus-in events and on
  first input after a quiet period.
- **Windows:** no SIGWINCH. A ~250 ms ticker polls `GetConsoleScreenBufferInfo` and
  diffs the reported size — deliberately choosing the poll (dossier §7 option b) over
  `ReadConsoleInput` window events, because mixing `ReadFile` stream reads with
  `ReadConsoleInput` on one handle is broken
  (https://github.com/microsoft/terminal/issues/394) and abandoning the byte stream
  would fork the input path (§4.3).
- **Mode 2048 upgrade (both platforms):** when the probe reports `InBandResize`, the
  terminal's `CSI 48 ; rows ; cols ; hpx ; wpx t` in-band reports become the source of
  truth (https://gist.github.com/rockorager/e695fb2924d36b2bcf1fff4a3704bd83); the
  Windows poll ticker is stopped, and unix SIGWINCH handling remains armed but merely
  triggers the same re-query + emission (harmless duplication — the intake stage's
  latest-wins policy absorbs it, ADR-0005). Windows Terminal does
  not support 2048 yet (https://github.com/microsoft/terminal/issues/19618), so the poll
  ticker remains the working Windows path for now.

### 2.9 Event channel ownership and the reader goroutine

Contract (the `server/ws/ws.go` one-reader/`sync.Once`/`done chan struct{}` discipline):

- **Exactly one goroutine** — started by `Start`, owned by the backend — reads the input
  fd, feeds the parser, and sends decoded events on the `Events()` channel. No other
  code reads the fd, ever.
- The channel is buffered (64 — a small, documented decoupling buffer, not a policy
  queue). Ordering is the wire order across ALL event kinds: key, mouse, paste, and
  resize events are never reordered — and never coalesced or dropped — by the backend.
  Resize notifications are materialized as `ResizeEvent`s through the same producer path
  (§2.8), so the channel has a single logical producer.
- If the consumer stalls, the reader **blocks**; events are never dropped. This is
  acceptable because the consumer is the App's dedicated intake stage (ADR-0005), which
  only dequeues and applies coalescing/overflow policy — it pulls promptly by
  construction even when component handlers are slow.
- **Shutdown sequence** in `Stop` (under the `sync.Once`): close the internal `done`
  channel → unblock the fd read (unix: `SetReadDeadline(time.Now())` on the `*os.File`,
  valid for pollable ttys; Windows: the reader waits on the console handle with a short
  bounded wait and re-checks `done`) → wait for the reader via `sync.WaitGroup` → record
  the reader's terminal error (if any) for `Err()` → close `Events()`. **Only the
  reader's owner closes the channel, and only after the reader has exited** — consumers
  may safely `range` until close, then consult `Err()`.

> **Rev 1 (Lector must-fix #2).** The backend-side "coalesced through the latest-size
> cell" language is gone: the channel is ordered and un-coalesced end to end, and
> `Err()` (§2.1) carries the reader/probe terminal error the App loop reads after close.

> **Rev 2 (pollability guarantee — db-tui finding #1, fix/term-reader-unblock).**
> `SetReadDeadline` is only honored on poller-registered fds, and a tty inherited on
> stdin arrives in *blocking* mode — the deadline was a silent no-op
> (`os.ErrNoDeadline`) and Stop hung on the reader join for every real interactive
> session. `start` therefore guarantees pollability via `makePollable` (unix), run
> after `makeRaw`: a deadline-capable file passes through; the controlling terminal
> is read through a private non-blocking `/dev/tty` description; otherwise the fd is
> duplicated atomically (`F_DUPFD_CLOEXEC`), flipped `O_NONBLOCK`, and re-wrapped —
> teardown restores the exact `F_GETFL` word and closes only the duplicate, after the
> reader joins. Nothing may call `Fd()` on the swapped file (it reverts the fd to
> blocking mode). Regression: `pty_linux_test.go` drives a real pty with the slave as
> a raw blocking fd — the shell-inherited stdin shape the pipe-based lifecycle tests
> cannot reproduce.

### 2.10 Teardown and restore guarantees

`Stop` restores in reverse order of acquisition, best-effort on every step,
`errors.Join`-ing failures (the `server/scaffold.go` teardown pattern):

1. Kitty pop `CSI < u` (if pushed);
2. mode disables: `?2048l`, `?1006l`, `?1002l`, `?2004l`;
3. cursor: `CSI 0 SP q` (default shape), `CSI ?25 h` (show), SGR reset `CSI m`;
4. leave alternate screen `CSI ?1049 l` (if entered);
5. — steps 1–4 emitted as one final write —
6. `term.Restore` of the saved termios state (unix) / `SetConsoleMode` restore of both
   saved handle modes (Windows);
7. stop reader, record its terminal error for `Err()`, close `Events()` (§2.9).

The whole sequence runs under `sync.Once`, so concurrent/duplicate `Stop` calls and the
error path racing the normal path are safe. The runtime guarantees invocation on every
exit: `App.Run` acquires via `Start` and `defer`s `Stop` so error returns **and panics**
restore the terminal before the panic propagates (mirrors golib server ADR-0006 / the
`server/scaffold.go` lifecycle: synchronous acquire, deferred teardown, `errors.Join`).
A raw-mode terminal left unrestored is the worst failure mode a TUI library can ship;
this is a hard acceptance criterion (§5.6).

### 2.11 Dependency decision

`golang.org/x/term` + `golang.org/x/sys` are imported by `tui/term` **only** (ADR-0001
§2.2, G9). Rationale re-stated at this layer's level of detail: std `syscall` on Windows
has `GetConsoleMode` but **not** `SetConsoleMode`
(https://pkg.go.dev/syscall?GOOS=windows), so a "zero-dep" Windows driver would hand-roll
`NewLazySystemDLL("kernel32.dll")` bindings — zero-dep theater against a package that is
effectively a stdlib extension and already in the module graph. On unix, x/term's
`MakeRaw`/`Restore`/`GetSize` replace ~200 lines of per-GOOS ioctl code with the
canonical, maintained implementation. `tui`, `tui/style`, `tui/widget`,
`tui/internal/grapheme` remain stdlib-only; `TestBackend` lives in `tui` precisely so
tests of core and widgets never touch `tui/term`.

## 3. Consequences

**Positive**

- One parser, every platform: the VT-input decision (§2.4) means Windows is not a second
  input implementation but a mode flag plus a resize poller — the highest-leverage
  simplification available (dossier §7 verdict).
- Capability truth instead of capability folklore: per-capability probing survives tmux
  re-emission, `$TERM` lies, and new terminals, and new capabilities slot in as one probe
  line + one struct field (ADR-0001 evolution note).
- The latched-cursor + single-channel + one-write contracts make the backend's externally
  observable behavior fully assertable through `TestBackend` (flush counts, cursor state,
  grid snapshots) — no PTY in CI.
- Kitty flags 1+2 eliminate the ESC timeout and key-collision ambiguity on kitty, foot,
  ghostty, alacritty, WezTerm, iTerm2, and Windows Terminal — the modern majority —
  while the legacy path still works everywhere else.

**Negative (costs)**

- We own a VT parser, a key decoder across two protocols, and a capability prober
  forever — spec-driven and bounded, but real code (the Duffield "low-level view-layer
  stuff" burden acknowledged in ADR-0001 §3). Mitigation: the parser is a pure function
  of bytes; the test corpus is the spec.
- Startup pays the probe fence: typically <10 ms locally, up to the 250 ms budget over
  slow links or on silent terminals. Accepted as the price of truth; tunable via
  `WithProbeTimeout`.
- The Windows resize poll is a 250 ms latency floor and a (tiny) idle wakeup source on
  the one platform+terminal combination (WT today) without mode 2048; it disappears as
  WT ships 2048.
- Blocking-on-stall channel semantics tie input liveness to intake liveness; a wedged
  intake stage (ADR-0005) stops input consumption. Accepted: intake only dequeues and is
  fast by construction, a wedged intake is the bug to fix, and backend-side dropping
  would only mask it.

**Evolution**

- Additive capability fields (OSC 52 clipboard, kitty flags 4/8/16, `?1016` pixel mouse,
  XTWINOPS pixel size for future graphics) extend `Capabilities` and the probe batch
  without interface changes.
- An SSH backend is `Open` over arbitrary reader/writer + resize plumbing; a web backend
  is the byte stream over a socket to xterm.js — both fit the existing seam (ADR-0001
  §4.6).

## 4. Alternatives considered

1. **terminfo (the tcell model: embedded compiled DB + `infocmp` fallback).** Rejected —
   fixed by ADR-0001 §2.4 #4; at this ADR's level: terminfo answers "what does `$TERM`
   claim" not "what does the attached terminal do", is stale for modern modes
   (2026/2027/2048, kitty), and costs a binary database plus a parser for its compiled
   format. Live probing is what vaxis and Neovim shipped and it demonstrably works.
2. **crossterm/tcell-style `ReadConsoleInput` on Windows.** Reading typed
   `INPUT_RECORD`s gives window-resize events for free but forks the entire input path
   (two parsers, two key decoders) and cannot be mixed with stream reads on the same
   handle (https://github.com/microsoft/terminal/issues/394). Rejected: VT input mode +
   one parser + a resize poll is strictly less code and converges with the 2048 future.
3. **Immediate cursor operations (Ratatui's shape).** Rejected: every cursor call would
   be its own write, violating the one-write flicker rule (ADR-0003 §2.9 R3); latching
   costs four small fields.
4. **xterm `modifyOtherKeys` as a middle keyboard tier.** Real xterm supports it and not
   the kitty protocol. Deferred (N3): it adds a third encoding (`CSI 27;mod;code ~` /
   `CSI code;mod u`) and push/pop-less state management for one terminal family that
   still works acceptably on the legacy path. Additive later behind a capability field.
5. **Per-event-type channels (keys, mouse, resize separately).** Rejected: relative
   ordering between event kinds is semantically meaningful (paste vs. keys, press vs.
   drag), and a single channel is the simplest thing that preserves it; the runtime
   demultiplexes by type switch (ADR-0005).
6. **Probing lazily / per-feature on first use.** Rejected: mid-session probes interleave
   replies with user input during interactive use, need per-feature fencing, and make
   `Capabilities()` time-varying, which poisons ADR-0003/0006 assumptions. One fence at
   Start is bounded and simple.
7. **Hand-rolled kernel32 `LazyDLL` bindings to keep x/sys out.** Rejected — §2.11.

## 5. Acceptance criteria

1. `tui` (containing `Backend`, `Capabilities`, `TestBackend`) imports stdlib only;
   `x/term`/`x/sys` appear in `tui/term` imports exclusively; `go vet ./tui/...` and
   builds pass for `GOOS=linux`, `GOOS=darwin`, and `GOOS=windows`.
2. The DEC parser passes a table-driven corpus covering: every ctlseqs sequence in §2.5,
   kitty sequences with sub-parameters, OSC/DCS with both ST and BEL terminators,
   CAN/SUB aborts, ESC-from-anywhere restarts — and a fuzz/property test proving byte
   stream splits at arbitrary boundaries never change the decoded event sequence.
3. The capability prober, unit-tested against scripted reply fixtures on in-memory
   pipes: (a) full-modern replies set every flag, incl. `Mouse == TriYes` from the
   DECRQM ?1006 answer; (b) DA1-only replies return before the deadline with
   `Mouse == TriUnknown`, the mode flags false, and `DarkBackground == true` (the
   documented assume-dark fallback); (c) total silence returns at the 250 ms deadline
   with the same defaults; (d) `$COLORTERM=truecolor` pre-seed survives probe silence as
   `ProfileTrueColor`; (e) replies after the fence are discarded harmlessly; (f) ctx
   cancellation mid-probe discards partial replies, restores acquired terminal state,
   and returns `ctx.Err()`.
4. Kitty push (`CSI > 3 u`) is emitted iff the probe reported support, and pop
   (`CSI < u`) is emitted on every Stop path that pushed, verified by capturing the
   output stream.
5. `Flush` with an empty diff and unchanged cursor writes zero bytes; any non-empty
   frame produces exactly one `Write` on the output — asserted with a counting writer.
6. Restore-on-panic: a test app that panics inside a render callback leaves the
   (scripted) terminal with: kitty popped, modes disabled, cursor shown/reset, alt
   screen exited, termios restored — and the panic still propagates. Double-`Stop` is a
   no-op.
7. `Events()` is closed after `Stop` returns and never receives a send afterwards
   (verified under `-race`); `Err()` returns nil after a clean Stop and returns the
   scripted reader error after an abnormal reader exit (TestBackend `SetErr` plus a
   term fixture whose input pipe fails mid-read).
8. `TestBackend`: an interaction script (keys + mouse + resize) drives a demo tree and
   asserts cell-exact `String()` frames and `Flushes()` counts, deterministically, in CI
   with no PTY.
9. Manual platform matrix documented in `tui/term/README.md`: Linux (kitty/foot/tmux),
   macOS Terminal, Windows Terminal, and legacy conhost — recording which capabilities
   each negotiates and that degradation is graceful.

## 6. Questions for the reviewer

- **Q1.** `Flush(diff []CellUpdate)` takes a slice; golib convention prefers `iter.Seq`
  for iteration (`iter.Seq[CellUpdate]` would let ADR-0003's differ stream without
  materializing). The slice was chosen because the differ already owns a reusable scratch
  slice and the seam stays trivially mockable — but is the iterator form the better
  golib-native contract to freeze now, given the seam is hard to change later?
  — **Lector r1:** keep `Flush([]CellUpdate)` — `Flush` is a hot internal driver seam
  fed by a reusable diff slice; the slice is the better concrete contract.
- **Q2.** The probe deadline default is 100 ms with a 50 ms floor. Over high-latency SSH
  (~200 ms RTT) even DA1 may miss the deadline, silently degrading a fully modern remote
  terminal to the legacy profile. Options: (a) accept, document `WithProbeTimeout`;
  (b) default higher (250 ms) and eat slower startup everywhere; (c) keep 100 ms but
  allow late-arriving probe replies to *upgrade* capabilities after Start (adds
  time-varying `Capabilities()`, which §4.6 rejected). We chose (a) — confirm, or pick a
  different trade?
  — **Lector r1:** option (b) — raise the default to 250 ms with the same option/floor;
  the DA1 fence avoids paying it on good terminals and 100 ms falsely downgrades
  ordinary high-latency SSH. Folded in rev 1 (§2.4, §2.6).
- **Q3.** On non-kitty terminals we hold a lone ESC for 35 ms (§2.5). An alternative is
  delivering ESC immediately and emitting a synthetic "revoke" when it turns out to be a
  sequence prefix — no framework does this because revocation leaks into app semantics.
  Is the timeout (with its slow-SSH spurious-ESC caveat) acceptable as the only legacy
  mechanism, or should the runtime expose the pending-ESC state so widgets can defer
  ESC-triggered actions themselves?
  — **Lector r1:** accepted — the timeout stays the only legacy mechanism; pending-ESC
  state is not exposed (it leaks parser mechanics into app semantics).
- **Q4.** `Mouse` is probe-independent (there is no reliable mouse-support query); we
  enable `?1002/?1006` on everything except detected-legacy-conhost and report
  `Mouse=true` optimistically. A terminal that ignores the enable simply never sends
  mouse sequences — harmless, but `Capabilities().Mouse` then over-promises. Is an
  optimistic flag acceptable, or should `Mouse` be tri-state (yes/no/unknown) at the
  cost of a non-uniform capability model?
  — **Lector r1:** never report an optimistic request as boolean support; tri-state
  adopted. Folded in rev 1 (§2.2 `Tri`, §2.6 DECRQM ?1006 probe row, §2.7).
- **Q5.** Windows floor 1809+ with hard `ErrConsoleTooOld` (§2.4, N4): golib server-side
  tools may still meet Server 2016 (1607: VT output yes, ConPTY no). Should the floor be
  softened to "1607+ output, degraded input" (extra conditional paths in the one place
  we've kept single-pathed), or is the 1809 hard floor right for a 2026 library?
  — **Lector r1:** keep the 1809+ hard floor for v1; a degraded 1607 path is too much
  platform branching for the first implementation.
