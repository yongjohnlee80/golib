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

// cmd declares one command.
func cmd(id string, scope keymap.Scope, keys ...string) keymap.Command {
	return keymap.Command{ID: id, Scope: scope, Keys: tuikey.MustParse(keys...)}
}

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

// The literal continuations: g after g, and Ctrl+\ after the Terminal's prefix, which sends it.
var (
	literalG         = tuikey.MustParse("G")
	literalCtrlSlash = tuikey.MustParse("Ctrl+\\", "Ctrl+Shift+\\")
)

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

// TreeScope is a Tree's keys.
const TreeScope keymap.Scope = "tui.tree"

var (
	TreeDown     = cmd("tui.tree.down", TreeScope, append(with("Down"), "J")...)
	TreeUp       = cmd("tui.tree.up", TreeScope, append(with("Up"), "K")...)
	TreePageDown = cmd("tui.tree.pageDown", TreeScope, with("PgDown")...)
	TreePageUp   = cmd("tui.tree.pageUp", TreeScope, with("PgUp")...)
	TreeFirst    = cmd("tui.tree.first", TreeScope, append(with("Home"), "G")...)
	TreeLast     = cmd("tui.tree.last", TreeScope, append(with("End"), "Shift+G")...)
	TreeExpand   = cmd("tui.tree.expand", TreeScope, append(with("Right"), "L")...)  // a leaf activates; an open branch steps in
	TreeCollapse = cmd("tui.tree.collapse", TreeScope, append(with("Left"), "H")...) // a closed node steps to its parent
	TreeActivate = cmd("tui.tree.activate", TreeScope, with("Enter")...)

	treeCommands = []keymap.Command{TreeDown, TreeUp, TreePageDown, TreePageUp, TreeFirst, TreeLast,
		TreeExpand, TreeCollapse, TreeActivate}
)

// TableScope is a table's keys (TableCore: DataTable, and the GUI's table).
const TableScope keymap.Scope = "tui.table"

// The table's keys are a small grammar, as an editor's: a count ([count]j), and g waiting for its
// second g. The digits are that grammar, not commands; the first key of every command, the g prefix's
// included, is a command.
var (
	TableUp          = cmd("tui.table.up", TableScope, append(with("Up"), "K")...)
	TableDown        = cmd("tui.table.down", TableScope, append(with("Down"), "J")...)
	TableLeft        = cmd("tui.table.left", TableScope, append(with("Left"), "H")...)
	TableRight       = cmd("tui.table.right", TableScope, append(with("Right"), "L")...)
	TableFieldNext   = cmd("tui.table.fieldNext", TableScope, "W")
	TableFieldPrev   = cmd("tui.table.fieldPrev", TableScope, "B")
	TablePageUp      = cmd("tui.table.pageUp", TableScope, with("PgUp")...)
	TablePageDown    = cmd("tui.table.pageDown", TableScope, with("PgDown")...)
	TableGoPrefix    = cmd("tui.table.goPrefix", TableScope, "G")   // g, then g: the first row ([count]gg: row count)
	TableLast        = cmd("tui.table.last", TableScope, "Shift+G") // [count]G: row count
	TableFieldFirst  = cmd("tui.table.fieldFirst", TableScope, append(with("Home"), "0")...)
	TableFieldLast   = cmd("tui.table.fieldLast", TableScope, append(with("End"), "$")...)
	TableVisual      = cmd("tui.table.selectCells", TableScope, "V")
	TableVisualRows  = cmd("tui.table.selectRows", TableScope, "Shift+V")
	TableVisualBlock = cmd("tui.table.selectBlock", TableScope, "Ctrl+V")
	TableEscape      = cmd("tui.table.escape", TableScope, with("Esc")...)
	TableYank        = cmd("tui.table.yank", TableScope, "Y") // y copies; y again copies the row
	TableSort        = cmd("tui.table.sort", TableScope, "S")
	TableActivate    = cmd("tui.table.activate", TableScope, with("Enter")...) // on an editable source: edit the cell
	// An editable source's: open the cell's field, a new row below, delete the
	// row (d, then d), empty the cell.
	TableEditCell  = cmd("tui.table.edit", TableScope, "I", "F2")
	TableOpenRow   = cmd("tui.table.openRow", TableScope, "O")
	TableDeleteRow = cmd("tui.table.deleteRow", TableScope, "D") // d, then d
	TableClearCell = cmd("tui.table.clearCell", TableScope, "X", "Delete")

	tableCommands = []keymap.Command{TableUp, TableDown, TableLeft, TableRight, TableFieldNext,
		TableFieldPrev, TablePageUp, TablePageDown, TableGoPrefix, TableLast, TableFieldFirst,
		TableFieldLast, TableVisual, TableVisualRows, TableVisualBlock, TableEscape, TableYank,
		TableSort, TableActivate, TableEditCell, TableOpenRow, TableDeleteRow, TableClearCell}
)

