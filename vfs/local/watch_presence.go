//go:build linux || darwin || windows

package local

// presence is what an lstat at handling time found at a path: the FSEvents and ReadDirectoryChangesW
// watches decide what an event means by it, since what they report says what happened at some point,
// not what is there now.
type presence uint8

const (
	absent presence = iota
	presentFile
	presentDir
	unknown // lstat failed other than "does not exist": report a write and let the consumer re-read
)
