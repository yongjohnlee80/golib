package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// KEY COMMANDS — every key a golib widget acts on, declared as data. A widget
// asks its Context which of its own commands a key is (tui.Context.KeyCommand); a key-mapping scheme
// changes their keys by id. Each widget's defaults reproduce the keys its handler took before
// commands existed, written out key by key: where the old handler let Shift through to a functional
// key, the Shift form is a default too.
//
// A label's mnemonic (&File, a button's underlined letter) is not a command and cannot be rebound.
//
// This file declares core's widgets' commands, through internal/kit's Cmd and the helpers commands
// are written with (With, Mods, AnyCombo). A component's own are in its <component>_commands.go
// (table_commands.go, terminal_commands.go, …).

// ListScope is a List's keys.
const ListScope keymap.Scope = "tui.list"

var (
	ListUp       = kit.Cmd("tui.list.up", ListScope, append(kit.With("Up"), "K")...)
	ListDown     = kit.Cmd("tui.list.down", ListScope, append(kit.With("Down"), "J")...)
	ListTop      = kit.Cmd("tui.list.top", ListScope, "G")
	ListBottom   = kit.Cmd("tui.list.bottom", ListScope, "Shift+G")
	ListPageUp   = kit.Cmd("tui.list.pageUp", ListScope, kit.With("PgUp")...)
	ListPageDown = kit.Cmd("tui.list.pageDown", ListScope, kit.With("PgDown")...)
	ListHome     = kit.Cmd("tui.list.home", ListScope, kit.With("Home")...)
	ListEnd      = kit.Cmd("tui.list.end", ListScope, kit.With("End")...)
	ListActivate = kit.Cmd("tui.list.activate", ListScope, kit.With("Enter")...)
	ListToggle   = kit.Cmd("tui.list.toggle", ListScope, "Space") // multi-select only
	ListMenu     = kit.Cmd("tui.list.contextMenu", ListScope, "Shift+F10", "Menu")

	listCommands = []keymap.Command{ListMenu, ListUp, ListDown, ListTop, ListBottom, ListPageUp,
		ListPageDown, ListHome, ListEnd, ListActivate, ListToggle}
)

// InputScope is a TextInput's keys.
const InputScope keymap.Scope = "tui.input"

// chords is key under every combination of Shift, Ctrl or Alt (the word modifier), and Super, kept
// where keep says so: a TextInput reads Shift as "extend the selection" and Ctrl or Alt as "by
// word", and its old handler ignored Super.
func chords(key string, keep func(shift, word bool) bool) []string {
	var out []string
	for _, shift := range []string{"", "Shift+"} {
		for _, w := range []string{"", "Ctrl+", "Alt+", "Ctrl+Alt+"} {
			if !keep(shift != "", w != "") {
				continue
			}
			for _, super := range []string{"", "Super+"} {
				out = append(out, w+super+shift+key)
			}
		}
	}
	return out
}

