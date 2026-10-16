// tokyo_night.qml — Tokyo Night (night): deep navy surfaces, neon pastel accents. Palette from
// folke/tokyonight.nvim (the night style).
//
//   bg_dark #16161e  bg #1a1b26  bg_highlight #292e42  bg_visual #283457
//   terminal_black #414868  fg_gutter #3b4261  dark3 #545c7e  comment #565f89  dark5 #737aa2
//   fg_dark #a9b1d6  fg #c0caf5
//   blue #7aa2f7  cyan #7dcfff  blue1 #2ac3de  blue5 #89ddff  blue6 #b4f9f8
//   magenta #bb9af7  purple #9d7cd8  orange #ff9e64  yellow #e0af68
//   green #9ece6a  green1 #73daca  teal #1abc9c  red #f7768e  red1 #db4b4b
//
// Written as #rrggbb so it looks the same in every terminal.

Theme {
    app {
        window: "#16161e"; windowText: "#c0caf5"
        button: "#292e42"; buttonText: "#c0caf5"
        highlight: "#7aa2f7"; highlightedText: "#16161e"
        base: "#1a1b26"; text: "#c0caf5"
        inactive { highlight: "#292e42"; highlightedText: "#a9b1d6" }
        mid: "#414868"; light: "#a9b1d6"
        backdrop: "#16161e"
    }
    menu {
        window: "#16161e"; windowText: "#c0caf5"
        highlight: "#7aa2f7"; highlightedText: "#16161e"
        accent: "#ff9e64"
    }
    document {
        window: "#1a1b26"; windowText: "#a9b1d6"
        highlight: "#bb9af7"; highlightedText: "#16161e"
        base: "#1a1b26"; text: "#c0caf5"
        selection: "#283457"; selectedText: "#c0caf5"
        cursor: "#c0caf5"; lineNumber: "#737aa2"
    }
    status {
        window: "#16161e"; windowText: "#a9b1d6"
    }
    syntax {
        keyword: "#9d7cd8"; controlFlow: "#bb9af7"
        dataType: "#2ac3de"; attribute: "#73daca"; function: "#7aa2f7"
        string: "#9ece6a"; specialChar: "#bb9af7"
        decVal: "#ff9e64"; float: "#ff9e64"; baseN: "#ff9e64"; constant: "#ff9e64"
        comment: "#565f89"; alert: "#f7768e"
        import: "#7dcfff"; operator: "#89ddff"
    }
}
