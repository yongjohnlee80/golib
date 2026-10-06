// QuitDialog.qml — asks before the editor ends.
//
// A COMPONENT: this file defines the type QuitDialog, which editor.qml uses as
// `QuitDialog { id: quitDialog }` once it imports editor.dialogs. It imports
// nothing itself. Its names — App, Theme, Dialog, Tui — resolve in the document
// that uses it, which is why switching the layout's theme import re-dresses
// this dialog too.
//
// Nothing here says how the dialog closes. Yes, No, their underlined letters
// and Escape all close it; the file says only what an answer does.

Dialog {
    title: qsTrId("editor.quit.title")
    standardButtons: Dialog.Yes | Dialog.No    // y and n answer it
    // The editor stays in view behind the question, undimmed: the user is
    // deciding whether to leave THAT, so it should be what they see.
    dim: false
    onAccepted: App.quit()

    // Bound: it mentions unsaved changes when there are some. The host
    // publishes the question as a message id, not words, so it follows a
    // language switch with nothing republished. It names no colour: a palette
    // propagates, so the question wears the card's. Yes and No are golib's
    // own words, translated with golib.
    Text {
        text: App.quitQuestion
        wrapMode: Tui.WordWrap
    }
}
