// shades_of_purple.qml — Shades of Purple: deep indigo surfaces with bold yellow, orange and
// mint accents. Palette from Ahmad Awais's Shades of Purple
// (https://github.com/ahmadawais/shades-of-purple-vscode).
//
//   surfaces #1e1e3f #28284e #2d2b55 #3b3a6e  selection #4d21fc  line numbers #a599e9
//   text #ffffff #e1efff  yellow #fad000  orange #ff9d00  pink #ff628c  orchid #fb94ff
//   mint #a5ff90  cyan #9effff  comment #b362ff
//
// Written as #rrggbb so it looks the same in every terminal.

Theme {
    app {
        window: "#1e1e3f"; windowText: "#e1efff"
        button: "#3b3a6e"; buttonText: "#ffffff"
        highlight: "#fad000"; highlightedText: "#1e1e3f"
        base: "#2d2b55"; text: "#ffffff"
        inactive { highlight: "#3b3a6e"; highlightedText: "#a599e9" }
        mid: "#4d4b88"; light: "#a599e9"
        backdrop: "#1e1e3f"
    }
    menu {
        window: "#1e1e3f"; windowText: "#e1efff"
        highlight: "#fad000"; highlightedText: "#1e1e3f"
        accent: "#ff9d00"
    }
    document {
        window: "#2d2b55"; windowText: "#e1efff"
        highlight: "#fad000"; highlightedText: "#1e1e3f"
        base: "#2d2b55"; text: "#ffffff"
        selection: "#4d21fc"; selectedText: "#ffffff"
        cursor: "#fad000"; lineNumber: "#a599e9"
    }
    status {
        window: "#1e1e3f"; windowText: "#a599e9"
    }
    syntax {
        keyword: "#ff9d00"; controlFlow: "#ff9d00"
        dataType: "#9effff"; attribute: "#fad000"; function: "#fad000"
        string: "#a5ff90"; specialChar: "#fb94ff"
        decVal: "#ff628c"; float: "#ff628c"; baseN: "#ff628c"; constant: "#ff628c"
        comment: "#b362ff"; alert: "#ff628c"
        import: "#ff9d00"; operator: "#ff9d00"
    }
}
