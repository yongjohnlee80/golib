# vtparse

`vtparse` is the DEC ANSI escape-sequence parser behind golib's terminal code: Paul Flo Williams'
[vt100.net state machine](https://vt100.net/emu/dec_ansi_parser), byte at a time, with no I/O. It is
internal to `golib/tui`: `tui/term` decodes keyboard input with it, and `tui/vt` decodes what a
program writes.

## Install

`vtparse` is internal to `golib/tui` and cannot be imported from outside it. It comes with golib:

```bash
go get github.com/yongjohnlee80/golib
```

`tui/term` (keyboard input) and `tui/vt` (a program's output) are its importers.

## Features

- Every state of the vt100.net model: ground, escape, escape-intermediate, the four CSI states,
  OSC, the five DCS states, and SOS/PM/APC; ESC from anywhere restarts, CAN and SUB abort.
- `:` sub-parameters inside CSI and DCS parameters (kitty keys, SGR colon colours).
- UTF-8 in ground, emitted as runes.
- Bounded memory: 32 parameters, 4 sub-parameters on input and 8 on output, values saturating
  at 65535, string payloads capped at 4 KiB. Excess is consumed and dropped.
- Split-safe: a sequence cut across reads at any byte decodes the same.

## Two modes

| | input (zero value) | output (`Output: true`) |
|---|---|---|
| DEL in ground | an `Execute` (Backspace) | ignored |
| CAN, SUB | abort, then an `Execute` (Ctrl+X, Ctrl+Z) | abort, then an `Execute` |
| ESC ESC | the first ESC is an Escape key | restarts the sequence |
| ESC + a non-ASCII byte | an Alt-prefixed rune | dropped |
| C1 controls (U+0080–U+009F) | printed runes | act as ESC + (r − 0x40); U+009C ends a string |
| sub-parameters | 4 | 8 |

## Example

```go
p := vtparse.Parser{Output: true}
for _, b := range []byte("\x1b[1;31mred") {
	p.Feed(b, func(a *vtparse.Action) {
		switch a.Kind {
		case vtparse.CSI:
			fmt.Println("CSI", a.Param(0, 0), a.Param(1, 0), string(a.Final)) // CSI 1 31 m
		case vtparse.Print:
			fmt.Print(string(a.Rune))
		}
	})
}
```

An `Action`'s `Data` aliases the parser's storage and is valid only during the emit call.

## License

[Apache-2.0](../../../LICENSE)
