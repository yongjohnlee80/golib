// nord.qml — Nord: an arctic, north-bluish palette, its Polar Night surfaces with Snow Storm
// text and Frost and Aurora accents. Palette from https://www.nordtheme.com/docs/colors-and-palettes.
//
//   Polar Night  nord0 #2e3440  nord1 #3b4252  nord2 #434c5e  nord3 #4c566a
//   Snow Storm   nord4 #d8dee9  nord5 #e5e9f0  nord6 #eceff4
//   Frost        nord7 #8fbcbb  nord8 #88c0d0  nord9 #81a1c1  nord10 #5e81ac
//   Aurora       nord11 #bf616a  nord12 #d08770  nord13 #ebcb8b  nord14 #a3be8c  nord15 #b48ead
//
// Two colours are not among the sixteen, on purpose: the backdrop #242933, a shade below nord0
// so the screen around the page stays apart from it, and comments #616e88, the brightened
// nord3 Nord's own editor ports use, since nord3 itself is too faint on nord0.
//
// Written as #rrggbb so it looks the same in every terminal.

Theme {
    app {
        window: "#3b4252"; windowText: "#e5e9f0"
        button: "#434c5e"; buttonText: "#e5e9f0"
        highlight: "#88c0d0"; highlightedText: "#2e3440"
        base: "#2e3440"; text: "#d8dee9"
        inactive { highlight: "#434c5e"; highlightedText: "#d8dee9" }
        mid: "#4c566a"; light: "#d8dee9"
        backdrop: "#242933"
    }
    menu {
        window: "#3b4252"; windowText: "#e5e9f0"
        highlight: "#88c0d0"; highlightedText: "#2e3440"
        accent: "#ebcb8b"
    }
    document {
        window: "#2e3440"; windowText: "#d8dee9"
        highlight: "#81a1c1"; highlightedText: "#2e3440"
        base: "#2e3440"; text: "#d8dee9"
        selection: "#434c5e"; selectedText: "#eceff4"
        cursor: "#d8dee9"; lineNumber: "#616e88"
    }
    status {
        window: "#3b4252"; windowText: "#d8dee9"
    }
    syntax {
        keyword: "#81a1c1"; controlFlow: "#81a1c1"
        dataType: "#8fbcbb"; attribute: "#d08770"; function: "#88c0d0"
        string: "#a3be8c"; specialChar: "#ebcb8b"
        decVal: "#b48ead"; float: "#b48ead"; baseN: "#b48ead"; constant: "#b48ead"
        comment: "#616e88"; alert: "#bf616a"
        import: "#5e81ac"; operator: "#81a1c1"
    }
}
