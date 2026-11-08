// Package widgettest is what the tests of tui/widget and the packages under it share: an App
// harness run from inside a package's own tests (Harness), and the key grid the key-map cells walk
// (KeyGrid, KeyGridDiff and the deviations they allow). Only tests import it; it depends on no
// widget package, so any of them can.
package widgettest
