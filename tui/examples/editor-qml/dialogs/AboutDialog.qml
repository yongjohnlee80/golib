// AboutDialog.qml — what this program is.
//
// A COMPONENT, like QuitDialog.qml: the type AboutDialog, used by editor.qml,
// importing nothing of its own. Enter or Escape closes it, as its help line
// says.

Dialog {
    title: qsTrId("editor.about.title")
    standardButtons: Dialog.Ok
    defaultButton: Dialog.Ok               // Enter closes it
    helpText: qsTrId("editor.about.help")

    // On the card's colours, which it inherits — no palette of its own.
    Text {
        wrapMode: Tui.WordWrap
        text: qsTrId("editor.about.text")
    }
}