var (
	plain     = func(shift, word bool) bool { return !shift && !word }
	shifted   = func(shift, word bool) bool { return shift && !word }
	worded    = func(shift, word bool) bool { return !shift && word }
	both      = func(shift, word bool) bool { return shift && word }
	unshifted = func(shift, _ bool) bool { return !shift }
	anyShift  = func(shift, _ bool) bool { return shift }
	unworded  = func(_, word bool) bool { return !word }
	anyWord   = func(_, word bool) bool { return word }
	always    = func(bool, bool) bool { return true }

	InputLeft            = kit.Cmd("tui.input.left", InputScope, chords("Left", plain)...)
	InputSelectLeft      = kit.Cmd("tui.input.selectLeft", InputScope, chords("Left", shifted)...)
	InputWordLeft        = kit.Cmd("tui.input.wordLeft", InputScope, chords("Left", worded)...)
	InputSelectWordLeft  = kit.Cmd("tui.input.selectWordLeft", InputScope, chords("Left", both)...)
	InputRight           = kit.Cmd("tui.input.right", InputScope, chords("Right", plain)...)
	InputSelectRight     = kit.Cmd("tui.input.selectRight", InputScope, chords("Right", shifted)...)
	InputWordRight       = kit.Cmd("tui.input.wordRight", InputScope, chords("Right", worded)...)
	InputSelectWordRight = kit.Cmd("tui.input.selectWordRight", InputScope, chords("Right", both)...)
	InputHome            = kit.Cmd("tui.input.home", InputScope, chords("Home", unshifted)...)
	InputSelectHome      = kit.Cmd("tui.input.selectHome", InputScope, chords("Home", anyShift)...)
	InputEnd             = kit.Cmd("tui.input.end", InputScope, chords("End", unshifted)...)
	InputSelectEnd       = kit.Cmd("tui.input.selectEnd", InputScope, chords("End", anyShift)...)
	InputBackspace       = kit.Cmd("tui.input.backspace", InputScope, chords("Backspace", unworded)...)
	InputWordBackspace   = kit.Cmd("tui.input.wordBackspace", InputScope, chords("Backspace", anyWord)...)
	InputDelete          = kit.Cmd("tui.input.delete", InputScope, chords("Del", always)...)
	InputSubmit          = kit.Cmd("tui.input.submit", InputScope, chords("Enter", always)...)
	InputComplete        = kit.Cmd("tui.input.complete", InputScope, "Tab")           // with a completer
	InputCompletePrev    = kit.Cmd("tui.input.completePrev", InputScope, "Shift+Tab") // with a completer
	InputLineStart       = kit.Cmd("tui.input.lineStart", InputScope, readline("A")...)
	InputLineEnd         = kit.Cmd("tui.input.lineEnd", InputScope, readline("E")...)
	InputKillLine        = kit.Cmd("tui.input.killLine", InputScope, readline("U")...)
	InputKillWord        = kit.Cmd("tui.input.killWord", InputScope, readline("W")...)

	inputCommands = []keymap.Command{InputLeft, InputSelectLeft, InputWordLeft, InputSelectWordLeft,
		InputRight, InputSelectRight, InputWordRight, InputSelectWordRight, InputHome, InputSelectHome,
		InputEnd, InputSelectEnd, InputBackspace, InputWordBackspace, InputDelete, InputSubmit,
		InputComplete, InputCompletePrev, InputLineStart, InputLineEnd, InputKillLine, InputKillWord}
)

// readline is a readline Ctrl key under every other modifier the old handler let through with Ctrl.
func readline(letter string) []string {
	var out []string
	for _, m := range []string{"Ctrl+", "Ctrl+Alt+", "Ctrl+Super+", "Ctrl+Alt+Super+"} {
		out = append(out, m+letter)
	}
	return out
}

// MenuScope is a menu's keys: a menu bar's and a popup menu's. Arrows follow the layout: a bar walks
// with Left/Right, a column with Up/Down. A row's mnemonic is not a command: it is its label's.
const MenuScope keymap.Scope = "tui.menu"

var (
	MenuLeft     = kit.Cmd("tui.menu.left", MenuScope, "Left")
	MenuRight    = kit.Cmd("tui.menu.right", MenuScope, "Right")
	MenuUp       = kit.Cmd("tui.menu.up", MenuScope, "Up")
	MenuDown     = kit.Cmd("tui.menu.down", MenuScope, "Down")
	MenuVimLeft  = kit.Cmd("tui.menu.vimLeft", MenuScope, "H") // with vim navigation, after any row's mnemonic
	MenuVimDown  = kit.Cmd("tui.menu.vimDown", MenuScope, "J")
	MenuVimUp    = kit.Cmd("tui.menu.vimUp", MenuScope, "K")
	MenuVimRight = kit.Cmd("tui.menu.vimRight", MenuScope, "L")
	MenuActivate = kit.Cmd("tui.menu.activate", MenuScope, "Enter", "Space")
	MenuClose    = kit.Cmd("tui.menu.close", MenuScope, "Esc") // one level per press; from the bar, leave the menu

	menuCommands = []keymap.Command{MenuLeft, MenuRight, MenuUp, MenuDown, MenuVimLeft, MenuVimDown,
		MenuVimUp, MenuVimRight, MenuActivate, MenuClose}

	// menuKey is the key each menu command stands for in the bar and column logic.
	menuKey = map[string]rune{
		MenuLeft.ID: tui.KeyLeft, MenuVimLeft.ID: tui.KeyLeft, MenuRight.ID: tui.KeyRight,
		MenuVimRight.ID: tui.KeyRight, MenuUp.ID: tui.KeyUp, MenuVimUp.ID: tui.KeyUp,
		MenuDown.ID: tui.KeyDown, MenuVimDown.ID: tui.KeyDown, MenuActivate.ID: tui.KeyEnter,
		MenuClose.ID: tui.KeyEscape,
	}
)

