// Package term is golib/tui's concrete ANSI terminal driver: the
// real-terminal implementation of the [tui.Backend] seam on unix and Windows.
//
// [Open] validates the TTY and builds an inert [Backend]; Start acquires the
// device — raw mode ([golang.org/x/term.MakeRaw]; on Windows additionally the
// stdout VT modes), the alternate screen, the single input-reader goroutine,
// the DA1-fenced capability probe, and negotiated mode enablement (bracketed
// paste, SGR mouse, kitty keyboard flags 1+2, mode-2048 in-band resize).
// Stop restores everything in reverse order under a sync.Once, so error and
// panic paths (App.Run defers Stop) always leave the terminal usable.
//
// # Capability truth, not capability folklore
//
// Capabilities are resolved by live probing at Start — never terminfo:
// one batched write of DECRQM 2004/2026/2027/2048/1006,
// XTGETTCAP RGB+Smulx, OSC 10/11, and the kitty query, fenced by DA1. Every
// real terminal answers DA1, so healthy terminals never pay the probe
// timeout (default 250ms, WithProbeTimeout). Degradation is per-capability,
// not per-$TERM.
//
// # Input
//
// One spec-driven DEC ANSI parser (vt100.net) decodes the input byte stream
// on every platform — on Windows, ENABLE_VIRTUAL_TERMINAL_INPUT makes the
// console encode keys as VT sequences into stdin, so there is no second
// input path. Legacy CSI/SS3 keys with xterm modifier encoding, kitty CSI u
// keys (press/repeat/release), SGR mouse, bracketed paste (CR/CRLF
// normalized to \n), focus in/out, and mode-2048 resize reports all map onto
// the core tui event set. A lone ESC is disambiguated by a short hold
// (WithEscTimeout, default 35ms) only when kitty mode is inactive.
//
// # Output
//
// Flush emits one frame as one Write: cursor hidden during
// paint, overwrite-in-place (never clear-then-redraw), CUP only on
// discontinuity, SGR only on attribute change, and mode-2026 synchronized
// -update brackets when the terminal supports them. Cursor operations are
// latched, not immediate, so they land in the same write as the cell diff.
//
// This is the ONLY golib package that imports golang.org/x/term and
// golang.org/x/sys; the tui core, style, and widget packages
// are stdlib-only, and tests of those packages use tui.TestBackend instead
// of this driver.
package term
