// SaveDialog.qml — names the file to write: for a buffer that has no name yet,
// and for File > Save As.
//
// Qt's FileDialog. Its layout is the file browser's: the name to save as on
// top, the folder's files below, Cancel and Save beneath. Tab moves between
// the parts, and the footer says what the keys do in the one in use.
// Opening a folder is not a choice: Enter on one goes into it.
//
// A COMPONENT, like the other dialogs: it imports nothing and names no
// colour — the card, its panes and its buttons all inherit the Window's
// application palette, so the layout's theme import dresses it.

FileDialog {
    title: "Save"
    fileMode: Tui.SaveFile
    currentFolder: App.folder
    // Qt 6's writable selectedFile: Save As starts from the file being
    // edited — its folder listed, its name in the field, ready to change.
    selectedFile: App.path
    dim: false          // the editor stays in view behind it, as behind Quit
    onAccepted: App.saveAs(selectedFile)
    onRejected: App.saveCancelled()
}
