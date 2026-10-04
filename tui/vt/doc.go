// Package vt is a terminal emulator's screen model: the bytes a program writes go in, and a grid of cells, a scrollback, a
// cursor, modes and replies come out. It does no I/O, so it is tested by
// feeding bytes and reading the grid.
//
// The vocabulary is what a shell, vim, less, htop, git and tmux need,
// measured against xterm:
//
//   - printing: UTF-8 grapheme clusters with East Asian width, autowrap with
//     a pending wrap (DECAWM), insert mode (IRM), DEC line drawing (ESC ( 0);
//   - cursor: CUP/HVP, CUU/CUD/CUF/CUB, CNL/CPL, CHA/HPA/VPA and their
//     relative forms, DECSC/DECRC, tab stops (HT, HTS, TBC, CHT, CBT);
//   - editing: ED, EL, ECH, ICH, DCH, IL, DL, REP;
//   - scrolling: DECSTBM regions, IND, RI, NEL, SU/SD; lines scrolled off the
//     top of the primary screen go to the scrollback;
//   - attributes: SGR in full, with 16 and 256 colours and truecolor in its
//     ';' and ':' forms; DECSCUSR cursor shapes;
//   - modes: the alternate screen (1049, 1047, 47), DECCKM, DECKPAM/DECKPNM,
//     bracketed paste (2004), mouse reporting (1000, 1002, 1003, SGR 1006),
//     focus events (1004), DECTCEM, DECOM, DECAWM, IRM, LNM and synchronized
//     output (2026);
//   - replies, through WithReply: DA1, DA2, DSR/CPR and DECRQM;
//   - OSC 0 and 2 set the title. OSC 52 is dropped (a program does not write
//     the host's clipboard), and every other OSC is ignored.
//
// Resize reflows the primary screen's wrapped lines, scrollback included;
// the alternate screen is cut or padded, as xterm does.
//
// A Screen is not safe for concurrent use: one goroutine writes and reads it
// (a widget does both on its UI loop).
package vt
