package widget

import (
	"os"
	"path/filepath"
)

// htmlAppsDarwin are the browsers HTMLRasterizer runs headless when none is on the PATH, as macOS
// installs them: an application bundle's executable, in /Applications or the user's own.
var htmlAppsDarwin = []string{
	"Google Chrome.app/Contents/MacOS/Google Chrome",
	"Chromium.app/Contents/MacOS/Chromium",
	"Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
}

// htmlApps are the installed browsers' executables to look for: /Applications's, then the user's
// ~/Applications'; tests replace it.
var htmlApps = func() []string {
	roots := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	var out []string
	for _, root := range roots {
		for _, app := range htmlAppsDarwin {
			out = append(out, filepath.Join(root, app))
		}
	}
	return out
}
