// OpenDialog.qml — chooses a file to open, with a preview of it.
//
// Qt's FileDialog. Its layout is the file browser's: the folder on the left,
// a preview of the file under the cursor on the right, Cancel and Open
// beneath. Tab moves between the parts — into the preview to read the whole
// file — and the footer says what the keys do in the one in use.
// Opening a folder is not a choice: Enter on one goes into it.
//
// A COMPONENT, like the other dialogs: it imports nothing and names no
// colour — the card, its panes and its buttons all inherit the Window's
// application palette, so the layout's theme import dresses it.

FileDialog {
    title: "Open"
    fileMode: Tui.OpenFile
    currentFolder: App.folder
    dim: false          // the editor stays in view behind it, as behind Quit
    onAccepted: App.openFile(selectedFile)
}
