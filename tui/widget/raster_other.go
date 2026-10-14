//go:build !darwin

package widget

// htmlApps are the installed browsers' executables to look for beside the PATH: none here, where a
// browser is on the PATH; tests replace it.
var htmlApps = func() []string { return nil }
