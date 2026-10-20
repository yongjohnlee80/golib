// Panel.qml — the docked panel: a Drawer over the page, framed under its heading, floating where it
// is dragged or arranged (Alt/Option-left drag moves it, Alt/Option-right drag resizes it) and until
// then at the edge its use names. A use gives its id, its edge and geometry with the preferences they
// keep (tui/panelgeometry), its content, and the Shortcuts live while the keyboard is in the panel:
// the content and its keys go into the FocusScope together, and its keys win there over the Window's.
//
// It is non-modal, so it never traps focus or the pointer (a Dialog does), and minimizable, so a
// program's dock can hide it without closing it: the program needs WithWindowCollector.
Drawer {
    property string heading: ""
    // the frame's colour: the page's, as the lists in it are (a frame's own window tone would show
    // as a band beside its border, which the GUI draws as a thin line through the cell)
    property var surface: Theme.document.base
    default property alias content: scope.data
    // the key-map scope of the use's keys, which its FocusScope holds: "autodoc.explorer"
    property string paneKeys: ""
    modal: false
    minimizable: true
    movable: true
    windowResize: true
    Frame {
        title: heading
        palette.window: surface
        FocusScope { id: scope; keyScope: paneKeys }
    }
}