// TableFieldScope is the keys of a table cell's open field (TableEdit), before its TextArea's.
const TableFieldScope keymap.Scope = "tui.tableField"

var (
	TableFieldKeep     = cmd("tui.tableField.keep", TableFieldScope, "Enter") // keep, and move down
	TableFieldNewline  = cmd("tui.tableField.newline", TableFieldScope, "Alt+Enter", "Shift+Enter")
	TableFieldNextCell = cmd("tui.tableField.next", TableFieldScope, "Tab")           // keep, and move right
	TableFieldPrevCell = cmd("tui.tableField.previous", TableFieldScope, "Shift+Tab") // keep, and move left
	TableFieldDrop     = cmd("tui.tableField.drop", TableFieldScope, "Esc")
	TableFieldLiteral  = cmd("tui.tableField.literal", TableFieldScope, "Ctrl+V") // then Tab: a tab in the value

	tableFieldCommands = []keymap.Command{TableFieldKeep, TableFieldNewline, TableFieldNextCell,
		TableFieldPrevCell, TableFieldDrop, TableFieldLiteral}
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

// HTMLViewScope is an HTMLView's keys. The arrows, PgUp, PgDn, Home, End and Esc take any modifier, as
// its handler did; the letters and Space take none.
const HTMLViewScope keymap.Scope = "tui.htmlview"

var (
	HTMLViewUp       = cmd("tui.htmlview.up", HTMLViewScope, append(mods("Up", anyCombo), "K")...)
	HTMLViewDown     = cmd("tui.htmlview.down", HTMLViewScope, append(mods("Down", anyCombo), "J")...)
	HTMLViewPageUp   = cmd("tui.htmlview.pageUp", HTMLViewScope, mods("PgUp", anyCombo)...)
	HTMLViewPageDown = cmd("tui.htmlview.pageDown", HTMLViewScope, append(mods("PgDown", anyCombo), "Space")...)
	HTMLViewHalfUp   = cmd("tui.htmlview.halfUp", HTMLViewScope, "Ctrl+U")
	HTMLViewHalfDown = cmd("tui.htmlview.halfDown", HTMLViewScope, "Ctrl+D")
	HTMLViewTop      = cmd("tui.htmlview.top", HTMLViewScope, append(mods("Home", anyCombo), "G")...)
	HTMLViewBottom   = cmd("tui.htmlview.bottom", HTMLViewScope, append(mods("End", anyCombo), "Shift+G")...)
	HTMLViewCopy     = cmd("tui.htmlview.copy", HTMLViewScope, "Ctrl+C", "Y")
	HTMLViewClear    = cmd("tui.htmlview.clearSelection", HTMLViewScope, mods("Esc", anyCombo)...) // with a selection

	htmlViewCommands = []keymap.Command{HTMLViewUp, HTMLViewDown, HTMLViewPageUp, HTMLViewPageDown,
		HTMLViewHalfUp, HTMLViewHalfDown, HTMLViewTop, HTMLViewBottom, HTMLViewCopy, HTMLViewClear}
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

// FileViewScope is a file chooser's own keys, heard as they bubble up from its parts. Each acts only
// where it belongs: ~ and / in the listing, Enter in the filter field, Esc in the path field or with a
// filter to clear, and Ctrl and h, j, k or l among the parts.
const FileViewScope keymap.Scope = "tui.fileview"

var (
	FileViewHome       = cmd("tui.fileview.home", FileViewScope, "~")                              // the path field, ~/ written
	FileViewFilter     = cmd("tui.fileview.filter", FileViewScope, "/")                            // the filter field
	FileViewFilterDone = cmd("tui.fileview.filterDone", FileViewScope, mods("Enter", anyCombo)...) // back to the listing, the filter kept
	FileViewCancel     = cmd("tui.fileview.cancel", FileViewScope, mods("Esc", anyCombo)...)       // the path field's folder put back, else the filter cleared
	FileViewPartDown   = cmd("tui.fileview.partDown", FileViewScope, "Ctrl+J")
	FileViewPartUp     = cmd("tui.fileview.partUp", FileViewScope, "Ctrl+K")
	FileViewPreview    = cmd("tui.fileview.preview", FileViewScope, "Ctrl+L")
	FileViewBack       = cmd("tui.fileview.back", FileViewScope, "Ctrl+H") // from the preview

	fileViewCommands = []keymap.Command{FileViewHome, FileViewFilter, FileViewFilterDone, FileViewCancel,
		FileViewPartDown, FileViewPartUp, FileViewPreview, FileViewBack}
)

// CommandLineScope is the command-line prompt's own keys: Tab and Shift+Tab complete, and with the
// candidates' dropdown open, Down and Up move through it as they do. Each with any other modifier too.
const CommandLineScope keymap.Scope = "tui.cmdline"

var (
	noShift   = func(shift, _, _ bool) bool { return !shift }
	withShift = func(shift, _, _ bool) bool { return shift }

	CmdlineComplete     = cmd("tui.cmdline.complete", CommandLineScope, mods("Tab", noShift)...)
	CmdlineCompletePrev = cmd("tui.cmdline.completePrev", CommandLineScope, mods("Tab", withShift)...)
	CmdlineNext         = cmd("tui.cmdline.next", CommandLineScope, mods("Down", anyCombo)...) // the dropdown open
	CmdlinePrev         = cmd("tui.cmdline.prev", CommandLineScope, mods("Up", anyCombo)...)

	cmdlineCommands = []keymap.Command{CmdlineComplete, CmdlineCompletePrev, CmdlineNext, CmdlinePrev}
)

// CommandLineReplaceScope is the keys of a command's replace question (:s with c): Y, N and Q, either
// case, with any modifier, as its handler took them.
const CommandLineReplaceScope keymap.Scope = "tui.cmdline.replace"

var (
	CmdlineReplaceYes  = cmd("tui.cmdline.replace.yes", CommandLineReplaceScope, mods("Y", anyCombo)...)
	CmdlineReplaceNo   = cmd("tui.cmdline.replace.no", CommandLineReplaceScope, mods("N", anyCombo)...)
	CmdlineReplaceQuit = cmd("tui.cmdline.replace.quit", CommandLineReplaceScope, mods("Q", anyCombo)...)

	cmdlineReplaceCommands = []keymap.Command{CmdlineReplaceYes, CmdlineReplaceNo, CmdlineReplaceQuit}
)

// TerminalScope is a Terminal's own keys in input mode, where every other key is the program's: the
// escape prefix (Ctrl+\, then Ctrl+n for normal mode or Ctrl+\ to send it; the key after it is its
// grammar, not a command), Esc into normal mode with WithVimKeys outside a full-screen program, and,
// once the program has exited, Enter or i to start one again.
const TerminalScope keymap.Scope = "tui.terminal"

var (
	TerminalPrefix   = cmd("tui.terminal.prefix", TerminalScope, "Ctrl+\\", "Ctrl+Shift+\\")
	TerminalToNormal = cmd("tui.terminal.normalMode", TerminalScope, "Esc")
	TerminalRestart  = cmd("tui.terminal.restart", TerminalScope, append(mods("Enter", anyCombo), "I")...)

	terminalCommands = []keymap.Command{TerminalPrefix, TerminalToNormal, TerminalRestart}
)

// TerminalNormalScope is a Terminal's normal mode: moving through its scrollback, selecting and
// searching, as Vim reads the keys. The key after g is its grammar (g again: the first line).
const TerminalNormalScope keymap.Scope = "tui.terminal.normal"

var (
	TermHalfUp     = cmd("tui.terminal.normal.halfUp", TerminalNormalScope, "Ctrl+U")
	TermHalfDown   = cmd("tui.terminal.normal.halfDown", TerminalNormalScope, "Ctrl+D")
	TermPageUp     = cmd("tui.terminal.normal.pageUp", TerminalNormalScope, "Ctrl+B", "PgUp", "Shift+PgUp")
	TermPageDown   = cmd("tui.terminal.normal.pageDown", TerminalNormalScope, "Ctrl+F", "PgDown", "Shift+PgDown")
	TermCancel     = cmd("tui.terminal.normal.cancel", TerminalNormalScope, "Esc", "Shift+Esc") // a selection or g; else the host's
	TermInput      = cmd("tui.terminal.normal.input", TerminalNormalScope, "I", "A", "Enter", "Shift+Enter")
	TermLeft       = cmd("tui.terminal.normal.left", TerminalNormalScope, "H", "Left", "Shift+Left")
	TermRight      = cmd("tui.terminal.normal.right", TerminalNormalScope, "L", "Right", "Shift+Right")
	TermDown       = cmd("tui.terminal.normal.down", TerminalNormalScope, "J", "Down", "Shift+Down")
	TermUp         = cmd("tui.terminal.normal.up", TerminalNormalScope, "K", "Up", "Shift+Up")
	TermLineStart  = cmd("tui.terminal.normal.lineStart", TerminalNormalScope, "0", "Home", "Shift+Home")
	TermLineEnd    = cmd("tui.terminal.normal.lineEnd", TerminalNormalScope, "$", "End", "Shift+End")
	TermWord       = cmd("tui.terminal.normal.wordForward", TerminalNormalScope, "W")
	TermWordBack   = cmd("tui.terminal.normal.wordBackward", TerminalNormalScope, "B")
	TermWordEnd    = cmd("tui.terminal.normal.wordEnd", TerminalNormalScope, "E")
	TermGoPrefix   = cmd("tui.terminal.normal.goPrefix", TerminalNormalScope, "G") // then G: the first line
	TermBottom     = cmd("tui.terminal.normal.bottom", TerminalNormalScope, "Shift+G")
	TermVisual     = cmd("tui.terminal.normal.visual", TerminalNormalScope, "V")
	TermVisualLine = cmd("tui.terminal.normal.visualLine", TerminalNormalScope, "Shift+V")
	TermYank       = cmd("tui.terminal.normal.yank", TerminalNormalScope, "Y") // the selection
	TermSearch     = cmd("tui.terminal.normal.search", TerminalNormalScope, "/")
	TermSearchBack = cmd("tui.terminal.normal.searchBackward", TerminalNormalScope, "?")
	TermNext       = cmd("tui.terminal.normal.next", TerminalNormalScope, "N")
	TermPrev       = cmd("tui.terminal.normal.prev", TerminalNormalScope, "Shift+N")

	terminalNormalCommands = []keymap.Command{TermHalfUp, TermHalfDown, TermPageUp, TermPageDown,
		TermCancel, TermInput, TermLeft, TermRight, TermDown, TermUp, TermLineStart, TermLineEnd, TermWord,
		TermWordBack, TermWordEnd, TermGoPrefix, TermBottom, TermVisual, TermVisualLine, TermYank,
		TermSearch, TermSearchBack, TermNext, TermPrev}
)

// TerminalSearchScope is a Terminal's search line in normal mode: it takes every key while open; typed
// text is not a command.
const TerminalSearchScope keymap.Scope = "tui.terminal.search"

var (
	TermSearchClose     = cmd("tui.terminal.search.close", TerminalSearchScope, mods("Esc", anyCombo)...)
	TermSearchRun       = cmd("tui.terminal.search.run", TerminalSearchScope, mods("Enter", anyCombo)...)
	TermSearchBackspace = cmd("tui.terminal.search.backspace", TerminalSearchScope, mods("Backspace", anyCombo)...)

	terminalSearchCommands = []keymap.Command{TermSearchClose, TermSearchRun, TermSearchBackspace}
)

// FloatScope is a modal Float's keys: Esc hides it.
const FloatScope keymap.Scope = "tui.float"

// FloatClose hides a modal Float.
var FloatClose = cmd("tui.float.close", FloatScope, "Esc")

// PopupScope is an anchored popup's keys (a menu's, a select's, a strip's): Esc dismisses it.
const PopupScope keymap.Scope = "tui.popup"

// PopupClose dismisses an anchored popup.
var PopupClose = cmd("tui.popup.close", PopupScope, "Esc")

// ImageScope is a scrollable Image's keys.
const ImageScope keymap.Scope = "tui.image"

var (
	ImageDown     = cmd("tui.image.down", ImageScope, "Down", "J")
	ImageUp       = cmd("tui.image.up", ImageScope, "Up", "K")
	ImageRight    = cmd("tui.image.right", ImageScope, "Right", "L")
	ImageLeft     = cmd("tui.image.left", ImageScope, "Left", "H")
	ImagePageDown = cmd("tui.image.pageDown", ImageScope, "PgDown", "]", "Space")
	ImagePageUp   = cmd("tui.image.pageUp", ImageScope, "PgUp", "[")
	ImageBottom   = cmd("tui.image.bottom", ImageScope, "End", "Shift+G")
	ImageTop      = cmd("tui.image.top", ImageScope, "Home", "G")

	imageCommands = []keymap.Command{ImageDown, ImageUp, ImageRight, ImageLeft, ImagePageDown, ImagePageUp,
		ImageBottom, ImageTop}
)

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

// KeyCommands is every key command golib declares: the runtime's and every widget's, for keymap.Build.
// (Commands is the Editor command line's registry; these are keys.)
func KeyCommands() []keymap.Command {
	out := append([]keymap.Command(nil), tui.Commands()...)
	out = append(out, listCommands...)
	out = append(out, treeCommands...)
	out = append(out, tableCommands...)
	out = append(out, tableFieldCommands...)
	out = append(out, inputCommands...)
	out = append(out, menuCommands...)
	out = append(out, ButtonActivate, WindowMenu, DrawerClose, DrawerCloseOnQ)
	out = append(out, modalCommands...)
	out = append(out, tabsCommands...)
	out = append(out, tabStripCommands...)
	out = append(out, SplitCancel, SplitLeft, SplitRight, SplitUp, SplitDown)
	out = append(out, resizableCommands...)
	out = append(out, SelectOpen)
	out = append(out, selectPopupCommands...)
	out = append(out, textAreaCommands...)
	out = append(out, htmlViewCommands...)
	out = append(out, bufferViewCommands...)
	out = append(out, fileViewCommands...)
	out = append(out, cmdlineCommands...)
	out = append(out, cmdlineReplaceCommands...)
	out = append(out, terminalCommands...)
	out = append(out, terminalNormalCommands...)
	out = append(out, terminalSearchCommands...)
	out = append(out, FloatClose, PopupClose)
	out = append(out, imageCommands...)
	out = append(out, EditorCommands()...)
	return out
}
