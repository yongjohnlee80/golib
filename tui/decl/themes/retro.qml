// retro.qml — a 1990s Borland IDE, in CGA colours.
//
// Grey chrome with black text, the selected row on green, and every document —
// an editor, a table, a tree — on a deep navy (#000070): CGA's blue (#0000aa)
// was too bright to read on for long. Written as #rrggbb so it
// looks the same in every terminal: an ANSI slot name would take whatever the
// user's palette maps it to.
//
// A theme is a module of constants. The layout binds palette roles to these, so
// it never names a colour, and this file never names a widget. Selecting it is
// one import line:
//
//     import tui.theme.retro 1.0

Theme {
    // The APPLICATION palette: the Window sets it, and every surface that does
    // not set its own inherits it — the dialogs, their panes, the forms.
    app {
        window: "#aaaaaa"; windowText: "#000000"
        button: "#00aa00"; buttonText: "#000000"
        highlight: "#00aa00"; highlightedText: "#ffffff"
        base: "#000070"; text: "#ffffff"
        inactive { highlight: "#555555"; highlightedText: "#55ffff" }
        mid: "#555555"; light: "#ffffff"
        backdrop: "#00004a"
    }
    menu {
        window: "#aaaaaa"; windowText: "#000000"
        highlight: "#00aa00"; highlightedText: "#000000"
        accent: "#aa0000"
    }
    // A document's surface: the panes' frames and what is in them.
    document {
        window: "#000070"; windowText: "#aaaaaa"
        highlight: "#ffffff"; highlightedText: "#000000"
        base: "#000070"; text: "#ffff55"
        selection: "#00aaaa"; selectedText: "#000000"
        cursor: "#ffff55"; lineNumber: "#5555aa"
    }
    status {
        window: "#aaaaaa"; windowText: "#000000"
    }
    // KSyntaxHighlighting's styles, set once on the Window. On the deep navy.
    syntax {
        keyword: "#ffffff"; controlFlow: "#ffffff"
        dataType: "#55ff55"; attribute: "#55ffff"; function: "#ffff55"
        string: "#ff55ff"; specialChar: "#ff5555"
        decVal: "#55ffff"; float: "#55ffff"; baseN: "#55ffff"; constant: "#55ffff"
        comment: "#aaaaaa"; alert: "#ff5555"
        import: "#55ff55"; operator: "#aaaaaa"
    }
}
