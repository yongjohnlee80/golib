package gui

import (
	"fmt"
	"os"

	"gioui.org/app"
)

// Main runs a program whose App uses this backend, and gives Gio the main thread, which macOS
// and iOS require for windows. run builds the App and runs it; Main runs it on a new goroutine.
//
// Gio's main loop never returns on desktop platforms, so Main ends the process when run returns:
// status 0, or status 1 with the error on stderr. Put cleanup that must happen inside run.
func Main(run func() error) {
	go func() {
		if err := run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}
