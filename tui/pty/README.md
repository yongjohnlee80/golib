# pty

`pty` starts a program on a pseudo-terminal, on Linux and macOS, with the standard library and
`golang.org/x/sys` only. It is the process layer of golib's terminal widget, and it stands alone:
anything that needs to drive an interactive program can use it.

## Install

```bash
go get github.com/yongjohnlee80/golib/tui/pty
```

## Features

- `Start` opens a terminal pair (`/dev/ptmx`) and starts the program as a session leader with the
  terminal as its controlling tty, so job control, `Ctrl-C` and `SIGWINCH` work as in any terminal.
- `Read` is the program's output (`io.EOF` once the terminal is hung up), `Write` its keyboard.
- `Resize` sets the size; the program's foreground group gets `SIGWINCH`.
- `Signal` reaches the program's process group, never a reused pid on Linux.
- `Wait` reports the exit status as a shell does: the code, or 128 + the signal.
- `Close` hangs up as closing a terminal window does: `SIGHUP` (and `SIGCONT`) to the group, the
  master closed once the program exits, and `SIGKILL` to the group after a two-second grace. A
  shell forwards the hang-up to its jobs. `Close` can block for the grace period, so a UI calls it
  off its loop.
- Elsewhere (Windows), `Start` returns an error matching `errs.ErrUnsupported`.

On macOS the kernel hangs up a session leader's terminal as the leader exits, which can drop output
not read yet: a program that prints and exits at once may lose its last output there. A shell, which
outlives the commands it runs, does not.

What `Close` does not promise: a program that detached itself (`setsid`, `nohup`, `disown`, a
daemon) keeps running, as it would after any terminal closed.

## Example

```go
p, err := pty.Start(pty.Cmd{Path: "bash", Args: []string{"-i"}, Rows: 24, Cols: 80})
if err != nil {
	return err
}
defer p.Close()
go io.Copy(os.Stdout, p)
io.WriteString(p, "echo hello; exit 3\n")
code, _ := p.Wait() // 3
```

## License

[Apache-2.0](../../LICENSE)