// isMenuVimAlias reports whether c is one of the menu's vim aliases, which yield to a row's mnemonic.
func isMenuVimAlias(c keymap.Command) bool {
	switch c.ID {
	case MenuVimLeft.ID, MenuVimDown.ID, MenuVimUp.ID, MenuVimRight.ID:
		return true
	}
	return false
}

// ButtonScope is a leaf control's activation (a Button, a standalone MenuItem).
const ButtonScope keymap.Scope = "tui.button"

// ButtonActivate is the activation key of a Button and a standalone MenuItem: the same
// tui.ActivateAction the pointer gesture produces.
var ButtonActivate = kit.Cmd("tui.button.activate", ButtonScope, "Enter", "Space")

// ModalScope is a Modal's keys. A button's mnemonic is not a command, and neither are a host's
// WithModalDismissKeys, which are its code.
const ModalScope keymap.Scope = "tui.modal"

var (
	ModalDismiss = kit.Cmd("tui.modal.dismiss", ModalScope, "Esc")
	ModalAccept  = kit.Cmd("tui.modal.accept", ModalScope, "Enter") // the default button, when the focused control leaves Enter
	ModalPrev    = kit.Cmd("tui.modal.prev", ModalScope, "Left", "Up")
	ModalNext    = kit.Cmd("tui.modal.next", ModalScope, "Right", "Down")
	ModalVimPrev = kit.Cmd("tui.modal.vimPrev", ModalScope, "H", "K") // with vim navigation, after the mnemonics
	ModalVimNext = kit.Cmd("tui.modal.vimNext", ModalScope, "L", "J")

	modalCommands = []keymap.Command{ModalDismiss, ModalAccept, ModalPrev, ModalNext, ModalVimPrev, ModalVimNext}
)

// ctrlAny is key under Ctrl with every set of Shift, Alt and Super, as a handler that tested only
// for Ctrl took it.
func ctrlAny(key string) []string {
	var out []string
	for _, alt := range []string{"", "Alt+"} {
		for _, super := range []string{"", "Super+"} {
			for _, shift := range []string{"", "Shift+"} {
				out = append(out, "Ctrl+"+alt+super+shift+key)
			}
		}
	}
	return out
}

// TabsScope is a Tabs container's keys. The arrows and brackets choose a tab while the bar itself has
// focus; Ctrl+PgUp and Ctrl+PgDn while focus is anywhere in it.
const TabsScope keymap.Scope = "tui.tabs"

var (
	TabsPrev  = kit.Cmd("tui.tabs.prev", TabsScope, ctrlAny("PgUp")...)
	TabsNext  = kit.Cmd("tui.tabs.next", TabsScope, ctrlAny("PgDown")...)
	TabsLeft  = kit.Cmd("tui.tabs.left", TabsScope, "Left", "[") // the bar focused
	TabsRight = kit.Cmd("tui.tabs.right", TabsScope, "Right", "]")

	tabsCommands = []keymap.Command{TabsPrev, TabsNext, TabsLeft, TabsRight}
)

// TabStripScope is a TabStrip's and a TerminalTabs' strip keys. Ctrl+PgUp and Ctrl+PgDn choose a tab
// while focus is anywhere in the owner; the rest while the strip itself has focus.
const TabStripScope keymap.Scope = "tui.tabStrip"

var (
	TabStripPrev  = kit.Cmd("tui.tabStrip.prev", TabStripScope, "Ctrl+PgUp")
	TabStripNext  = kit.Cmd("tui.tabStrip.next", TabStripScope, "Ctrl+PgDown")
	TabStripLeft  = kit.Cmd("tui.tabStrip.left", TabStripScope, "Left", "[")
	TabStripRight = kit.Cmd("tui.tabStrip.right", TabStripScope, "Right", "]")
	TabStripAdd   = kit.Cmd("tui.tabStrip.add", TabStripScope, "+")      // with something to add
	TabStripMenu  = kit.Cmd("tui.tabStrip.menu", TabStripScope, "Enter") // with a menu: the active tab's

	tabStripCommands = []keymap.Command{TabStripAdd, TabStripMenu, TabStripPrev, TabStripNext,
		TabStripLeft, TabStripRight}
)

