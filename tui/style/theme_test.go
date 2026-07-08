package style

import "testing"

// TestThemeDerivationDefaults covers ADR-0006 §5 acceptance criterion 7:
// NewTheme(ANSI(4)) yields the documented defaults for all 20 slots (11
// base + 9 derived, incl. TokenBorder/TokenBorderFocused/
// TokenTextOnSecondary), and every default token resolves to kindANSI or
// kindDefault (G7 — no truecolor in the default theme). Token resolution
// returns a Color only — the API cannot attach SGR attributes on a token's
// behalf (TokenTextMuted is color-only, §2.5 normative rule).
func TestThemeDerivationDefaults(t *testing.T) {
	th := NewTheme(ANSI(4))
	want := map[Token]Color{
		TokenPrimary:         ANSI(4),
		TokenSecondary:       ANSI(4),   // ← Primary
		TokenAccent:          ANSI(4),   // ← Primary
		TokenForeground:      Default(), // terminal's own fg
		TokenBackground:      Default(), // terminal's own bg
		TokenSurface:         Default(), // ← Background
		TokenPanel:           Default(), // ← Surface
		TokenBoost:           Default(), // ← Surface
		TokenWarning:         ANSI(3),
		TokenError:           ANSI(1),
		TokenSuccess:         ANSI(2),
		TokenTextMuted:       Default(), // ← Foreground (color ONLY)
		TokenTextOnPrimary:   Default(), // ← Background
		TokenTextOnSecondary: Default(), // ← Background
		TokenTextOnAccent:    Default(), // ← Background
		TokenTextOnError:     Default(), // ← Background
		TokenTextOnSuccess:   Default(), // ← Background
		TokenTextOnWarning:   Default(), // ← Background
		TokenBorder:          Default(), // ← Foreground
		TokenBorderFocused:   ANSI(4),   // ← Accent
	}
	if len(want) != int(numTokens) {
		t.Fatalf("expectation table has %d slots, want %d — update alongside the token set", len(want), numTokens)
	}
	for tok, w := range want {
		got := th.Color(tok)
		if got != w {
			t.Errorf("token %d: Color = %v, want %v", tok, got, w)
		}
		// G7: every default resolves to kindANSI or kindDefault.
		_, isANSI := got.ANSIIndex()
		if !isANSI && !got.IsDefault() {
			t.Errorf("token %d: default theme color %v is neither ANSI-16 nor terminal-default (G7)", tok, got)
		}
	}
}

// TestDefaultTheme: the framework default is NewTheme(ANSI(4)), ANSI-16-first.
func TestDefaultTheme(t *testing.T) {
	if DefaultTheme() != NewTheme(ANSI(4)) {
		t.Fatal("DefaultTheme() != NewTheme(ANSI(4))")
	}
	if dark, forced := DefaultTheme().Dark(); dark || forced {
		t.Fatal("DefaultTheme() forces adaptivity")
	}
}

// TestThemeDerivationCascade: derivations read slot values after options
// apply, so overriding a base slot moves its dependents' defaults.
func TestThemeDerivationCascade(t *testing.T) {
	th := NewTheme(ANSI(4),
		WithToken(TokenSurface, ANSI(0)),
		WithToken(TokenAccent, ANSI(5)),
		WithToken(TokenForeground, ANSI(7)),
	)
	for _, tc := range []struct {
		name string
		tok  Token
		want Color
	}{
		{"Panel follows Surface", TokenPanel, ANSI(0)},
		{"Boost follows Surface", TokenBoost, ANSI(0)},
		{"BorderFocused follows Accent", TokenBorderFocused, ANSI(5)},
		{"TextMuted follows Foreground", TokenTextMuted, ANSI(7)},
		{"Border follows Foreground", TokenBorder, ANSI(7)},
		{"Secondary still follows Primary", TokenSecondary, ANSI(4)},
	} {
		if got := th.Color(tc.tok); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}

	// An explicit override always wins over its derivation.
	th = NewTheme(ANSI(4), WithToken(TokenBorderFocused, ANSI(6)), WithToken(TokenAccent, ANSI(5)))
	if got := th.Color(TokenBorderFocused); got != ANSI(6) {
		t.Errorf("explicit BorderFocused = %v, want ANSI(6)", got)
	}
}

// TestThemeAdaptiveSlot: adaptivity lives behind the token — an Adaptive
// color in a Theme slot is the sanctioned replacement for token-in-Adaptive
// (§2.4).
func TestThemeAdaptiveSlot(t *testing.T) {
	ad := Adaptive(ANSI(0), ANSI(15))
	th := NewTheme(ANSI(4), WithToken(TokenForeground, ad))
	if got := th.Color(TokenForeground); got != ad {
		t.Fatalf("adaptive slot = %v, want %v", got, ad)
	}
	// The derivation cascade carries the adaptive color into dependents.
	if got := th.Color(TokenTextMuted); got != ad {
		t.Fatalf("TextMuted derived from adaptive Foreground = %v, want %v", got, ad)
	}
}

// TestThemeWithDark: WithDark forces adaptivity instead of the background
// probe (§2.5).
func TestThemeWithDark(t *testing.T) {
	if dark, forced := NewTheme(ANSI(4)).Dark(); dark || forced {
		t.Error("un-forced theme reports a dark override")
	}
	if dark, forced := NewTheme(ANSI(4), WithDark(true)).Dark(); !dark || !forced {
		t.Error("WithDark(true) not reported by Dark()")
	}
	if dark, forced := NewTheme(ANSI(4), WithDark(false)).Dark(); dark || !forced {
		t.Error("WithDark(false) must report (false, true)")
	}
}

// TestThemePanics: misconfiguration fails loud at construction — token-kind
// colors cannot occupy theme slots (single-pass resolution, §2.4) and
// out-of-range tokens are rejected.
func TestThemePanics(t *testing.T) {
	tokenColor, _ := New().Foreground(TokenPrimary).GetForeground()
	mustPanic(t, "NewTheme(token color)", func() { NewTheme(tokenColor) })
	mustPanic(t, "WithToken(token color)", func() { WithToken(TokenError, tokenColor) })
	mustPanic(t, "WithToken(out of range)", func() { WithToken(numTokens, ANSI(0)) })
	mustPanic(t, "WithToken(negative)", func() { WithToken(Token(-1), ANSI(0)) })
	mustPanic(t, "Theme.Color(out of range)", func() { NewTheme(ANSI(4)).Color(numTokens) })
	mustPanic(t, "Theme.Color(negative)", func() { NewTheme(ANSI(4)).Color(Token(-1)) })
}

// TestTokenCount pins the 20-slot vocabulary (11 base + 9 derived) so a
// token added or removed without revisiting the ADR fails a test.
func TestTokenCount(t *testing.T) {
	if numTokens != 20 {
		t.Fatalf("numTokens = %d, want 20 (11 base + 9 derived, ADR-0006 §2.5 rev 1)", numTokens)
	}
}
