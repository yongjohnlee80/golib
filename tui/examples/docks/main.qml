import tui 1.0
import demo 1.0
import demo.docks 1.0

// Two panels from one dock style, each with its own d. While focus is in a panel its d is the
// panel's; anywhere else d is the Window's. The page's n is live only while focus is on the page.
Window {
    Shortcut { sequence: "d"; onActivated: App.say("d: the Window's, focus is on the page") }
    Shortcut { sequence: "F2"; onActivated: files.toggle() }
    Shortcut { sequence: "F3"; onActivated: notes.toggle() }
    Shortcut { sequence: "Ctrl+Q"; onActivated: App.quit() }

    Flex {
        direction: Tui.Vertical
        Text { text: "F2 files, F3 notes, d anywhere, n on the page, Esc or q closes a panel, Ctrl+Q quits" }
        FocusScope {
            Layout.fillHeight: true
            Shortcut { sequence: "n"; onActivated: App.say("n: the page's own") }
            Button { id: page; text: "the page"; focus: true }
        }
        Text { text: App.status }
    }

    Panel {
        id: files
        heading: "files"
        edge: Tui.Left
        onOpened: fileList.forceActiveFocus()
        Shortcut { sequence: "d"; onActivated: App.say("d: delete, in the files panel") }
        ListView { id: fileList; model: App.files; textRole: "name" }
    }
    Panel {
        id: notes
        heading: "notes"
        edge: Tui.Right
        onOpened: noteList.forceActiveFocus()
        Shortcut { sequence: "d"; onActivated: App.say("d: done, in the notes panel") }
        ListView { id: noteList; model: App.notes; textRole: "name" }
    }
}
