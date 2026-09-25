# tui/decl/controls — TextField and Popup

Qt Quick Controls' `TextField` and `Popup` for the golib/tui QML vocabulary.

```go
p, err := tuidecl.NewProgram(tuidecl.Types(controls.Types()...), …)
```

```qml
Shortcut { sequence: "Ctrl+P"; onActivated: prompt.open() }

Popup {                                   // a command prompt
    id: prompt; modal: true
    TextField {
        placeholderText: "command"
        onAccepted: App.run(text)
    }
}
```

| type | over | properties | methods | signals |
| --- | --- | --- | --- | --- |
| `TextField` | `widget.TextInput` | `text` (runtime), `placeholderText`, `echoMode: TextInput.Normal \| TextInput.Password` | — | `accepted(text)`, `textEdited(text)` |
| `Popup` | `widget.Float` | `modal`, `dim` | `open()`, `close()` | `opened()`, `closed()` |

- A handler reads `text` as in Qt, where it runs in its object's scope.
- A TextField wears `base`/`text` and `highlight`/`highlightedText`; inside a
  coloured card it wears the card's colours.
- A Popup opens over its Window, or on the adapter's `WithOverlay` host. Escape
  closes it while it has the keyboard; `modal: true` takes the keyboard and
  gives it back when closed. It is centred — positioning by `x`/`y` needs
  expressions the evaluator does not run.

This package is written with nothing but `tuidecl`'s exported contract, in a
package of its own so the compiler holds it to that: it is the proof that a
program's own widgets can do everything the built-ins do. See
[USAGE.md §6](../USAGE.md#6-your-own-go-widgets-in-qml).

## Licence

See the repository's [LICENSE](../../../LICENSE).
