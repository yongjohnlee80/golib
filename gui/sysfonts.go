package gui

import (
	"os/exec"
	"slices"
	"strings"
	"sync"
)

// System font discovery and fallback resolution.
//
// When an application does not explicitly configure a custom font family, the GUI backend
// discovers and loads the host operating system's configured monospace and icon fonts.
//
// # Font Resolution Cascade
//
// On systems providing the `fontconfig` subsystem (Linux, BSD), font families are queried
// dynamically to build an ordered fallback chain for Gio text shaping:
//
//	┌─────────────────────────────────────────────────────────────┐
//	│                   Font Resolution Cascade                   │
//	└──────────────────────────────┬──────────────────────────────┘
//	                               │
//	    1. System Monospace:       ▼ (via `fc-match monospace`)
//	       e.g., "JetBrains Mono", "Hack", "Fira Code"
//	                               │
//	    2. Embedded Base Font:     ▼
//	       "Go Mono" (bundled statically with the binary)
//	                               │
//	    3. Nerd Font Icons:        ▼ (via `fc-list :charset=f101`)
//	       e.g., "Symbols Nerd Font Mono" for terminal icons & glyphs
//	                               │
//	    4. Generic Fallbacks:      ▼
//	       "monospace, emoji" for international scripts and emoji symbols
//
// Multiple Family Aliases:
// Fontconfig often exposes identical typefaces under multiple aliases (e.g. "CaskaydiaMono NFM"
// and "CaskaydiaMono Nerd Font Mono"). Both variants are included in the typeface list to ensure
// Gio's font shaper matches the font regardless of internal naming conventions.

// nerdIcon is a Private Use Area codepoint (U+F101, folder icon) present in Nerd Font distributions.
const nerdIcon = "f101"

// fontconfig runs a fontconfig tool, its output or "" when it is missing or fails.
var fontconfig = func(name string, args ...string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	out, err := exec.Command(path, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// families is a fontconfig family line ("JetBrainsMono Nerd Font,JetBrainsMono NF") as a Gio
// typeface list ("JetBrainsMono Nerd Font, JetBrainsMono NF"); "" for none.
func families(line string) string {
	var names []string
	for _, n := range strings.Split(strings.TrimSpace(line), ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, ", ")
}

// systemMonospace queries fontconfig for the primary system monospace font family.
// Returns a comma-separated list of recognized aliases, or an empty string if fontconfig is unavailable.
func systemMonospace() string {
	return families(fontconfig("fc-match", "-f", "%{family}", "monospace"))
}

// nerdFallback discovers the first installed typeface containing Nerd Font icons (PUA U+F101).
// Returns a comma-separated list of family aliases, or an empty string if none are installed.
func nerdFallback() string {
	first, _, _ := strings.Cut(fontconfig("fc-list", ":charset="+nerdIcon, "family"), "\n")
	return families(first)
}

// cellTypeface constructs the prioritized font fallback chain used to render character cells.
//
// Assembly Order:
//  1. Custom typeface specified in configuration, or system monospace if none set.
//  2. Embedded Go Mono baseline font.
//  3. Nerd Font icon family (if installed and not already present).
//  4. Generic fallback chains ("monospace, emoji").
func cellTypeface(c config) string {
	tf := c.typeface
	if !c.fontSet {
		if m := systemMonospace(); m != "" {
			tf = m + ", " + tf
		}
	}
	if n := nerdFallback(); n != "" && !namesFamily(tf, strings.Split(n, ", ")[0]) {
		tf += ", " + n
	}
	return fallbackChain(tf)
}

var monospaceOnce = sync.OnceValue(func() string { return cellTypeface(defaultConfig()) })

// MonospaceFamily returns the default monospace typeface fallback chain used for cells and code blocks.
//
// The font discovery process queries fontconfig once at process startup and caches the resulting
// string across all windows and canvases.
func MonospaceFamily() string { return monospaceOnce() }

// Families queries and returns all installed font families available on the host system.
//
// Parameters:
//   - mono: when true, restricts the listing to monospace fonts only (`fc-list :spacing=mono`).
//
// Returns a sorted slice of unique family names, or nil if fontconfig is unavailable (such as on macOS or Windows).
func Families(mono bool) []string {
	args := []string{":", "family"}
	if mono {
		args = []string{":spacing=mono", "family"}
	}
	return familyList(fontconfig("fc-list", args...))
}

// familyList parses the output of `fc-list`, returning a sorted slice of unique font family names.
func familyList(out string) []string {
	seen := map[string]bool{}
	var list []string
	for _, line := range strings.Split(out, "\n") {
		first, _, _ := strings.Cut(line, ",")
		if first = strings.TrimSpace(first); first != "" && !seen[first] {
			seen[first] = true
			list = append(list, first)
		}
	}
	slices.Sort(list)
	return list
}
