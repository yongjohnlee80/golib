package gui

import (
	"fmt"
	"os"

	"gioui.org/app"
)

// Main executes a GUI program and gives Gio the main OS thread, satisfying platform constraints
// on macOS and iOS that require window lifecycle and graphics context management to bind to
// the primary thread.
//
// The provided run function typically instantiates the [Backend], constructs the component tree,
// initializes [tui.App], and executes [tui.App.Run]. Main launches run on a newly spawned goroutine
// while parking the primary thread in Gio's platform event loop via [app.Main].
//
// When run completes:
//   - If run returns nil, Main terminates the process with exit code 0 via os.Exit(0).
//   - If run returns an error, the error message is written to os.Stderr and the process exits with code 1.
//
// NOTE: Because Gio's desktop main loop does not return normally, any deferred teardown or cleanup
// logic must execute within the body of run or via [tui.App] shutdown hooks.
func Main(run func() error) {
	beforeGio()
	go func() {
		if err := run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}
