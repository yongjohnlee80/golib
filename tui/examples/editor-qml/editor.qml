// editor.qml — the editor's LAYOUT.
//
// This file describes structure only: what is on the screen, where it is
// docked, and what each control does when used. It names no colour. How the
// editor LOOKS is a theme module — themes/mono.qml or themes/retro.qml — and
// the widgets here bind their palette roles to whichever one is imported.
// Switching theme is the third import line and nothing else.
//
// It mirrors github.com/yongjohnlee80/editor, which builds the same screen in
// Go: a menu bar, a boxed vim-style editor, and a three-part status line.

import tui 1.0      // the widget vocabulary, and the Tui singleton's enums
import editor 1.0   // the App singleton: this program's state and commands
import editor.theme.retro 1.0   // the Theme singleton — or editor.theme.mono
import editor.dialogs 1.0       // QuitDialog and AboutDialog, one file each

Window {
    // ---- keys -----------------------------------------------------------
    //
    // Qt's own Shortcut type. They fire whichever widget holds focus, because
    // the Window sees every key the focused widget does not consume. The menu
    // is reachable from the keyboard too: F10 goes to the bar and Alt plus a
    // category's underlined letter opens it — Alt+F for File.
    Shortcut { sequence: "Ctrl+Q"; onActivated: quitDialog.open() }
    Shortcut { sequence: "Ctrl+S"; onActivated: App.saveFile() }

    // ---- the menu bar ---------------------------------------------------
    //
    // `Dock.edge` is an ATTACHED property: written here, read by the Window
    // that docks this bar. Change Tui.Top to Tui.Bottom and the bar moves and
    // its dropdowns open upwards; nothing else in the file changes.
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
            MenuItem { text: "&Open"; onTriggered: App.openFile() }
            MenuItem { text: "&Save"; onTriggered: App.saveFile() }
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
    // No Dock.edge, so it fills whatever the bars leave.
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

    // ---- dialogs --------------------------------------------------------
    //
    // Each is its own file under dialogs/, a type named for the file, brought
    // in by `import editor.dialogs`. They open by id from a handler —
    // quitDialog.open() — and close themselves.
    QuitDialog { id: quitDialog }
    AboutDialog { id: aboutDialog }
}
