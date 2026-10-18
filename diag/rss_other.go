//go:build !linux

package diag

// residentSize is 0 where the standard library offers no current resident size.
func residentSize() uint64 { return 0 }
