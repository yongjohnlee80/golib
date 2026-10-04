# vt

`vt` is a terminal emulator's screen model: the bytes a program writes go in, and a grid of cells,
a scrollback, a cursor, modes and replies come out. It does no I/O, so it is tested by feeding bytes
and reading the grid, and it serves any host: golib's `widget.Terminal` over a PTY, a test harness,
a remote screen.

## Features

The vocabulary a shell, vim, less, htop, git and tmux need, measured against xterm:

- **printing:** UTF-8 grapheme clusters joined across writes (combining marks, ZWJ sequences,
  flags, VS16), East Asian width, autowrap with a pending wrap, insert mode, DEC line drawing;
- **cursor:** CUP/HVP, the relative moves, CHA/HPA/VPA, DECSC/DECRC, tab stops;
- **editing:** ED, EL, ECH, ICH, DCH, IL, DL, REP, never leaving half a wide character;
- **scrolling:** DECSTBM regions, IND, RI, NEL, SU/SD; rows leaving the top of the primary screen
  go to a capped scrollback;
- **attributes:** SGR in full, 16 and 256 colours and truecolor in both forms; DECSCUSR shapes;
- **modes:** the alternate screen (1049, 1047, 47), DECCKM, DECKPAM, bracketed paste, mouse
  reporting (9, 1000, 1002, 1003, SGR 1006), focus events, DECTCEM, DECOM, DECAWM, IRM, LNM and
  synchronized output (2026);
- **replies:** DA1, DA2, DSR/CPR, DECRQM, through `WithReply`;
- **titles:** OSC 0 and 2; OSC 52 (clipboard) is dropped on purpose.

`Resize` reflows the primary screen and the scrollback: rows autowrap broke are joined and broken
again at the new width, and the cursor keeps its place in the text. The alternate screen is cut or
padded, as xterm's is.

## Example

```go
var replies []byte
s := vt.New(24, 80, vt.WithReply(func(b []byte) { replies = append(replies, b...) }))
s.Write([]byte("\x1b[1;31mhello\x1b[0m\r\nworld\x1b[6n"))
c := s.Cell(0, 0) // "h", bold, red
row, col, _, _ := s.Cursor()
fmt.Println(c.Content, row, col, string(replies)) // h 1 5 ESC[2;6R
```

A Screen belongs to one goroutine: the one that writes it also reads it.

## Testing

Beyond the per-sequence cells, `testdata/transcripts` holds recorded programs (a coloured prompt,
`ls --color`, nvim, less) with the screen tmux showed for the same bytes; the suite replays them. To
record again, with tmux installed:

```bash
VT_RECORD=1 go test ./tui/vt -run TestRecordTranscripts
```

## License

[Apache-2.0](../../LICENSE)
