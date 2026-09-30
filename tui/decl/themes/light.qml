// light.qml — a light workspace, in paper tones: warm off-white documents with dark warm-brown
// text, parchment chrome, one muted blue accent for what is selected, and a burnt-orange cursor
// that is seen without glaring.
//
// Written as #rrggbb so it looks the same in every terminal, a dark one
// included. Selecting it is one import line:
//
//     import tui.theme.light 1.0

Theme {
    app {
        window: "#ebe6d9"; windowText: "#2b2620"
        button: "#ddd6c6"; buttonText: "#2b2620"
        highlight: "#2f5f8f"; highlightedText: "#fbf8f1"
        base: "#f6f2e7"; text: "#2e2a24"
        inactive { highlight: "#d8d0bd"; highlightedText: "#2f5f8f" }
        mid: "#b3aa96"; light: "#2f5f8f"
    }
    menu {
        window: "#e4ddcc"; windowText: "#2b2620"
        highlight: "#2f5f8f"; highlightedText: "#fbf8f1"
        accent: "#a23a1f"
    }
    document {
        window: "#f6f2e7"; windowText: "#7a7163"
        highlight: "#2f5f8f"; highlightedText: "#fbf8f1"
        base: "#f6f2e7"; text: "#2e2a24"
        selection: "#e8d9ae"; selectedText: "#1f1b16"
        cursor: "#d2691e"; lineNumber: "#aba08a"
    }
    status {
        window: "#e4ddcc"; windowText: "#3a342b"
    }
    syntax {
        keyword: "#1f4e8c"; controlFlow: "#1f4e8c"
        dataType: "#1f6363"; attribute: "#6a3d9a"; function: "#7a5200"
        string: "#9c2f1f"; specialChar: "#b8401f"
        decVal: "#1f5a8c"; float: "#1f5a8c"; baseN: "#1f5a8c"; constant: "#1f5a8c"
        comment: "#8a806e"; alert: "#c0392b"
        import: "#1f6363"; operator: "#4a443a"
    }
}
