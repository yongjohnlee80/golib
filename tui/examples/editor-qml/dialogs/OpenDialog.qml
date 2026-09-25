// OpenDialog.qml — chooses a file to open, with a preview of it.
//
// Qt's FileDialog. Its layout is the file browser's: the folder on the left,
// a preview of the file under the cursor on the right, Cancel and Open
// beneath. Tab moves between the parts — into the preview to read the whole
// file — and the footer says what the keys do in the one in use.
// Opening a folder is not a choice: Enter on one goes into it.
//
// A COMPONENT, like the other dialogs: it imports nothing, so the layout's
// theme dresses it.

FileDialog {
    title: "Open"
    fileMode: Tui.OpenFile
    currentFolder: App.folder
    palette.window: Theme.dialog.window
    palette.windowText: Theme.dialog.windowText
    palette.button: Theme.dialog.button
    palette.buttonText: Theme.dialog.buttonText
    palette.highlight: Theme.dialog.highlight
    palette.highlightedText: Theme.dialog.highlightedText
    palette.base: Theme.dialog.base
    palette.text: Theme.dialog.text
    palette.inactive.highlight: Theme.dialog.inactive.highlight
    palette.inactive.highlightedText: Theme.dialog.inactive.highlightedText
    palette.mid: Theme.dialog.mid
    palette.light: Theme.dialog.light
    onAccepted: App.openFile(selectedFile)
}