// SplitScope is a Split's divider keys: Alt and an arrow along the split's axis step the divider, and
// Esc cancels a drag. Off its axis an arrow is not the split's.
const SplitScope keymap.Scope = "tui.split"

var (
	SplitCancel = kit.Cmd("tui.split.cancel", SplitScope, "Esc")
	SplitLeft   = kit.Cmd("tui.split.left", SplitScope, "Alt+Left") // a horizontal split's divider
	SplitRight  = kit.Cmd("tui.split.right", SplitScope, "Alt+Right")
	SplitUp     = kit.Cmd("tui.split.up", SplitScope, "Alt+Up") // a vertical split's
	SplitDown   = kit.Cmd("tui.split.down", SplitScope, "Alt+Down")

	splitHorizontal = []keymap.Command{SplitCancel, SplitLeft, SplitRight}
	splitVertical   = []keymap.Command{SplitCancel, SplitUp, SplitDown}
)

// ResizableScope is a Resizable's keys: Shift and an arrow step its box, leaving the bare arrows to
// its content, and Esc cancels a drag.
const ResizableScope keymap.Scope = "tui.resizable"

var (
	ResizableCancel = kit.Cmd("tui.resizable.cancel", ResizableScope, "Esc")
	ResizableLeft   = kit.Cmd("tui.resizable.left", ResizableScope, "Shift+Left")
	ResizableRight  = kit.Cmd("tui.resizable.right", ResizableScope, "Shift+Right")
	ResizableUp     = kit.Cmd("tui.resizable.up", ResizableScope, "Shift+Up")
	ResizableDown   = kit.Cmd("tui.resizable.down", ResizableScope, "Shift+Down")

	resizableCommands = []keymap.Command{ResizableCancel, ResizableLeft, ResizableRight, ResizableUp, ResizableDown}
)

// SelectScope is a closed Select's keys.
const SelectScope keymap.Scope = "tui.select"

// SelectOpen opens a Select's options.
var SelectOpen = kit.Cmd("tui.select.open", SelectScope, "Enter", "Shift+Enter", "Space", "Down", "Shift+Down")

// SelectPopupScope is an open Select's options. Its keys take any modifier, as its handler did. j and
// k move only where the select does not filter: a filter spends every letter on its query.
const SelectPopupScope keymap.Scope = "tui.select.popup"

var (
	SelectClose     = kit.Cmd("tui.select.popup.close", SelectPopupScope, chords("Esc", always)...)
	SelectCommit    = kit.Cmd("tui.select.popup.commit", SelectPopupScope, chords("Enter", always)...)
	SelectUp        = kit.Cmd("tui.select.popup.up", SelectPopupScope, chords("Up", always)...)
	SelectDown      = kit.Cmd("tui.select.popup.down", SelectPopupScope, chords("Down", always)...)
	SelectVimUp     = kit.Cmd("tui.select.popup.vimUp", SelectPopupScope, chords("K", unshifted)...)
	SelectVimDown   = kit.Cmd("tui.select.popup.vimDown", SelectPopupScope, chords("J", unshifted)...)
	SelectPageUp    = kit.Cmd("tui.select.popup.pageUp", SelectPopupScope, chords("PgUp", always)...)
	SelectPageDown  = kit.Cmd("tui.select.popup.pageDown", SelectPopupScope, chords("PgDown", always)...)
	SelectFirst     = kit.Cmd("tui.select.popup.first", SelectPopupScope, chords("Home", always)...)
	SelectLast      = kit.Cmd("tui.select.popup.last", SelectPopupScope, chords("End", always)...)
	SelectBackspace = kit.Cmd("tui.select.popup.backspace", SelectPopupScope, chords("Backspace", always)...) // the filter's last character

	selectPopupCommands = []keymap.Command{SelectClose, SelectCommit, SelectUp, SelectDown, SelectVimUp,
		SelectVimDown, SelectPageUp, SelectPageDown, SelectFirst, SelectLast, SelectBackspace}
)

