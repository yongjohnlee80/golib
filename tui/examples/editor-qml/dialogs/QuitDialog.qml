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
    title: "Quit"
    standardButtons: Dialog.Yes | Dialog.No    // y and n answer it
    // The editor stays in view behind the question, undimmed: the user is
    // deciding whether to leave THAT, so it should be what they see.
    dim: false
    onAccepted: App.quit()

    // Bound: it mentions unsaved changes when there are some. It names no
    // colour: a palette propagates, so the question wears the card's.
    Text {
        text: App.quitQuestion
        wrapMode: Tui.WordWrap
    }
}
