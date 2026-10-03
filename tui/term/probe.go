package term

import (
	"context"
	"strings"
	"time"

	"github.com/yongjohnlee80/golib/tui"
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
	caps := tui.Capabilities{
		ColorProfile: preseedProfile(b.cfg.env),
		// The documented, fixed unknown-fallback: when OSC 11 goes
		// unanswered, ASSUME DARK.
		DarkBackground: true,
	}

	b.probing.Store(true)
	defer b.probing.Store(false)

	tmux := inTmux(b.cfg.env)
	if err := b.write([]byte(probeQueries(tmux))); err != nil {
		return caps, err
	}

	timer := time.NewTimer(b.cfg.probeTimeout)
	defer timer.Stop()

	modes := make(map[int]int)
	var fg, bg tui.ProbedColor
	var kitty, rgb, smulx bool
	graphics := tui.TriUnknown

collect:
	for {
		select {
		case r := <-b.probeCh:
			switch r.kind {
			case prDECRPM:
				modes[r.mode] = r.value
			case prKitty:
				kitty = true
			case prOSCColor:
				if r.osc == 10 {
					fg = r.color
				} else {
					bg = r.color
				}
			case prTermcap:
				rgb = rgb || r.rgb
				smulx = smulx || r.smulx
			case prGraphics:
				graphics = tui.TriNo
				if r.graphicsOK {
					graphics = tui.TriYes
				}
			case prDA1:
				if graphics == tui.TriUnknown && !tmux {
					// the terminal answered in order and said nothing of graphics; behind tmux the
					// fence is tmux's own answer, which proves nothing of the terminal outside
					graphics = tui.TriNo
				}
				break collect // the fence: everything unanswered is unsupported
			}
		case <-timer.C:
			break collect // silent terminal: same defaults, at the deadline
		case <-ctx.Done():
			return caps, ctx.Err()
		}
	}

	// DECRPM Ps ∈ {1, 2, 3} (set / reset / permanently set) means the mode
	// exists; 0 means unrecognized; silence means unsupported.
	supported := func(mode int) bool {
		v, ok := modes[mode]
		return ok && v >= 1 && v <= 3
	}
	caps.BracketedPaste = supported(2004)
	caps.SyncOutput = supported(2026)
	caps.UnicodeCore = supported(2027)
	caps.InBandResize = supported(2048)

	// Mouse is tri-state: TriYes only on a verifiable DECRQM ?1006
	// answer; silence stays TriUnknown — a requested-but-unverified enable
	// is never reported as support.
	switch v, ok := modes[1006]; {
	case ok && v >= 1 && v <= 3:
		caps.Mouse = tui.TriYes
	case ok:
		caps.Mouse = tui.TriNo
	default:
		caps.Mouse = tui.TriUnknown
	}

	caps.KittyKeyboard = kitty
	caps.KittyGraphics = graphics
	caps.Undercurl = smulx
	if rgb {
		caps.ColorProfile = tui.ProfileTrueColor // upgrade only, never downgrade
	}
	caps.DefaultFG = fg
	caps.DefaultBG = bg
	if bg.Known {
		caps.DarkBackground = relativeLuminance(bg) < 0.5
	}
	return caps, nil
}

// relativeLuminance approximates the relative luminance of c in [0, 1]
// (Rec. 709 coefficients) for the DarkBackground derivation.
func relativeLuminance(c tui.ProbedColor) float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}
