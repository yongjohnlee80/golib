// Package qmlfiles is the toolkit's theme files, one QML Theme per theme, and their names, with
// nothing of the toolkit: a server checking a stored theme name imports it and links no UI.
// Package themes offers the same files to a layout.
package qmlfiles

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed *.qml
var files embed.FS

// FS is the themes' QML files, one per theme: dark.qml, light.qml, mono.qml, ….
func FS() fs.FS { return files }

// Names are the themes, in the order a menu lists them.
func Names() []string {
	entries, _ := fs.ReadDir(files, ".")
	var out []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".qml"); ok {
			out = append(out, name)
		}
	}
	return out
}
