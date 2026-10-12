// Panel.qml — a dock style: one consumer's look for a docked panel, written once and used for each
// of its panels. A Drawer from the edge a use names, framed under its heading; the use's content
// and its Shortcuts go into the FocusScope, so those keys are live while focus is in the panel and
// win there over the Window's.
Drawer {
    property string heading: ""
    default property alias content: scope.data
    modal: false
    closeOnQ: true
    size: 32
    Frame {
        title: heading
        FocusScope { id: scope }
    }
}
