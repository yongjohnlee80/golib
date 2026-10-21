// Package xterm is xterm's input encoding, written once for both directions: the bytes and
// parameters a key, a modifier, a paste and a mouse report are sent as. term's decoder reads them,
// the Terminal widget writes them to the program it runs, and web maps a browser's buttons through
// them, so a key added here is decoded and encoded alike. Package sgr beside it holds the text
// attributes' SGR codes, which vt parses and term emits.
//
// It encodes nothing itself; each table and lookup states the xterm form it names
// (https://invisible-island.net/xterm/ctlseqs/ctlseqs.html).
package xterm
