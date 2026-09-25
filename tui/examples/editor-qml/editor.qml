// editor.qml — the editor's LAYOUT.
//
// This file describes structure only: what is on the screen, where it is
// docked, and what each control does when used. It names no colour. How the
// editor LOOKS is a theme module — themes/mono.qml or themes/retro.qml — and
// the widgets here bind their palette roles to whichever one is imported.
// Switching theme is the third import line and nothing else.
//
// A palette is set ONCE, where it starts. Roles propagate from parent to
// child, as Qt's do: the Window carries the application palette, a surface
// that is a distinct part of the design (the menu bar, the document, the
// status line) overrides only its own roles, and everything else — every
// dialog, its panes and buttons, the command prompt — inherits and names no
// colour at all.
//
// It mirrors github.com/yongjohnlee80/editor, which builds the same screen in
// Go: a menu bar, a boxed vim-style editor, and a three-part status line.

import tui 1.0      // the widget vocabulary, and the Tui singleton's enums
import editor 1.0   // the App singleton: this program's state and commands
import editor.theme.retro 1.0   // the Theme singleton — or editor.theme.mono
import editor.dialogs 1.0       // QuitDialog, AboutDialog, OpenDialog, SaveDialog

Window {
    // ---- the application palette ----------------------------------------
    palette.window: Theme.app.window
    palette.windowText: Theme.app.windowText
    palette.button: Theme.app.button
    palette.buttonText: Theme.app.buttonText
    palette.highlight: Theme.app.highlight
    palette.highlightedText: Theme.app.highlightedText
    palette.base: Theme.app.base
    palette.text: Theme.app.text
    palette.inactive.highlight: Theme.app.inactive.highlight
    palette.inactive.highlightedText: Theme.app.inactive.highlightedText
    palette.mid: Theme.app.mid
    palette.light: Theme.app.light

    // ---- keys -----------------------------------------------------------
    //
    // Qt's own Shortcut type. They fire whichever widget holds focus, because
    // the Window sees every key the focused widget does not consume. The menu
    // is reachable from the keyboard too: F10 goes to the bar and Alt plus a
    // category's underlined letter opens it — Alt+F for File.
    Shortcut { sequence: "Ctrl+Q"; onActivated: quitDialog.open() }
    Shortcut { sequence: "Ctrl+S"; onActivated: App.saveFile() }
    // Where the terminal can report Shift with Ctrl — the kitty keyboard
    // protocol. Elsewhere it arrives as Ctrl+S, and the menu is the way in.
    Shortcut { sequence: "Ctrl+Shift+S"; onActivated: saveDialog.open() }
    // The command prompt, as vim's `:` is — on a key the editor does not
    // type, since a Shortcut fires whatever mode the editor is in.
    Shortcut { sequence: "Ctrl+P"; onActivated: prompt.open() }

    // ---- the menu bar ---------------------------------------------------
    //
    // `Dock.edge` is an ATTACHED property: written here, read by the Window
    // that docks this bar. Change Tui.Top to Tui.Bottom and the bar moves and
    // its dropdowns open upwards; nothing else in the file changes.
    //
    // It overrides the roles it wears: a menu bar is its own strip of colour.
    MenuBar {
        Dock.edge: Tui.Top
        vimNavigation: true
        palette.window: Theme.menu.window
        palette.windowText: Theme.menu.windowText
        palette.highlight: Theme.menu.highlight
        palette.highlightedText: Theme.menu.highlightedText
        palette.accent: Theme.menu.accent       // the access-key letter

        // `&` marks a mnemonic, exactly as in Qt: "&File" is the label File
        // with F as its hotkey, and "E&xit" underlines the x.
        Menu {
            title: "&File"
            MenuItem { text: "&New";  onTriggered: App.newFile() }
            MenuItem { text: "&Open"; onTriggered: openDialog.open() }
            MenuItem { text: "&Save"; onTriggered: App.saveFile() }
            // Save As ALWAYS asks, so the document opens the dialog itself.
            MenuItem { text: "Save &As…"; onTriggered: saveDialog.open() }
            MenuItem { text: "&Command…"; onTriggered: prompt.open() }
            MenuItem { text: "E&xit"; onTriggered: quitDialog.open() }
        }

        Menu {
            title: "&Option"
            Menu {
                title: "&Keymaps"
                // A shared `group` makes these a radio set: choosing one
                // clears the other.
                MenuItem {
                    text: "&1. Vim  (modal)"
                    group: "keyset"
                    checked: true
                    onTriggered: App.useVim()
                }
                MenuItem {
                    text: "&2. Nano (modeless)"
                    group: "keyset"
                    onTriggered: App.useNano()
                }
            }
        }

        // Help sits at the far end of the bar, where it has sat in this kind
        // of application for thirty years.
        Menu {
            title: "&Help"
            align: Tui.Right
            MenuItem { text: "&About"; onTriggered: aboutDialog.open() }
        }
    }

    // ---- the document ---------------------------------------------------
    //
    // No Dock.edge, so it fills whatever the bars leave. The frame and the
    // text inside it are the document's colours, not the application's, so
    // both say so.
    Frame {
        palette.window: Theme.frame.window
        palette.windowText: Theme.frame.windowText
        palette.highlight: Theme.frame.highlight  // the border while focused

        Editor {
            id: editor
            focus: true
            palette.base: Theme.editor.base
            palette.text: Theme.editor.text
            palette.highlight: Theme.editor.highlight
            palette.highlightedText: Theme.editor.highlightedText
            // BOUND to a source: choosing a keymap in the menu changes
            // App.keyset, and the editor follows without the host reaching in.
            keyset: App.keyset
            onModeChanged: App.syncStatus()
            onTextChanged: App.markDirty()

            // KDE KSyntaxHighlighting's type: Go highlights, the document
            // names the definition, the theme colours it. The host says which
            // definition fits the file — QML for a .qml file, none otherwise.
            SyntaxHighlighter {
                definition: App.syntax
                theme.keyword: Theme.syntax.keyword
                theme.controlFlow: Theme.syntax.controlFlow
                theme.dataType: Theme.syntax.dataType
                theme.attribute: Theme.syntax.attribute
                theme.function: Theme.syntax.function
                theme.string: Theme.syntax.string
                theme.specialChar: Theme.syntax.specialChar
                theme.decVal: Theme.syntax.decVal
                theme.float: Theme.syntax.float
                theme.baseN: Theme.syntax.baseN
                theme.constant: Theme.syntax.constant
                theme.comment: Theme.syntax.comment
                theme.alert: Theme.syntax.alert
                theme.import: Theme.syntax.import
                theme.operator: Theme.syntax.operator
            }
        }
    }

    // ---- the status line ------------------------------------------------
    StatusBar {
        Dock.edge: Tui.Bottom
        palette.window: Theme.status.window
        palette.windowText: Theme.status.windowText
        left: App.mode
        center: App.status
        right: App.clock
    }

    // ---- the command prompt ---------------------------------------------
    //
    // Qt Quick Controls' Popup and TextField (golib's tui/decl/controls): a
    // modal Popup takes the keyboard while open and gives it back when
    // closed; Escape closes it. It names no colour — the frame wears the
    // application palette, the field its panes' base, both inherited.
    Popup {
        id: prompt
        modal: true
        onOpened: command.clear()
        Frame {
            title: "Command"
            TextField {
                id: command
                placeholderText: "w  q  wq  e <file>"
                onAccepted: App.runCommand(text)
            }
        }
    }

    // ---- dialogs --------------------------------------------------------
    //
    // Each is its own file under dialogs/, a type named for the file, brought
    // in by `import editor.dialogs`. They open by id from a handler —
    // quitDialog.open() — and close themselves. None names a colour: each
    // inherits the Window's application palette.
    QuitDialog { id: quitDialog }
    AboutDialog { id: aboutDialog }
    OpenDialog { id: openDialog }
    // Opened by File > Save As, and by the HOST when Save finds the buffer has
    // no name — whether it has one is the host's to know.
    SaveDialog { id: saveDialog }
}
