package term

import (
	"context"
	"strings"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// The startup capability probe: one batched write, one fence,
// one deadline — no terminfo. All queries go out in a
// single Write, then replies are read until the DA1 fence answers. Every real
// terminal answers DA1, and answers arrive in request order, so when the DA1
// reply arrives all still-unanswered probes are marked unsupported and Start
// proceeds immediately; the timeout is only the backstop for terminals and
// multiplexers that never answer DA1.
//
// Batch order: DECRQM 2004, 2026, 2027, 2048, 1006; XTGETTCAP "RGB;Smulx"
// (hex 524742;536D756C78); OSC 10; OSC 11; kitty query CSI ? u; the kitty
// graphics query (graphicsQuery); DA1 fence.
const probeBatch = "\x1b[?2004$p" +
	"\x1b[?2026$p" +
	"\x1b[?2027$p" +
	"\x1b[?2048$p" +
	"\x1b[?1006$p" +
	"\x1bP+q524742;536D756C78\x1b\\" +
	"\x1b]10;?\x1b\\" +
	"\x1b]11;?\x1b\\" +
	"\x1b[?u"

// graphicsQuery asks whether kitty's graphics protocol is spoken: a 1×1 RGB image, queried (a=q)
// and never stored or shown. Inside tmux it is passed through to the terminal outside.
const graphicsQuery = "\x1b_Gi=" + graphicsProbeID + ",s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"

// da1Fence is the probe's last query: every terminal answers DA1, in order.
const da1Fence = "\x1b[c"

// probeQueries is the whole batch, the graphics query passed through tmux when inside it.
func probeQueries(tmux bool) string {
	return probeBatch + passthrough(graphicsQuery, tmux) + da1Fence
}

// passthrough wraps an escape sequence so tmux hands it to the terminal outside it (DCS tmux; …
// ST, every ESC inside doubled); with tmux false it is the sequence. tmux forwards it only when its
// allow-passthrough option is on.
func passthrough(seq string, tmux bool) string {
	if !tmux {
		return seq
	}
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// inTmux reports whether the program runs inside tmux.
func inTmux(lookup func(string) (string, bool)) bool {
	v, ok := lookup("TMUX")
	return ok && v != ""
}

// preseedProfile derives the ColorProfile pre-seed from the environment
// (no I/O). Pre-seeds are only ever upgraded by probe replies,
// never downgraded by probe silence.
func preseedProfile(lookup func(string) (string, bool)) tui.ColorProfile {
	profile := tui.ProfileANSI16
	if v, ok := lookup("TERM"); ok && strings.Contains(v, "256color") {
		profile = tui.ProfileANSI256
	}
	// https://github.com/termstandard/colors
	if v, ok := lookup("COLORTERM"); ok {
		switch strings.ToLower(v) {
		case "truecolor", "24bit":
			return tui.ProfileTrueColor
		}
	}
	// Heuristic pre-seeds, never authority: these
	// terminals are all truecolor-capable.
	if _, ok := lookup("WT_SESSION"); ok {
		return tui.ProfileTrueColor
	}
	if v, ok := lookup("TERM_PROGRAM"); ok {
		switch v {
		case "iTerm.app", "WezTerm", "ghostty", "kitty":
			return tui.ProfileTrueColor
		}
	}
	return profile
}

// runProbe executes the startup probe and resolves Capabilities. On ctx
// cancellation it returns ctx.Err() and the caller discards everything —
// a partially-negotiated profile is never observable.
func (b *Backend) runProbe(ctx context.Context) (tui.Capabilities, error) {
	preseed := preseedProfile(b.cfg.env)
	b.probing.Store(true)
	defer b.probing.Store(false)
	tmux := inTmux(b.cfg.env)
	if err := b.write([]byte(probeQueries(tmux))); err != nil {
		return probeResult{}.capabilities(preseed), err
	}
	r, err := b.collectProbe(ctx, tmux)
	if err != nil {
		return probeResult{}.capabilities(preseed), err
	}
	return r.capabilities(preseed), nil
}

// probeResult is what the probe's replies said, before any of it is read as a capability.
type probeResult struct {
	modes             map[int]int // DECRPM: mode to its Ps
	fg, bg            tui.ProbedColor
	kitty, rgb, smulx bool
	graphics          tui.Tri
}

// collectProbe gathers the replies until the DA1 fence, the probe timeout, or ctx's end (an error).
// Behind tmux (tmux) the fence is tmux's own answer, which proves nothing of the terminal outside.
func (b *Backend) collectProbe(ctx context.Context, tmux bool) (probeResult, error) {
	r := probeResult{modes: make(map[int]int), graphics: tui.TriUnknown}
	timer := time.NewTimer(b.cfg.probeTimeout)
	defer timer.Stop()
	for {
		select {
		case rep := <-b.probeCh:
			switch rep.kind {
			case prDECRPM:
				r.modes[rep.mode] = rep.value
			case prKitty:
				r.kitty = true
			case prOSCColor:
				if rep.osc == 10 {
					r.fg = rep.color
				} else {
					r.bg = rep.color
				}
			case prTermcap:
				r.rgb = r.rgb || rep.rgb
				r.smulx = r.smulx || rep.smulx
			case prGraphics:
				r.graphics = tui.TriNo
				if rep.graphicsOK {
					r.graphics = tui.TriYes
				}
			case prDA1:
				if r.graphics == tui.TriUnknown && !tmux {
					// the terminal answered in order and said nothing of graphics
					r.graphics = tui.TriNo
				}
				return r, nil // the fence: everything unanswered is unsupported
			}
		case <-timer.C:
			return r, nil // silent terminal: same defaults, at the deadline
		case <-ctx.Done():
			return r, ctx.Err()
		}
	}
}

// capabilities reads the replies as capabilities, on top of preseed's colour profile. It is pure:
// the zero probeResult is a terminal that answered nothing, and gets the documented defaults
// (DarkBackground assumed, Mouse unknown, graphics unknown).
func (r probeResult) capabilities(preseed tui.ColorProfile) tui.Capabilities {
	caps := tui.Capabilities{
		ColorProfile: preseed,
		// The documented, fixed unknown-fallback: when OSC 11 goes
		// unanswered, ASSUME DARK.
		DarkBackground: true,
	}
	// DECRPM Ps ∈ {1, 2, 3} (set / reset / permanently set) means the mode
	// exists; 0 means unrecognized; silence means unsupported.
	supported := func(mode int) bool {
		v, ok := r.modes[mode]
		return ok && v >= 1 && v <= 3
	}
	caps.BracketedPaste = supported(2004)
	caps.SyncOutput = supported(2026)
	caps.UnicodeCore = supported(2027)
	caps.InBandResize = supported(2048)

	// Mouse is tri-state: TriYes only on a verifiable DECRQM ?1006
	// answer; silence stays TriUnknown — a requested-but-unverified enable
	// is never reported as support.
	switch v, ok := r.modes[1006]; {
	case ok && v >= 1 && v <= 3:
		caps.Mouse = tui.TriYes
	case ok:
		caps.Mouse = tui.TriNo
	default:
		caps.Mouse = tui.TriUnknown
	}

	caps.KittyKeyboard = r.kitty
	caps.KittyGraphics = r.graphics
	caps.Undercurl = r.smulx
	if r.rgb {
		caps.ColorProfile = tui.ProfileTrueColor // upgrade only, never downgrade
	}
	caps.DefaultFG = r.fg
	caps.DefaultBG = r.bg
	if r.bg.Known {
		caps.DarkBackground = style.Dark(r.bg.R, r.bg.G, r.bg.B) // WCAG: white reads better than black
	}
	return caps
}
