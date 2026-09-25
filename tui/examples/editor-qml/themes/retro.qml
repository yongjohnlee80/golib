// retro.qml — a 1990s Borland IDE.
//
// Black on grey for the menu bar and the status line, each menu's access key
// in red, the selected row on green, and the document on dark blue — the
// Turbo C++ look.
//
// A theme is a module of constants. The layout binds palette roles to these —
// `palette.window: Theme.menu.window` — so it never names a colour, and this
// file never names a widget. Selecting it is one line in editor.qml:
//
//     import editor.theme.retro 1.0
//
// The colours are the CGA palette Turbo C++ drew with, written as #rrggbb so
// they look the same in every terminal. An ANSI slot name ("blue") would take
// whatever the user's terminal palette maps it to, and modern palettes map
// blue to something far lighter than the original.

Theme {
    menu {
        window: "#aaaaaa"; windowText: "#000000"
        highlight: "#00aa00"; highlightedText: "#000000"
        accent: "#aa0000"
    }
    frame {
        window: "#0000aa"; windowText: "#aaaaaa"
        highlight: "#ffffff"
    }
    editor {
        base: "#0000aa"; text: "#ffff55"
        highlight: "#00aaaa"; highlightedText: "#000000"
    }
    status {
        window: "#aaaaaa"; windowText: "#000000"
    }
    // The APPLICATION palette: the Window sets it, and every surface that does
    // not set its own inherits it — the dialogs, their panes, the prompt.
    app {
        window: "#aaaaaa"; windowText: "#000000"
        button: "#00aa00"; buttonText: "#000000"
        highlight: "#00aa00"; highlightedText: "#ffffff"   // the focused button, and list row
        base: "#0000aa"; text: "#ffffff"                   // a file dialog's panes: the editor's blue
        inactive { highlight: "#555555"; highlightedText: "#55ffff" }  // a row not in use
        mid: "#555555"; light: "#ffffff"                   // a pane's frame, idle and in use
    }
}
