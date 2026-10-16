// kanagawa.qml — Kanagawa (wave): sumi-ink surfaces and the muted colours of Hokusai's Great
// Wave. Palette from rebelot/kanagawa.nvim (the wave theme).
//
//   sumiInk0 #16161d  sumiInk3 #1f1f28  sumiInk4 #2a2a37  sumiInk5 #363646  sumiInk6 #54546d
//   waveBlue1 #223249  waveBlue2 #2d4f67  fujiGray #727169  oldWhite #c8c093  fujiWhite #dcd7ba
//   oniViolet #957fb8  crystalBlue #7e9cd8  springBlue #7fb4ca  waveAqua2 #7aa89f
//   springGreen #98bb6c  boatYellow2 #c0a36e  carpYellow #e6c384  sakuraPink #d27e99
//   waveRed #e46876  peachRed #ff5d62  surimiOrange #ffa066  roninYellow #ff9e3b
//
// Written as #rrggbb so it looks the same in every terminal.

Theme {
    app {
        window: "#16161d"; windowText: "#dcd7ba"
        button: "#2a2a37"; buttonText: "#dcd7ba"
        highlight: "#7e9cd8"; highlightedText: "#16161d"
        base: "#1f1f28"; text: "#dcd7ba"
        inactive { highlight: "#2a2a37"; highlightedText: "#c8c093" }
        mid: "#54546d"; light: "#c8c093"
        backdrop: "#16161d"
    }
    menu {
        window: "#16161d"; windowText: "#dcd7ba"
        highlight: "#7e9cd8"; highlightedText: "#16161d"
        accent: "#ffa066"
    }
    document {
        window: "#1f1f28"; windowText: "#c8c093"
        highlight: "#957fb8"; highlightedText: "#16161d"
        base: "#1f1f28"; text: "#dcd7ba"
        selection: "#2d4f67"; selectedText: "#dcd7ba"
        cursor: "#c8c093"; lineNumber: "#727169"
    }
    status {
        window: "#16161d"; windowText: "#c8c093"
    }
    syntax {
        keyword: "#957fb8"; controlFlow: "#957fb8"
        dataType: "#7aa89f"; attribute: "#e6c384"; function: "#7e9cd8"
        string: "#98bb6c"; specialChar: "#7fb4ca"
        decVal: "#d27e99"; float: "#d27e99"; baseN: "#d27e99"; constant: "#ffa066"
        comment: "#727169"; alert: "#e46876"
        import: "#e46876"; operator: "#c0a36e"
    }
}
