# tui/decl/decltest — test a QML program in `go test`

A broken QML document is a runtime error unless a test finds it first. This
package holds the two tests every [`tui/decl`](..) program should have. Both take
the options `tuidecl.NewProgram` takes.

```go
// Every QML file the program can load — including the theme the layout does
// not import and the dialog no screen uses yet.
func TestTheQMLIsSound(t *testing.T) {
    decltest.Check(t, programOptions()...)
}

// The program running on a test backend, stopped when the test ends.
func TestSaveAsksForAName(t *testing.T) {
    s := decltest.Run(t, 80, 24, programOptions()...)
    s.Keys(t, decltest.Ctrl('s'))
    s.WaitForText(t, "Save As")
}
```

| | |
| --- | --- |
| `Check(t, opts…)` | fails t once per problem `tuidecl.Check` finds |
| `Run(t, w, h, opts…) *Screen` | runs the program; a handler error fails t unless the options give an `ErrorSink` |
| `RunWith(t, w, h, setup, opts…)` | the same, with `setup(program)` between building and running — where a program's main binds its host (finds widgets, loads a file) before the first frame |
| `Screen.WaitFor(t, what, cond)` / `WaitForText(t, text)` | poll the screen until it shows what you expect |
| `Screen.Keys(t, evs…)` | deliver key events |
| `Screen.Quit()` | closed when the program stops |
| `Rune`, `Type`, `Ctrl`, `Alt` | key events |
| `WaitTimeout` | how long every wait lasts (3s) |

What `Check` mounts, and why, is in [USAGE.md §11](../USAGE.md#11-testing-a-qml-screen).

## Licence

See the repository's [LICENSE](../../../LICENSE).
