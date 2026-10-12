# docks: a dock style, and keys where they belong

Two panels from one dock style, each with its own `d`.

- [`docks/Panel.qml`](docks/Panel.qml) is the style: a `Drawer` from the edge a use names, framed
  under its heading, its content in a `FocusScope`.
- [`main.qml`](main.qml) uses it twice. Each use gives the panel's content (a `ListView`) and its
  `Shortcut`s, which are live while focus is in that panel.

| Where focus is | `d` | `n` |
| --- | --- | --- |
| the page | the Window's | the page's FocusScope's |
| the files panel | delete, the panel's | — |
| the notes panel | done, the panel's | — |

F2 and F3 open the panels; Esc or q closes one; Ctrl+Q quits.

    go run ./tui/examples/docks

The rules are in [tui/decl/USAGE.md §4b](../../decl/USAGE.md#4b-panels-a-dock-style-is-a-component):
a Shortcut is live while focus is inside the element that declares it, and a dock style is a
component rooted at a `Drawer`.
