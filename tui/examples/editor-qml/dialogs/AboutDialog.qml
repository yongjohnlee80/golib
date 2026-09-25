// AboutDialog.qml — what this program is.
//
// A COMPONENT, like QuitDialog.qml: the type AboutDialog, used by editor.qml,
// importing nothing of its own. Enter or Escape closes it, as its help line
// says.

Dialog {
    title: "About"
    standardButtons: Dialog.Ok
    defaultButton: Dialog.Ok               // Enter closes it
    helpText: "Enter or Esc to close"

    // On the card's colours, which it inherits — no palette of its own.
    Text {
        wrapMode: Tui.WordWrap
        text: "editor-qml\n\nA text editor whose screen is written in QML,\nrunning on golib/tui."
    }
}
