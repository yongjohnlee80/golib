// TilePanel.qml — the tiled panel: a Tile of a TileManager, framed under its heading, as Panel.qml
// frames a Drawer. The manager owns its size: it shares the screen and its borders with the other
// tiles (Alt/Option-right drag moves the nearest shared border, Alt/Option-left drag re-docks or
// swaps it), and a dock hides and shows it: the program needs WithWindowCollector. A use gives its
// edge, the edges it may move to, its size, its content and the Shortcuts live while the keyboard is
// in it.
Tile {
    property var heading: "" // a string, or a qsTrId message: Frame.title takes either
    // the frame's colour: the page's, as the lists in it are
    property var surface: Theme.document.base
    default property alias content: scope.data
    // the key-map scope of the use's keys, which its FocusScope holds
    property string paneKeys: ""
    minimizable: true // the centre tile never hides, whatever this says
    Frame {
        title: heading
        palette.window: surface
        FocusScope { id: scope; keyScope: paneKeys }
    }
}
