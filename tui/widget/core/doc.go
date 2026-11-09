// Package core is what every widget of golib's builds on: the Base a widget embeds, the overlay
// host and its layers (menus, popups, dialogs, floats), the containers, the text fields, lists and
// menus, and the key commands they read. The components (the editor, the terminal, the tables, the
// file views, the HTML view) are built on it.
//
// tui/widget forwards every name this package exports (its facade, generated), so a program that
// imports tui/widget compiles unchanged.
package core
