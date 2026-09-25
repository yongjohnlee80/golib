// mono.qml — black and white.
//
// The chrome is REVERSED: black text on a white menu bar and status line, and
// the selected row inverted back to white on black. The document area keeps the
// terminal's own colours, whatever they are.
//
// A theme is a module of constants. The layout binds palette roles to these —
// `palette.window: Theme.menu.window` — so it never names a colour, and this
// file never names a widget. Selecting it is one line in editor.qml:
//
//     import editor.theme.mono 1.0
//
// Colours are ANSI slots ("white", "brightblack"), "#rrggbb", or "default" for
// the terminal's own.

Theme {
    menu {
        window: "white"; windowText: "black"
        highlight: "black"; highlightedText: "white"
        accent: "black"             // the underline marks the key; no colour
    }
    frame {
        window: "default"; windowText: "default"
        highlight: "default"
    }
    editor {
        base: "default"; text: "default"
        highlight: "white"; highlightedText: "black"
    }
    status {
        window: "white"; windowText: "black"
    }
    // The APPLICATION palette: the Window sets it, and every surface that does
    // not set its own inherits it — the dialogs, their panes, the prompt.
    app {
        window: "white"; windowText: "black"
        button: "white"; buttonText: "black"
        highlight: "black"; highlightedText: "white"       // the focused button, and list row
        base: "white"; text: "black"                       // a file dialog's panes
        inactive { highlight: "brightblack"; highlightedText: "white" }  // a row not in use
        mid: "brightblack"; light: "black"                 // a pane's frame, idle and in use
    }
}
