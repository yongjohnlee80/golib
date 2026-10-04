// Package vtparse is the DEC ANSI parser golib's terminal code shares: a
// byte-at-a-time implementation of the vt100.net state machine, with no I/O.
//
// One Parser reads one stream. Its zero value reads an input stream, as
// tui/term's key decoder does, where DEL, CAN, SUB and ESC ESC are keys.
// With Output set it reads what a program writes to a terminal, as tui/vt
// does, where they keep vt100.net's meaning and C1 controls act. Feed takes
// one byte and emits zero or more Actions; a sequence split across reads
// decodes as if it arrived whole.
package vtparse
