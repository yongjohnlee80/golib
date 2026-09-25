// SaveDialog.qml — names the file to write, for a buffer that has no name yet.
//
// Qt's FileDialog. Its layout is the file browser's: the name to save as on
// top, the folder's files below, Cancel and Save beneath. Tab moves between
// the parts, and the footer says what the keys do in the one in use.
// Opening a folder is not a choice: Enter on one goes into it.
//
// A COMPONENT, like the other dialogs: it imports nothing, so the layout's
// theme dresses it.

FileDialog {
    title: "Save"
    fileMode: Tui.SaveFile
    currentFolder: App.folder
    dim: false          // the editor stays in view behind it, as behind Quit
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
    onAccepted: App.saveAs(selectedFile)
}
