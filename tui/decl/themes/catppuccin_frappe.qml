// catppuccin_frappe.qml — Catppuccin Frappé: muted blue-grey surfaces, pastel
// accents. Palette from https://catppuccin.com/palette (Frappé flavour).
//
//   crust #232634  mantle #292c3c  base #303446
//   surface0 #414559  surface1 #51576d  surface2 #626880
//   overlay0 #737994  overlay1 #838ba7  overlay2 #949cbb
//   subtext0 #a5adce  subtext1 #b5bfe2  text #c6d0f5
//   rosewater #f2d5cf  flamingo #eebebe  pink #f4b8e4  mauve #ca9ee6
//   red #e78284  maroon #ea999c  peach #ef9f76  yellow #e5c890
//   green #a6d189  teal #81c8be  sky #99d1db  sapphire #85c1dc
//   blue #8caaee  lavender #babbf1
//
// Written as #rrggbb so it looks the same in every terminal.

Theme {
    app {
        window: "#292c3c"; windowText: "#c6d0f5"
        button: "#414559"; buttonText: "#c6d0f5"
        highlight: "#8caaee"; highlightedText: "#232634"
        base: "#303446"; text: "#c6d0f5"
        inactive { highlight: "#414559"; highlightedText: "#babbf1" }
        mid: "#51576d"; light: "#babbf1"
        backdrop: "#232634"
    }
    menu {
        window: "#292c3c"; windowText: "#c6d0f5"
        highlight: "#8caaee"; highlightedText: "#232634"
        accent: "#ef9f76"
    }
    document {
        window: "#303446"; windowText: "#a5adce"
        highlight: "#babbf1"; highlightedText: "#232634"
        base: "#303446"; text: "#c6d0f5"
        selection: "#414559"; selectedText: "#c6d0f5"
        cursor: "#f2d5cf"; lineNumber: "#838ba7"
    }
    status {
        window: "#292c3c"; windowText: "#b5bfe2"
    }
    syntax {
        keyword: "#ca9ee6"; controlFlow: "#ca9ee6"
        dataType: "#e5c890"; attribute: "#e5c890"; function: "#8caaee"
        string: "#a6d189"; specialChar: "#f4b8e4"
        decVal: "#ef9f76"; float: "#ef9f76"; baseN: "#ef9f76"; constant: "#ef9f76"
        comment: "#949cbb"; alert: "#e78284"
        import: "#81c8be"; operator: "#99d1db"
    }
}
