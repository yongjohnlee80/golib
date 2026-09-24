// editor.qml — the editor's LAYOUT.
//
// This file describes structure only: what is on the screen, where it is
// docked, and what each control does when used. It names no colour. How the
// editor LOOKS is a separate concern, in its own importable module, so the
// structure here and the theme can change independently.
//
// It mirrors github.com/yongjohnlee80/editor, which builds the same screen in
// Go: a menu bar, a boxed vim-style editor, and a three-part status line.

import tui 1.0      // the widget vocabulary, and the Tui singleton's enums
import editor 1.0   // the App singleton: this program's state and commands

Window {
    // ---- keys -----------------------------------------------------------
    //
    // Qt's own Shortcut type. They fire whichever widget holds focus, because
    // the Window sees every key the focused widget does not consume. The menu
    // is reachable from the keyboard too: F10 goes to the bar and Alt plus a
    // category's underlined letter opens it — Alt+F for File.
    Shortcut { sequence: "Ctrl+Q"; onActivated: App.exit() }
    Shortcut { sequence: "Ctrl+S"; onActivated: App.saveFile() }

    // ---- the menu bar ---------------------------------------------------
    //
    // `Dock.edge` is an ATTACHED property: written here, read by the Window
    // that docks this bar. Change Tui.Top to Tui.Bottom and the bar moves and
    // its dropdowns open upwards; nothing else in the file changes.
    MenuBar {
        Dock.edge: Tui.Top
        vimNavigation: true

        // `&` marks a mnemonic, exactly as in Qt: "&File" is the label File
        // with F as its hotkey, and "E&xit" underlines the x.
        Menu {
            title: "&File"
            MenuItem { text: "&New";  onTriggered: App.newFile() }
            MenuItem { text: "&Open"; onTriggered: App.openFile() }
            MenuItem { text: "&Save"; onTriggered: App.saveFile() }
            MenuItem { text: "E&xit"; onTriggered: App.exit() }
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
            MenuItem { text: "&About"; onTriggered: App.about() }
        }
    }

    // ---- the document ---------------------------------------------------
    //
    // No Dock.edge, so it fills whatever the bars leave.
    Frame {
        Editor {
            id: editor
            focus: true
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
        left: App.mode
        center: App.status
        right: App.clock
    }
}
