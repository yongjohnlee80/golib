// retro.qml — a 1990s Borland IDE.
//
// Black on white for the menu bar and the status line, each menu's access key
// in red, the selected row on green, and the document on blue — the
// Turbo C++ look.
//
// A theme is a module of constants. The layout binds palette roles to these —
// `palette.window: Theme.menu.window` — so it never names a colour, and this
// file never names a widget. Selecting it is one line in editor.qml:
//
//     import editor.theme.retro 1.0
//
// Colours are ANSI slots ("blue", "brightyellow"), "#rrggbb", or "default" for
// the terminal's own. ANSI slots follow the user's terminal palette, which is
// what the original text-mode colours did too.

Theme {
    menu {
        window: "white"; windowText: "black"
        highlight: "green"; highlightedText: "black"
        accent: "red"
    }
    frame {
        window: "blue"; windowText: "white"
        highlight: "brightwhite"
    }
    editor {
        base: "blue"; text: "brightyellow"
        highlight: "cyan"; highlightedText: "black"
    }
    status {
        window: "white"; windowText: "black"
    }
}
