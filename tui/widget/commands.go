package widget

import (
	"slices"

	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// KEY COMMANDS — every key a golib widget acts on, declared as data. A widget
// asks its Context which of its own commands a key is (tui.Context.KeyCommand); a key-mapping scheme
// changes their keys by id. Each widget's defaults reproduce the keys its handler took before
// commands existed, written out key by key: where the old handler let Shift through to a functional
// key, the Shift form is a default too.
//
// A label's mnemonic (&File, a button's underlined letter) is not a command and cannot be rebound.
//
// This file declares cmd, the helpers commands are written with, and core's widgets' commands. A
// component's own are in its <component>_commands.go (table_commands.go, terminal_commands.go, …).

// cmd declares one command.
func cmd(id string, scope keymap.Scope, keys ...string) keymap.Command {
	c := keymap.Command{ID: id, Scope: scope, Keys: tuikey.MustParse(keys...)}
	declared = append(declared, c)
	return c
}

// declared is every key command a widget declares through cmd, in the order the package initializes
// them (its files by name). KeyCommands lists them, so a new widget's commands are in keymap.Build
// without another list to edit.
var declared []keymap.Command

// isKey reports whether e is one of keys, by the one matching model (key.Of, then its candidates). A
// grammar's continuation is read with it: the second g, the key after the Terminal's prefix. They are
// the keys the grammar names, so a map moving the prefix moves the prefix alone.
func isKey(e tui.KeyEvent, keys ...tuikey.Seq) bool {
	if e.Kind == tui.KeyRelease {
		return false
	}
	chord, ok := tuikey.Of(e.Code, e.Mods, e.Text)
	if !ok {
		return false
	}
	for _, c := range tuikey.Candidates(chord) {
		if slices.Contains(keys, c) {
			return true
		}
	}
	return false
}

// with is each text and its Shift form, for a functional key the old handlers took either way.
func with(texts ...string) []string {
	out := make([]string, 0, 2*len(texts))
	for _, t := range texts {
		out = append(out, t, "Shift+"+t)
	}
	return out
}

// ListScope is a List's keys.
const ListScope keymap.Scope = "tui.list"

var (
	ListUp       = cmd("tui.list.up", ListScope, append(with("Up"), "K")...)
	ListDown     = cmd("tui.list.down", ListScope, append(with("Down"), "J")...)
	ListTop      = cmd("tui.list.top", ListScope, "G")
	ListBottom   = cmd("tui.list.bottom", ListScope, "Shift+G")
	ListPageUp   = cmd("tui.list.pageUp", ListScope, with("PgUp")...)
	ListPageDown = cmd("tui.list.pageDown", ListScope, with("PgDown")...)
	ListHome     = cmd("tui.list.home", ListScope, with("Home")...)
	ListEnd      = cmd("tui.list.end", ListScope, with("End")...)
	ListActivate = cmd("tui.list.activate", ListScope, with("Enter")...)
	ListToggle   = cmd("tui.list.toggle", ListScope, "Space") // multi-select only
	ListMenu     = cmd("tui.list.contextMenu", ListScope, "Shift+F10", "Menu")

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

	InputLeft            = cmd("tui.input.left", InputScope, chords("Left", plain)...)
	InputSelectLeft      = cmd("tui.input.selectLeft", InputScope, chords("Left", shifted)...)
	InputWordLeft        = cmd("tui.input.wordLeft", InputScope, chords("Left", worded)...)
	InputSelectWordLeft  = cmd("tui.input.selectWordLeft", InputScope, chords("Left", both)...)
	InputRight           = cmd("tui.input.right", InputScope, chords("Right", plain)...)
	InputSelectRight     = cmd("tui.input.selectRight", InputScope, chords("Right", shifted)...)
	InputWordRight       = cmd("tui.input.wordRight", InputScope, chords("Right", worded)...)
	InputSelectWordRight = cmd("tui.input.selectWordRight", InputScope, chords("Right", both)...)
	InputHome            = cmd("tui.input.home", InputScope, chords("Home", unshifted)...)
	InputSelectHome      = cmd("tui.input.selectHome", InputScope, chords("Home", anyShift)...)
	InputEnd             = cmd("tui.input.end", InputScope, chords("End", unshifted)...)
	InputSelectEnd       = cmd("tui.input.selectEnd", InputScope, chords("End", anyShift)...)
	InputBackspace       = cmd("tui.input.backspace", InputScope, chords("Backspace", unworded)...)
	InputWordBackspace   = cmd("tui.input.wordBackspace", InputScope, chords("Backspace", anyWord)...)
	InputDelete          = cmd("tui.input.delete", InputScope, chords("Del", always)...)
	InputSubmit          = cmd("tui.input.submit", InputScope, chords("Enter", always)...)
	InputComplete        = cmd("tui.input.complete", InputScope, "Tab")           // with a completer
	InputCompletePrev    = cmd("tui.input.completePrev", InputScope, "Shift+Tab") // with a completer
	InputLineStart       = cmd("tui.input.lineStart", InputScope, readline("A")...)
	InputLineEnd         = cmd("tui.input.lineEnd", InputScope, readline("E")...)
	InputKillLine        = cmd("tui.input.killLine", InputScope, readline("U")...)
	InputKillWord        = cmd("tui.input.killWord", InputScope, readline("W")...)

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
	MenuLeft     = cmd("tui.menu.left", MenuScope, "Left")
	MenuRight    = cmd("tui.menu.right", MenuScope, "Right")
	MenuUp       = cmd("tui.menu.up", MenuScope, "Up")
	MenuDown     = cmd("tui.menu.down", MenuScope, "Down")
	MenuVimLeft  = cmd("tui.menu.vimLeft", MenuScope, "H") // with vim navigation, after any row's mnemonic
	MenuVimDown  = cmd("tui.menu.vimDown", MenuScope, "J")
	MenuVimUp    = cmd("tui.menu.vimUp", MenuScope, "K")
	MenuVimRight = cmd("tui.menu.vimRight", MenuScope, "L")
	MenuActivate = cmd("tui.menu.activate", MenuScope, "Enter", "Space")
	MenuClose    = cmd("tui.menu.close", MenuScope, "Esc") // one level per press; from the bar, leave the menu

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
var ButtonActivate = cmd("tui.button.activate", ButtonScope, "Enter", "Space")

// ModalScope is a Modal's keys. A button's mnemonic is not a command, and neither are a host's
// WithModalDismissKeys, which are its code.
const ModalScope keymap.Scope = "tui.modal"

var (
	ModalDismiss = cmd("tui.modal.dismiss", ModalScope, "Esc")
	ModalAccept  = cmd("tui.modal.accept", ModalScope, "Enter") // the default button, when the focused control leaves Enter
	ModalPrev    = cmd("tui.modal.prev", ModalScope, "Left", "Up")
	ModalNext    = cmd("tui.modal.next", ModalScope, "Right", "Down")
	ModalVimPrev = cmd("tui.modal.vimPrev", ModalScope, "H", "K") // with vim navigation, after the mnemonics
	ModalVimNext = cmd("tui.modal.vimNext", ModalScope, "L", "J")

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
	TabsPrev  = cmd("tui.tabs.prev", TabsScope, ctrlAny("PgUp")...)
	TabsNext  = cmd("tui.tabs.next", TabsScope, ctrlAny("PgDown")...)
	TabsLeft  = cmd("tui.tabs.left", TabsScope, "Left", "[") // the bar focused
	TabsRight = cmd("tui.tabs.right", TabsScope, "Right", "]")

	tabsCommands = []keymap.Command{TabsPrev, TabsNext, TabsLeft, TabsRight}
)

// TabStripScope is a TabStrip's and a TerminalTabs' strip keys. Ctrl+PgUp and Ctrl+PgDn choose a tab
// while focus is anywhere in the owner; the rest while the strip itself has focus.
const TabStripScope keymap.Scope = "tui.tabStrip"

var (
	TabStripPrev  = cmd("tui.tabStrip.prev", TabStripScope, "Ctrl+PgUp")
	TabStripNext  = cmd("tui.tabStrip.next", TabStripScope, "Ctrl+PgDown")
	TabStripLeft  = cmd("tui.tabStrip.left", TabStripScope, "Left", "[")
	TabStripRight = cmd("tui.tabStrip.right", TabStripScope, "Right", "]")
	TabStripAdd   = cmd("tui.tabStrip.add", TabStripScope, "+")      // with something to add
	TabStripMenu  = cmd("tui.tabStrip.menu", TabStripScope, "Enter") // with a menu: the active tab's

	tabStripCommands = []keymap.Command{TabStripAdd, TabStripMenu, TabStripPrev, TabStripNext,
		TabStripLeft, TabStripRight}
)

// SplitScope is a Split's divider keys: Alt and an arrow along the split's axis step the divider, and
// Esc cancels a drag. Off its axis an arrow is not the split's.
const SplitScope keymap.Scope = "tui.split"

var (
	SplitCancel = cmd("tui.split.cancel", SplitScope, "Esc")
	SplitLeft   = cmd("tui.split.left", SplitScope, "Alt+Left") // a horizontal split's divider
	SplitRight  = cmd("tui.split.right", SplitScope, "Alt+Right")
	SplitUp     = cmd("tui.split.up", SplitScope, "Alt+Up") // a vertical split's
	SplitDown   = cmd("tui.split.down", SplitScope, "Alt+Down")

	splitHorizontal = []keymap.Command{SplitCancel, SplitLeft, SplitRight}
	splitVertical   = []keymap.Command{SplitCancel, SplitUp, SplitDown}
)

// ResizableScope is a Resizable's keys: Shift and an arrow step its box, leaving the bare arrows to
// its content, and Esc cancels a drag.
const ResizableScope keymap.Scope = "tui.resizable"

var (
	ResizableCancel = cmd("tui.resizable.cancel", ResizableScope, "Esc")
	ResizableLeft   = cmd("tui.resizable.left", ResizableScope, "Shift+Left")
	ResizableRight  = cmd("tui.resizable.right", ResizableScope, "Shift+Right")
	ResizableUp     = cmd("tui.resizable.up", ResizableScope, "Shift+Up")
	ResizableDown   = cmd("tui.resizable.down", ResizableScope, "Shift+Down")

	resizableCommands = []keymap.Command{ResizableCancel, ResizableLeft, ResizableRight, ResizableUp, ResizableDown}
)

// SelectScope is a closed Select's keys.
const SelectScope keymap.Scope = "tui.select"

// SelectOpen opens a Select's options.
var SelectOpen = cmd("tui.select.open", SelectScope, "Enter", "Shift+Enter", "Space", "Down", "Shift+Down")

// SelectPopupScope is an open Select's options. Its keys take any modifier, as its handler did. j and
// k move only where the select does not filter: a filter spends every letter on its query.
const SelectPopupScope keymap.Scope = "tui.select.popup"

var (
	SelectClose     = cmd("tui.select.popup.close", SelectPopupScope, chords("Esc", always)...)
	SelectCommit    = cmd("tui.select.popup.commit", SelectPopupScope, chords("Enter", always)...)
	SelectUp        = cmd("tui.select.popup.up", SelectPopupScope, chords("Up", always)...)
	SelectDown      = cmd("tui.select.popup.down", SelectPopupScope, chords("Down", always)...)
	SelectVimUp     = cmd("tui.select.popup.vimUp", SelectPopupScope, chords("K", unshifted)...)
	SelectVimDown   = cmd("tui.select.popup.vimDown", SelectPopupScope, chords("J", unshifted)...)
	SelectPageUp    = cmd("tui.select.popup.pageUp", SelectPopupScope, chords("PgUp", always)...)
	SelectPageDown  = cmd("tui.select.popup.pageDown", SelectPopupScope, chords("PgDown", always)...)
	SelectFirst     = cmd("tui.select.popup.first", SelectPopupScope, chords("Home", always)...)
	SelectLast      = cmd("tui.select.popup.last", SelectPopupScope, chords("End", always)...)
	SelectBackspace = cmd("tui.select.popup.backspace", SelectPopupScope, chords("Backspace", always)...) // the filter's last character

	selectPopupCommands = []keymap.Command{SelectClose, SelectCommit, SelectUp, SelectDown, SelectVimUp,
		SelectVimDown, SelectPageUp, SelectPageDown, SelectFirst, SelectLast, SelectBackspace}
)

// mods is key under every set of Shift, Ctrl, Alt and Super that keep accepts, Super always free: a
// handler that tested one modifier and ignored the rest took the key under all of them.
func mods(key string, keep func(shift, ctrl, alt bool) bool) []string {
	var out []string
	for _, shift := range []bool{false, true} {
		for _, ctrl := range []bool{false, true} {
			for _, alt := range []bool{false, true} {
				if !keep(shift, ctrl, alt) {
					continue
				}
				m := map[bool]string{true: "Ctrl+"}[ctrl] + map[bool]string{true: "Alt+"}[alt]
				for _, super := range []string{"", "Super+"} {
					out = append(out, m+super+map[bool]string{true: "Shift+"}[shift]+key)
				}
			}
		}
	}
	return out
}

// TextAreaScope is a TextArea's keys. Shift extends the selection, Ctrl or Alt moves by word, and Ctrl
// on Home and End goes to the text's start and end; typed text is not a command.
const TextAreaScope keymap.Scope = "tui.textarea"

var (
	lineKey  = func(shift, ctrl, _ bool) bool { return !shift && !ctrl }
	lineSel  = func(shift, ctrl, _ bool) bool { return shift && !ctrl }
	textKey  = func(shift, ctrl, _ bool) bool { return !shift && ctrl }
	textSel  = func(shift, ctrl, _ bool) bool { return shift && ctrl }
	anyCombo = func(bool, bool, bool) bool { return true }

	TextAreaNewline         = cmd("tui.textarea.newline", TextAreaScope, mods("Enter", anyCombo)...)
	TextAreaBackspace       = cmd("tui.textarea.backspace", TextAreaScope, mods("Backspace", anyCombo)...)
	TextAreaDelete          = cmd("tui.textarea.delete", TextAreaScope, mods("Del", anyCombo)...)
	TextAreaLeft            = cmd("tui.textarea.left", TextAreaScope, chords("Left", plain)...)
	TextAreaSelectLeft      = cmd("tui.textarea.selectLeft", TextAreaScope, chords("Left", shifted)...)
	TextAreaWordLeft        = cmd("tui.textarea.wordLeft", TextAreaScope, chords("Left", worded)...)
	TextAreaSelectWordLeft  = cmd("tui.textarea.selectWordLeft", TextAreaScope, chords("Left", both)...)
	TextAreaRight           = cmd("tui.textarea.right", TextAreaScope, chords("Right", plain)...)
	TextAreaSelectRight     = cmd("tui.textarea.selectRight", TextAreaScope, chords("Right", shifted)...)
	TextAreaWordRight       = cmd("tui.textarea.wordRight", TextAreaScope, chords("Right", worded)...)
	TextAreaSelectWordRight = cmd("tui.textarea.selectWordRight", TextAreaScope, chords("Right", both)...)
	TextAreaUp              = cmd("tui.textarea.up", TextAreaScope, chords("Up", unshifted)...)
	TextAreaSelectUp        = cmd("tui.textarea.selectUp", TextAreaScope, chords("Up", anyShift)...)
	TextAreaDown            = cmd("tui.textarea.down", TextAreaScope, chords("Down", unshifted)...)
	TextAreaSelectDown      = cmd("tui.textarea.selectDown", TextAreaScope, chords("Down", anyShift)...)
	TextAreaPageUp          = cmd("tui.textarea.pageUp", TextAreaScope, chords("PgUp", unshifted)...)
	TextAreaSelectPageUp    = cmd("tui.textarea.selectPageUp", TextAreaScope, chords("PgUp", anyShift)...)
	TextAreaPageDown        = cmd("tui.textarea.pageDown", TextAreaScope, chords("PgDown", unshifted)...)
	TextAreaSelectPageDown  = cmd("tui.textarea.selectPageDown", TextAreaScope, chords("PgDown", anyShift)...)
	TextAreaHome            = cmd("tui.textarea.home", TextAreaScope, mods("Home", lineKey)...)
	TextAreaSelectHome      = cmd("tui.textarea.selectHome", TextAreaScope, mods("Home", lineSel)...)
	TextAreaStart           = cmd("tui.textarea.start", TextAreaScope, mods("Home", textKey)...)
	TextAreaSelectStart     = cmd("tui.textarea.selectStart", TextAreaScope, mods("Home", textSel)...)
	TextAreaEnd             = cmd("tui.textarea.end", TextAreaScope, mods("End", lineKey)...)
	TextAreaSelectEnd       = cmd("tui.textarea.selectEnd", TextAreaScope, mods("End", lineSel)...)
	TextAreaFinish          = cmd("tui.textarea.finish", TextAreaScope, mods("End", textKey)...)
	TextAreaSelectFinish    = cmd("tui.textarea.selectFinish", TextAreaScope, mods("End", textSel)...)
	TextAreaLineStart       = cmd("tui.textarea.lineStart", TextAreaScope, readline("A")...)
	TextAreaLineEnd         = cmd("tui.textarea.lineEnd", TextAreaScope, readline("E")...)
	TextAreaKillLine        = cmd("tui.textarea.killLine", TextAreaScope, readline("U")...)
	TextAreaKillWord        = cmd("tui.textarea.killWord", TextAreaScope, readline("W")...)

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
	BufferViewUp       = cmd("tui.bufferview.up", BufferViewScope, mods("Up", anyCombo)...)
	BufferViewDown     = cmd("tui.bufferview.down", BufferViewScope, mods("Down", anyCombo)...)
	BufferViewPageUp   = cmd("tui.bufferview.pageUp", BufferViewScope, mods("PgUp", anyCombo)...)
	BufferViewPageDown = cmd("tui.bufferview.pageDown", BufferViewScope, mods("PgDown", anyCombo)...)
	BufferViewTop      = cmd("tui.bufferview.top", BufferViewScope, mods("Home", anyCombo)...)
	BufferViewBottom   = cmd("tui.bufferview.bottom", BufferViewScope, mods("End", anyCombo)...) // and follows the tail
	BufferViewCopy     = cmd("tui.bufferview.copy", BufferViewScope, "Y")                        // the whole buffer

	bufferViewCommands = []keymap.Command{BufferViewUp, BufferViewDown, BufferViewPageUp,
		BufferViewPageDown, BufferViewTop, BufferViewBottom, BufferViewCopy}
)

// FloatScope is a modal Float's keys: Esc hides it.
const FloatScope keymap.Scope = "tui.float"

// FloatClose hides a modal Float.
var FloatClose = cmd("tui.float.close", FloatScope, "Esc")

// PopupScope is an anchored popup's keys (a menu's, a select's, a strip's): Esc dismisses it.
const PopupScope keymap.Scope = "tui.popup"

// PopupClose dismisses an anchored popup.
var PopupClose = cmd("tui.popup.close", PopupScope, "Esc")

// WindowMenu opens and leaves the window's menu bar (a declarative Window's): F10, as a desktop's
// toolkit binds it. It is not a mnemonic; a category's Alt+letter is.
var WindowMenu = cmd("tui.window.menu", keymap.Global, "F10", "Super+F10")

// DrawerScope is a declarative Drawer's own keys, after its Shortcuts: Esc closes it, and q too where
// it says closeOnQ.
const DrawerScope keymap.Scope = "tui.drawer"

var (
	DrawerClose    = cmd("tui.drawer.close", DrawerScope, "Esc")
	DrawerCloseOnQ = cmd("tui.drawer.closeOnQ", DrawerScope, "Q") // with closeOnQ
)