// TextAreaScope is a TextArea's keys. Shift extends the selection, Ctrl or Alt moves by word, and Ctrl
// on Home and End goes to the text's start and end; typed text is not a command.
const TextAreaScope keymap.Scope = "tui.textarea"

var (
	lineKey = func(shift, ctrl, _ bool) bool { return !shift && !ctrl }
	lineSel = func(shift, ctrl, _ bool) bool { return shift && !ctrl }
	textKey = func(shift, ctrl, _ bool) bool { return !shift && ctrl }
	textSel = func(shift, ctrl, _ bool) bool { return shift && ctrl }

	TextAreaNewline         = kit.Cmd("tui.textarea.newline", TextAreaScope, kit.Mods("Enter", kit.AnyCombo)...)
	TextAreaBackspace       = kit.Cmd("tui.textarea.backspace", TextAreaScope, kit.Mods("Backspace", kit.AnyCombo)...)
	TextAreaDelete          = kit.Cmd("tui.textarea.delete", TextAreaScope, kit.Mods("Del", kit.AnyCombo)...)
	TextAreaLeft            = kit.Cmd("tui.textarea.left", TextAreaScope, chords("Left", plain)...)
	TextAreaSelectLeft      = kit.Cmd("tui.textarea.selectLeft", TextAreaScope, chords("Left", shifted)...)
	TextAreaWordLeft        = kit.Cmd("tui.textarea.wordLeft", TextAreaScope, chords("Left", worded)...)
	TextAreaSelectWordLeft  = kit.Cmd("tui.textarea.selectWordLeft", TextAreaScope, chords("Left", both)...)
	TextAreaRight           = kit.Cmd("tui.textarea.right", TextAreaScope, chords("Right", plain)...)
	TextAreaSelectRight     = kit.Cmd("tui.textarea.selectRight", TextAreaScope, chords("Right", shifted)...)
	TextAreaWordRight       = kit.Cmd("tui.textarea.wordRight", TextAreaScope, chords("Right", worded)...)
	TextAreaSelectWordRight = kit.Cmd("tui.textarea.selectWordRight", TextAreaScope, chords("Right", both)...)
	TextAreaUp              = kit.Cmd("tui.textarea.up", TextAreaScope, chords("Up", unshifted)...)
	TextAreaSelectUp        = kit.Cmd("tui.textarea.selectUp", TextAreaScope, chords("Up", anyShift)...)
	TextAreaDown            = kit.Cmd("tui.textarea.down", TextAreaScope, chords("Down", unshifted)...)
	TextAreaSelectDown      = kit.Cmd("tui.textarea.selectDown", TextAreaScope, chords("Down", anyShift)...)
	TextAreaPageUp          = kit.Cmd("tui.textarea.pageUp", TextAreaScope, chords("PgUp", unshifted)...)
	TextAreaSelectPageUp    = kit.Cmd("tui.textarea.selectPageUp", TextAreaScope, chords("PgUp", anyShift)...)
	TextAreaPageDown        = kit.Cmd("tui.textarea.pageDown", TextAreaScope, chords("PgDown", unshifted)...)
	TextAreaSelectPageDown  = kit.Cmd("tui.textarea.selectPageDown", TextAreaScope, chords("PgDown", anyShift)...)
	TextAreaHome            = kit.Cmd("tui.textarea.home", TextAreaScope, kit.Mods("Home", lineKey)...)
	TextAreaSelectHome      = kit.Cmd("tui.textarea.selectHome", TextAreaScope, kit.Mods("Home", lineSel)...)
	TextAreaStart           = kit.Cmd("tui.textarea.start", TextAreaScope, kit.Mods("Home", textKey)...)
	TextAreaSelectStart     = kit.Cmd("tui.textarea.selectStart", TextAreaScope, kit.Mods("Home", textSel)...)
	TextAreaEnd             = kit.Cmd("tui.textarea.end", TextAreaScope, kit.Mods("End", lineKey)...)
	TextAreaSelectEnd       = kit.Cmd("tui.textarea.selectEnd", TextAreaScope, kit.Mods("End", lineSel)...)
	TextAreaFinish          = kit.Cmd("tui.textarea.finish", TextAreaScope, kit.Mods("End", textKey)...)
	TextAreaSelectFinish    = kit.Cmd("tui.textarea.selectFinish", TextAreaScope, kit.Mods("End", textSel)...)
	TextAreaLineStart       = kit.Cmd("tui.textarea.lineStart", TextAreaScope, readline("A")...)
	TextAreaLineEnd         = kit.Cmd("tui.textarea.lineEnd", TextAreaScope, readline("E")...)
	TextAreaKillLine        = kit.Cmd("tui.textarea.killLine", TextAreaScope, readline("U")...)
	TextAreaKillWord        = kit.Cmd("tui.textarea.killWord", TextAreaScope, readline("W")...)

	textAreaCommands = []keymap.Command{TextAreaNewline, TextAreaBackspace, TextAreaDelete,
		TextAreaLeft, TextAreaSelectLeft, TextAreaWordLeft, TextAreaSelectWordLeft,
		TextAreaRight, TextAreaSelectRight, TextAreaWordRight, TextAreaSelectWordRight,
		TextAreaUp, TextAreaSelectUp, TextAreaDown, TextAreaSelectDown,
		TextAreaPageUp, TextAreaSelectPageUp, TextAreaPageDown, TextAreaSelectPageDown,
		TextAreaHome, TextAreaSelectHome, TextAreaStart, TextAreaSelectStart,
		TextAreaEnd, TextAreaSelectEnd, TextAreaFinish, TextAreaSelectFinish,
		TextAreaLineStart, TextAreaLineEnd, TextAreaKillLine, TextAreaKillWord}
)

