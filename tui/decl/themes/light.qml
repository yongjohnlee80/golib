// light.qml — a light workspace: pale chrome, white documents, dark text, one
// blue accent for what is selected.
//
// Written as #rrggbb so it looks the same in every terminal, a dark one
// included. Selecting it is one import line:
//
//     import tui.theme.light 1.0

Theme {
    app {
        window: "#e4e4e4"; windowText: "#1c1c1c"
        button: "#d0d0d0"; buttonText: "#1c1c1c"
        highlight: "#005fd7"; highlightedText: "#ffffff"
        base: "#ffffff"; text: "#262626"
        inactive { highlight: "#bcbcbc"; highlightedText: "#005f87" }
        mid: "#a8a8a8"; light: "#005fd7"
    }
    menu {
        window: "#dadada"; windowText: "#1c1c1c"
        highlight: "#005fd7"; highlightedText: "#ffffff"
        accent: "#af0000"
    }
    document {
        window: "#ffffff"; windowText: "#6c6c6c"
        highlight: "#005fd7"; highlightedText: "#ffffff"
        base: "#ffffff"; text: "#262626"
        selection: "#afd7ff"; selectedText: "#000000"
    }
    status {
        window: "#dadada"; windowText: "#303030"
    }
    syntax {
        keyword: "#0000af"; controlFlow: "#0000af"
        dataType: "#005f5f"; attribute: "#5f00af"; function: "#875f00"
        string: "#af0000"; specialChar: "#d70000"
        decVal: "#005faf"; float: "#005faf"; baseN: "#005faf"; constant: "#005faf"
        comment: "#8a8a8a"; alert: "#d70000"
        import: "#005f5f"; operator: "#444444"
    }
}
