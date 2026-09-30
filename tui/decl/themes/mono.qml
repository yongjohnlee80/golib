// mono.qml — black and white.
//
// The chrome is REVERSED: black text on a white menu bar and status line, and
// the selected row inverted back to white on black. What shows a DOCUMENT — an
// editor, a table, a tree — keeps the terminal's own colours, whatever
// they are, so the syntax reads the same in any terminal.
//
// Colours are ANSI slots ("white", "brightblack"), "#rrggbb", or "default" for
// the terminal's own. Selecting it is one import line:
//
//     import tui.theme.mono 1.0

Theme {
    app {
        window: "white"; windowText: "black"
        button: "white"; buttonText: "black"
        highlight: "black"; highlightedText: "white"
        base: "default"; text: "default"
        inactive { highlight: "brightblack"; highlightedText: "white" }
        mid: "brightblack"; light: "black"
    }
    menu {
        window: "white"; windowText: "black"
        highlight: "black"; highlightedText: "white"
        accent: "black"             // the underline marks the key; no colour
    }
    document {
        window: "default"; windowText: "default"
        highlight: "brightwhite"; highlightedText: "black"
        base: "default"; text: "default"
        selection: "white"; selectedText: "black"
        cursor: "brightwhite"; lineNumber: "brightblack"
    }
    status {
        window: "white"; windowText: "black"
    }
    // Two colours: the syntax is carried by brightness — comments dimmed,
    // keywords bright.
    syntax {
        keyword: "brightwhite"; controlFlow: "brightwhite"
        dataType: "default"; attribute: "default"; function: "default"
        string: "default"; specialChar: "brightwhite"
        decVal: "default"; float: "default"; baseN: "default"; constant: "brightwhite"
        comment: "brightblack"; alert: "brightwhite"
        import: "default"; operator: "default"
    }
}
