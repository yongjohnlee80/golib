// Package paths is the one place a path's form is decided: its separator, its drive or share, ~, and
// file: URLs. It uses only the standard library.
//
// Two kinds of path exist, and they are different types:
//
//   - an [OS] path is what the file system takes, in the OS's own form (C:\Users\me, /home/me);
//   - a [Logical] path is a place in a tree, a URL's path or a value on the wire. Its separator is
//     [LogicalSep] on every OS, as in io/fs and URLs. Only [ParseLogical] and [Rel] make one, so a
//     Logical in hand has been checked.
//
// [Under] turns a Logical into an OS path below a root, and [Rel] turns an OS path back. Nothing else
// converts between them.
//
// [Encode] gives a name from data (a scope, a codename, an id) one legal path element, the same on
// every OS, and [Decode] reverses it.
//
// # What this package cannot see
//
// Everything here is lexical. It reads strings and never touches the file system, so it cannot see
// symlinks, junctions or 8.3 short names. [Within] is not containment: what a program may reach is
// decided where the file is opened, by a root such as [os.Root] or vfs/local.
//
// The rules for each OS are plain string work, selected by a flag, so Windows' rules are tested on
// every OS. The tests state each difference between the OSes that they cover.
//
// Concurrency: the package is pure functions of their arguments; call them from anywhere.
package paths
