// sepia.qml — a reading light theme: warm sepia paper (#e6d9b9), toned down from a reader's
// bright #f4ecd8 so a long read is easy on the eyes, with brown ink (#5b4636), the tones of a
// reader's sepia mode, parchment chrome a shade deeper, and earthy syntax colours (rust,
// olive, plum, ochre, brick, slate) that keep 5:1 or better on the page.
//
// Written as #rrggbb so it looks the same in every terminal, a dark one
// included. Selecting it is one import line:
//
//     import tui.theme.sepia 1.0

Theme {
    app {
        window: "#dacca9"; windowText: "#4a3826"
        button: "#cfbf9b"; buttonText: "#4a3826"
        highlight: "#8a5a2b"; highlightedText: "#fbf6ea"
        base: "#e6d9b9"; text: "#5b4636"
        inactive { highlight: "#cfbd96"; highlightedText: "#8a5a2b" }
        mid: "#b5a380"; light: "#8a5a2b"
        backdrop: "#d4c4a0"
    }
    menu {
        window: "#d9caa6"; windowText: "#4a3826"
        highlight: "#8a5a2b"; highlightedText: "#fbf6ea"
        accent: "#9b3d1f"
    }
    document {
        window: "#e6d9b9"; windowText: "#8c7656"
        highlight: "#8a5a2b"; highlightedText: "#fbf6ea"
        base: "#e6d9b9"; text: "#5b4636"
        selection: "#d6b97a"; selectedText: "#3b2a1a"
        cursor: "#b5541f"; lineNumber: "#a8946c"
    }
    status {
        window: "#d9caa6"; windowText: "#5b4636"
    }
    syntax {
        keyword: "#7c3f12"; controlFlow: "#7c3f12"
        dataType: "#4f6b2f"; attribute: "#7a4b6b"; function: "#6b5200"
        string: "#8f3b2a"; specialChar: "#a8461f"
        decVal: "#2f5e6b"; float: "#2f5e6b"; baseN: "#2f5e6b"; constant: "#2f5e6b"
        comment: "#8a7350"; alert: "#b0301e"
        import: "#4f6b2f"; operator: "#6b5a44"
    }
}
