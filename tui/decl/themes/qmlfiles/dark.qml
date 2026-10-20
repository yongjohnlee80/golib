// dark.qml — a dark workspace: charcoal chrome, near-black documents, one blue
// accent for what is selected.
//
// Written as #rrggbb so it looks the same in every terminal. Selecting it is one
// import line:
//
//     import tui.theme.dark 1.0

Theme {
    app {
        window: "#3a3a3a"; windowText: "#dadada"
        button: "#4e4e4e"; buttonText: "#eeeeee"
        highlight: "#005f87"; highlightedText: "#ffffff"
        base: "#1c1c1c"; text: "#d0d0d0"
        inactive { highlight: "#444444"; highlightedText: "#87afd7" }
        mid: "#585858"; light: "#87afd7"
        backdrop: "#121212"
    }
    menu {
        window: "#303030"; windowText: "#dadada"
        highlight: "#005f87"; highlightedText: "#ffffff"
        accent: "#ffaf5f"
    }
    document {
        window: "#1c1c1c"; windowText: "#8a8a8a"
        highlight: "#87afd7"; highlightedText: "#000000"
        base: "#1c1c1c"; text: "#d0d0d0"
        selection: "#005f87"; selectedText: "#ffffff"
        cursor: "#ffaf00"; lineNumber: "#5f5f5f"
    }
    status {
        window: "#303030"; windowText: "#bcbcbc"
    }
    syntax {
        keyword: "#87afd7"; controlFlow: "#87afd7"
        dataType: "#87d7af"; attribute: "#afafd7"; function: "#d7d787"
        string: "#d7af87"; specialChar: "#ff8787"
        decVal: "#afd7ff"; float: "#afd7ff"; baseN: "#afd7ff"; constant: "#afd7ff"
        comment: "#6c6c6c"; alert: "#ff5f5f"
        import: "#87d7af"; operator: "#a8a8a8"
    }
}
