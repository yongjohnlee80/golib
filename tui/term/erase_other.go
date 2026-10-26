//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package term

import "os"

// eraseByte is 0 where there is no termios to ask: Windows' consoles and Windows Terminal send DEL
// for Backspace, so a lone ^H decodes as Ctrl+H, unchanged.
func eraseByte(*os.File) byte { return 0 }
