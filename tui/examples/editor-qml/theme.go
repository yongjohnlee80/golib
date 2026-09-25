package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
)

// SWITCHING THEME FROM THE MENU — Option > Theme.
//
// A theme is chosen by one line of editor.qml, its import:
//
//	import editor.theme.retro 1.0
//
// so switching theme at runtime is that line, rewritten, and the layout
// reloaded — the path a hot reload takes. Nothing is rebuilt that the theme
// does not touch: the buffer, the cursor and the mode stay as they were.
//
// Under -dev the file on disk stays the authority: the menu rewrites the
// layout in memory, and the next save of editor.qml brings back its own
// import.

// themeImport is a theme import line; its group is the theme's name.
var themeImport = regexp.MustCompile(`(?m)^import editor\.theme\.([a-z][a-z0-9]*) ` +
	regexp.QuoteMeta(moduleVersion))

// themeOf is the theme a layout imports, "" for none.
func themeOf(src []byte) string {
	if m := themeImport.FindSubmatch(src); m != nil {
		return string(m[1])
	}
	return ""
}

// themeState is what the menu reads: which theme is checked.
func themeState(theme string) map[string]any {
	return map[string]any{
		"App.themeRetro": theme == "retro",
		"App.themeMono":  theme == "mono",
	}
}

// useTheme switches to the named theme: the layout with its import line
// rewritten, reloaded. A theme the program does not ship is refused, and the
// screen stays as it was.
func (h *Host) useTheme(name string) error {
	if !h.hasTheme(name) {
		return h.message(fmt.Sprintf("no theme %q", name))
	}
	src, err := h.layoutSource()
	if err != nil {
		return err
	}
	if themeOf(src) == "" {
		return h.message("editor.qml imports no theme to switch")
	}
	next := themeImport.ReplaceAll(src, []byte("import editor.theme."+name+" "+moduleVersion))
	// AFTER the handler: a menu row's signal is still being emitted, and the
	// engine reconciles only between emissions, not inside one.
	h.p.Post(func() {
		if _, err := h.p.Reload(next); err != nil {
			_ = h.message("theme: " + err.Error())
			return
		}
		if h.dev == "" {
			h.layoutSrc = next
		}
		state := themeState(name)
		state["App.status"] = "theme: " + name
		_ = h.p.SetMany(state)
	})
	return nil
}

// layoutSource is the layout as it stands: the file under -dev, else the
// copy this host last loaded.
func (h *Host) layoutSource() ([]byte, error) {
	if h.dev != "" {
		return os.ReadFile(path.Join(h.dev, "editor.qml"))
	}
	return h.layoutSrc, nil
}

// hasTheme reports whether the program ships the named theme.
func (h *Host) hasTheme(name string) bool {
	var themes fs.FS = themeFiles
	if h.dev != "" {
		themes = os.DirFS(h.dev)
	}
	_, err := fs.Stat(themes, "themes/"+name+".qml")
	return err == nil && !strings.ContainsAny(name, "/.")
}
