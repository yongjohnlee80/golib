package gui

import (
	"os/exec"
	"slices"
	"strings"
	"sync"
)

// SYSTEM FONTS — what the cells are drawn in when the app names no font: the system's monospace
// family, as a terminal emulator uses, and an installed Nerd Font for the icons a shell prompt or
// a file tree draws. Both are asked of fontconfig (fc-match, fc-list), where the system has it
// (Linux, the BSDs); without it, nothing is added and the embedded Go Mono stays the cell font.
//
// Every name fontconfig gives a family goes in the list: Gio matches a family by some of its
// names and not others (it finds "CaskaydiaMono NFM" but not "CaskaydiaMono Nerd Font Mono"), so
// the list carries both and Gio takes the one it knows.

// nerdIcon is a codepoint every Nerd Font carries (a folder): a family covering it has the icons.
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

// systemMonospace is the system's monospace family, every name it has, or "".
func systemMonospace() string {
	return families(fontconfig("fc-match", "-f", "%{family}", "monospace"))
}

// nerdFallback is the first installed family carrying Nerd Font icons, every name it has, or "".
func nerdFallback() string {
	first, _, _ := strings.Cut(fontconfig("fc-list", ":charset="+nerdIcon, "family"), "\n")
	return families(first)
}

// cellTypeface is the typeface list the cells are drawn in: the app's font, or the system's
// monospace before Go Mono when it named none; then a Nerd Font for icons, then the generic
// monospace and emoji families (fallbackChain).
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

// MonospaceFamily is the typeface list monospace text drawn natively uses, as the cells do by
// default: the system's monospace, Go Mono, a Nerd Font for icons, then the generic families.
// It asks fontconfig once.
func MonospaceFamily() string { return monospaceOnce() }

// Families lists the installed font families, as fontconfig names them (fc-list), sorted and each
// once; mono lists only the monospace ones (fc-list :spacing=mono). A family fontconfig gives
// several names is listed by its first. Nil where fontconfig is missing (macOS, Windows).
func Families(mono bool) []string {
	args := []string{":", "family"}
	if mono {
		args = []string{":spacing=mono", "family"}
	}
	return familyList(fontconfig("fc-list", args...))
}

// familyList is fc-list's family lines as a sorted list, each family once, by its first name.
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
