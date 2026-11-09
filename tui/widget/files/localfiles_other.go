//go:build !windows

package files

import "os"

// localFiles is the local disk, rooted at "/".
func localFiles() FileSource {
	home, _ := os.UserHomeDir()
	return FileSource{FS: os.DirFS("/"), Root: "/", Home: home}
}
