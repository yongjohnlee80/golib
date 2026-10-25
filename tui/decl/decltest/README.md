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
| `Screen.Keys(t, evs…)` | deliver key or mouse events, and return once the program has dispatched them: a focus move, a capture or a handler they run has happened, so a read after it sees their effect. A frame, or work a handler posts, can come later: wait for it. A loop the test holds never dispatches them, and Keys fails after `WaitTimeout` |
| `Screen.KeysAsync(t, evs…)` | queue them and return at once, before they're dispatched: only for input behind a loop the test holds on purpose |
| `Screen.Quit()` | closed when the program stops |
| `Rune`, `Type`, `Ctrl`, `Alt` | key events |
| `WaitTimeout` | how long every wait lasts (3s) |
| `CheckStyle(t, style)` | fails t for each type a style replaces that a document could tell from the standard one: its contract differs, or, mounted in that type's minimal declaration under the style, a getter, its Restyle or its Destroyed refuses or panics on the widget the style's Build makes |

What `Check` mounts, and why, is in [USAGE.md §11](../USAGE.md#11-testing-a-qml-screen).

## Licence

See the repository's [LICENSE](../../../LICENSE).