// BufferViewScope is a BufferView's keys. The arrows, PgUp, PgDn, Home and End take any modifier, as
// its handler did; Y takes none.
const BufferViewScope keymap.Scope = "tui.bufferview"

var (
	BufferViewUp       = kit.Cmd("tui.bufferview.up", BufferViewScope, kit.Mods("Up", kit.AnyCombo)...)
	BufferViewDown     = kit.Cmd("tui.bufferview.down", BufferViewScope, kit.Mods("Down", kit.AnyCombo)...)
	BufferViewPageUp   = kit.Cmd("tui.bufferview.pageUp", BufferViewScope, kit.Mods("PgUp", kit.AnyCombo)...)
	BufferViewPageDown = kit.Cmd("tui.bufferview.pageDown", BufferViewScope, kit.Mods("PgDown", kit.AnyCombo)...)
	BufferViewTop      = kit.Cmd("tui.bufferview.top", BufferViewScope, kit.Mods("Home", kit.AnyCombo)...)
	BufferViewBottom   = kit.Cmd("tui.bufferview.bottom", BufferViewScope, kit.Mods("End", kit.AnyCombo)...) // and follows the tail
	BufferViewCopy     = kit.Cmd("tui.bufferview.copy", BufferViewScope, "Y")                                // the whole buffer

	bufferViewCommands = []keymap.Command{BufferViewUp, BufferViewDown, BufferViewPageUp,
		BufferViewPageDown, BufferViewTop, BufferViewBottom, BufferViewCopy}
)

// FloatScope is a modal Float's keys: Esc hides it.
const FloatScope keymap.Scope = "tui.float"

// FloatClose hides a modal Float.
var FloatClose = kit.Cmd("tui.float.close", FloatScope, "Esc")

// PopupScope is an anchored popup's keys (a menu's, a select's, a strip's): Esc dismisses it.
const PopupScope keymap.Scope = "tui.popup"

// PopupClose dismisses an anchored popup.
var PopupClose = kit.Cmd("tui.popup.close", PopupScope, "Esc")

// WindowMenu opens and leaves the window's menu bar (a declarative Window's): F10, as a desktop's
// toolkit binds it. It is not a mnemonic; a category's Alt+letter is.
var WindowMenu = kit.Cmd("tui.window.menu", keymap.Global, "F10", "Super+F10")

// DrawerScope is a declarative Drawer's own keys, after its Shortcuts: Esc closes it, and q too where
// it says closeOnQ.
const DrawerScope keymap.Scope = "tui.drawer"

var (
	DrawerClose    = kit.Cmd("tui.drawer.close", DrawerScope, "Esc")
	DrawerCloseOnQ = kit.Cmd("tui.drawer.closeOnQ", DrawerScope, "Q") // with closeOnQ
)
