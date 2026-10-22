package widget

import "fmt"

// editor_actions.go: what each Action IS, one row per action: its name, its description for a help
// card, the binding classes that accept it, and whether it changes the text. String, Description,
// actionModes and mutatingAction read this table, so a new action is one row here (and its key
// bindings and behaviour), not an edit in each of them. A test holds every action below actMax to
// having a row.

// modeSet is the binding classes an action is accepted in.
type modeSet uint8

const (
	inNormal modeSet = 1 << iota
	inVisual
	inInsert
)

// actionSpec is what an Action is, apart from what it does.
type actionSpec struct {
	name, desc string
	modes      modeSet    // where a key may be bound to it; none: it is bindable nowhere
	mutating   bool       // it changes the text, so a read-only core refuses it
	exec       actionExec // which part of the core runs it (execAction); actNone: none does
}

// actionExec is the part of the core that runs an action.
type actionExec uint8

const (
	actNone         actionExec = iota // run by no part of the core (ActUnbound, the operator prefixes)
	actMotion                         // a cursor motion (execMotion)
	actInsertEntry                    // a way into Insert (execInsertEntry)
	actEdit                           // a Normal-mode edit, undo or redo (execEdit)
	actVisualMode                     // entering, leaving or acting on Visual (execVisual)
	actInsertAssist                   // an Insert-mode assist (execInsertAssist)
	actInsertEdit                     // an Insert editing key: Tab, Enter, Backspace, Delete (execInsertEdit)
	actOther                          // the clipboard, the command line, find and the rest (execOther)
)

var actionSpecs = [actMax]actionSpec{
	ActUnbound:          {name: "Unbound", desc: "Unbound key (bubbles to parent component)"},
	ActLeft:             {name: "Left", desc: "Move cursor left", modes: inNormal | inVisual | inInsert, exec: actMotion},
	ActDown:             {name: "Down", desc: "Move cursor down", modes: inNormal | inVisual | inInsert, exec: actMotion},
	ActUp:               {name: "Up", desc: "Move cursor up", modes: inNormal | inVisual | inInsert, exec: actMotion},
	ActRight:            {name: "Right", desc: "Move cursor right", modes: inNormal | inVisual | inInsert, exec: actMotion},
	ActLineStart:        {name: "LineStart", desc: "Move cursor to start of line", modes: inNormal | inVisual | inInsert, exec: actMotion},
	ActLineEnd:          {name: "LineEnd", desc: "Move cursor to end of line", modes: inNormal | inVisual | inInsert, exec: actMotion},
	ActWordForward:      {name: "WordForward", desc: "Move cursor to start of next word", modes: inNormal | inVisual | inInsert, exec: actMotion},
	ActWordBack:         {name: "WordBack", desc: "Move cursor to start of previous word", modes: inNormal | inVisual | inInsert, exec: actMotion},
	ActWordEnd:          {name: "WordEnd", desc: "Move cursor to end of current word", modes: inNormal | inVisual | inInsert, exec: actMotion},
	ActParaForward:      {name: "ParaForward", desc: "Move cursor to next paragraph boundary", modes: inNormal | inVisual | inInsert, exec: actMotion},
	ActParaBack:         {name: "ParaBack", desc: "Move cursor to previous paragraph boundary", modes: inNormal | inVisual | inInsert, exec: actMotion},
	ActGoBottom:         {name: "GoBottom", desc: "Move cursor to bottom line", modes: inNormal | inVisual},
	ActPageUp:           {name: "PageUp", desc: "Scroll and move cursor up one page", modes: inNormal | inVisual | inInsert, exec: actMotion},
	ActPageDown:         {name: "PageDown", desc: "Scroll and move cursor down one page", modes: inNormal | inVisual | inInsert, exec: actMotion},
	ActDeletePrefix:     {name: "DeletePrefix", desc: "Arm linewise delete prefix (dd)", modes: inNormal, mutating: true},
	ActYankPrefix:       {name: "YankPrefix", desc: "Arm linewise yank prefix (yy)", modes: inNormal},
	ActGoPrefix:         {name: "GoPrefix", desc: "Arm jump to line prefix (gg)", modes: inNormal | inVisual},
	ActInsert:           {name: "Insert", desc: "Enter Insert mode before cursor", modes: inNormal, mutating: true, exec: actInsertEntry},
	ActAppend:           {name: "Append", desc: "Enter Insert mode after cursor", modes: inNormal, mutating: true, exec: actInsertEntry},
	ActInsertLineStart:  {name: "InsertLineStart", desc: "Enter Insert mode at start of line", modes: inNormal, mutating: true, exec: actInsertEntry},
	ActAppendLineEnd:    {name: "AppendLineEnd", desc: "Enter Insert mode at end of line", modes: inNormal, mutating: true, exec: actInsertEntry},
	ActOpenBelow:        {name: "OpenBelow", desc: "Open new line below and enter Insert mode", modes: inNormal, mutating: true, exec: actInsertEntry},
	ActOpenAbove:        {name: "OpenAbove", desc: "Open new line above and enter Insert mode", modes: inNormal, mutating: true, exec: actInsertEntry},
	ActDeleteChar:       {name: "DeleteChar", desc: "Delete character at cursor", modes: inNormal, mutating: true, exec: actEdit},
	ActDeleteToEnd:      {name: "DeleteToEnd", desc: "Delete characters to end of line", modes: inNormal, mutating: true, exec: actEdit},
	ActPasteAfter:       {name: "PasteAfter", desc: "Paste register content after cursor", modes: inNormal, mutating: true, exec: actEdit},
	ActPasteBefore:      {name: "PasteBefore", desc: "Paste register content before cursor", modes: inNormal, mutating: true, exec: actEdit},
	ActUndo:             {name: "Undo", desc: "Undo previous edit", modes: inNormal | inInsert, mutating: true, exec: actEdit},
	ActRedo:             {name: "Redo", desc: "Redo previously undone edit", modes: inNormal | inInsert, mutating: true, exec: actEdit},
	ActVisual:           {name: "Visual", desc: "Toggle character-wise Visual mode", modes: inNormal | inVisual, exec: actVisualMode},
	ActVisualLine:       {name: "VisualLine", desc: "Toggle line-wise Visual mode", modes: inNormal | inVisual, exec: actVisualMode},
	ActVisualYank:       {name: "VisualYank", desc: "Yank selection to register and system clipboard", modes: inVisual, exec: actVisualMode},
	ActVisualDelete:     {name: "VisualDelete", desc: "Delete selection to register", modes: inVisual, mutating: true, exec: actVisualMode},
	ActCut:              {name: "Cut", desc: "Cut selection or current line to register", modes: inNormal | inVisual | inInsert, mutating: true, exec: actOther},
	ActCopy:             {name: "Copy", desc: "Copy selection or current line to register", modes: inNormal | inVisual | inInsert, exec: actOther},
	ActPaste:            {name: "Paste", desc: "Paste register content at cursor", modes: inNormal | inVisual | inInsert, mutating: true, exec: actOther},
	ActSelectAll:        {name: "SelectAll", desc: "Select entire buffer content", modes: inNormal | inVisual | inInsert, exec: actOther},
	ActEscape:           {name: "Escape", desc: "Escape to Normal mode", modes: inNormal | inVisual | inInsert},
	ActToggleRendered:   {name: "ToggleRendered", desc: "Switch between the Rendered view and Raw", modes: inNormal | inVisual | inInsert, exec: actOther},
	ActFindNext:         {name: "FindNext", desc: "Go to the find's next match", modes: inNormal | inVisual, exec: actOther},
	ActFindPrev:         {name: "FindPrev", desc: "Go to the find's previous match", modes: inNormal | inVisual, exec: actOther},
	ActCommandLine:      {name: "CommandLine", desc: "Open the command line", modes: inNormal | inVisual, exec: actOther},
	ActLineDownFirst:    {name: "LineDownFirst", desc: "Next line, first non-blank", modes: inNormal | inVisual, exec: actMotion},
	ActLineUpFirst:      {name: "LineUpFirst", desc: "Previous line, first non-blank", modes: inNormal | inVisual, exec: actMotion},
	ActFirstNonBlank:    {name: "FirstNonBlank", desc: "First non-blank", modes: inNormal | inVisual, exec: actMotion},
	ActLeftWrap:         {name: "LeftWrap", desc: "Left, across lines", modes: inNormal | inVisual, exec: actMotion},
	ActFindCharForward:  {name: "FindCharForward", desc: "Find a character forward", modes: inNormal | inVisual},
	ActFindCharBack:     {name: "FindCharBack", desc: "Find a character back", modes: inNormal | inVisual},
	ActTillCharForward:  {name: "TillCharForward", desc: "Till a character forward", modes: inNormal | inVisual},
	ActTillCharBack:     {name: "TillCharBack", desc: "Till a character back", modes: inNormal | inVisual},
	ActFindCharRepeat:   {name: "FindCharRepeat", desc: "Repeat the character find", modes: inNormal | inVisual, exec: actMotion},
	ActFindCharReverse:  {name: "FindCharReverse", desc: "Repeat the character find, reversed", modes: inNormal | inVisual, exec: actMotion},
	ActMatchPair:        {name: "MatchPair", desc: "Matching bracket", modes: inNormal | inVisual},
	ActViewTop:          {name: "ViewTop", desc: "Top of the view", modes: inNormal | inVisual, exec: actMotion},
	ActViewMiddle:       {name: "ViewMiddle", desc: "Middle of the view", modes: inNormal | inVisual, exec: actMotion},
	ActViewBottom:       {name: "ViewBottom", desc: "Bottom of the view", modes: inNormal | inVisual, exec: actMotion},
	ActHalfPageDown:     {name: "HalfPageDown", desc: "Half a page down", modes: inNormal | inVisual},
	ActHalfPageUp:       {name: "HalfPageUp", desc: "Half a page up", modes: inNormal | inVisual},
	ActWORDForward:      {name: "WORDForward", desc: "Next WORD", modes: inNormal | inVisual, exec: actMotion},
	ActWORDBack:         {name: "WORDBack", desc: "Previous WORD", modes: inNormal | inVisual, exec: actMotion},
	ActWORDEnd:          {name: "WORDEnd", desc: "End of WORD", modes: inNormal | inVisual, exec: actMotion},
	ActWordEndBack:      {name: "WordEndBack", desc: "End of the previous word", modes: inNormal | inVisual, exec: actMotion},
	ActWORDEndBack:      {name: "WORDEndBack", desc: "End of the previous WORD", modes: inNormal | inVisual, exec: actMotion},
	ActChangePrefix:     {name: "ChangePrefix", desc: "Change", modes: inNormal, mutating: true},
	ActChangeToEnd:      {name: "ChangeToEnd", desc: "Change to end of line", modes: inNormal, mutating: true, exec: actEdit},
	ActSubstChar:        {name: "SubstChar", desc: "Change the character", modes: inNormal, mutating: true, exec: actEdit},
	ActSubstLine:        {name: "SubstLine", desc: "Change the line", modes: inNormal, mutating: true, exec: actEdit},
	ActYankToEnd:        {name: "YankToEnd", desc: "Copy to end of line", modes: inNormal, exec: actEdit},
	ActReplaceChar:      {name: "ReplaceChar", desc: "Replace the character", modes: inNormal, mutating: true},
	ActJoin:             {name: "Join", desc: "Join lines", modes: inNormal, mutating: true, exec: actEdit},
	ActJoinRaw:          {name: "JoinRaw", desc: "Join lines as they are", modes: inNormal, mutating: true, exec: actEdit},
	ActToggleCase:       {name: "ToggleCase", desc: "Toggle case", modes: inNormal, mutating: true, exec: actEdit},
	ActCaseToggleOp:     {name: "CaseToggleOp", desc: "Toggle case of the text", modes: inNormal, mutating: true},
	ActLowerOp:          {name: "LowerOp", desc: "Lowercase the text", modes: inNormal, mutating: true},
	ActUpperOp:          {name: "UpperOp", desc: "Uppercase the text", modes: inNormal, mutating: true},
	ActIndentPrefix:     {name: "IndentPrefix", desc: "Indent", modes: inNormal, mutating: true},
	ActOutdentPrefix:    {name: "OutdentPrefix", desc: "Outdent", modes: inNormal, mutating: true},
	ActRepeat:           {name: "Repeat", desc: "Repeat the last change", modes: inNormal},
	ActRegisterPrefix:   {name: "RegisterPrefix", desc: "Name the register", modes: inNormal | inVisual},
	ActMarkSet:          {name: "MarkSet", desc: "Set a mark", modes: inNormal | inVisual},
	ActMarkLine:         {name: "MarkLine", desc: "Go to a mark's line", modes: inNormal | inVisual},
	ActMarkExact:        {name: "MarkExact", desc: "Go to a mark", modes: inNormal | inVisual},
	ActMacroRecord:      {name: "MacroRecord", desc: "Record a macro", modes: inNormal},
	ActMacroPlay:        {name: "MacroPlay", desc: "Play a macro", modes: inNormal},
	ActJumpBack:         {name: "JumpBack", desc: "Jump back", modes: inNormal},
	ActJumpForward:      {name: "JumpForward", desc: "Jump forward", modes: inNormal},
	ActVisualChange:     {name: "VisualChange", desc: "Change the selection", modes: inVisual, mutating: true, exec: actVisualMode},
	ActVisualReplace:    {name: "VisualReplace", desc: "Replace the selection's characters", modes: inVisual, mutating: true},
	ActVisualToggleCase: {name: "VisualToggleCase", desc: "Toggle the selection's case", modes: inVisual, mutating: true, exec: actVisualMode},
	ActVisualLower:      {name: "VisualLower", desc: "Lowercase the selection", modes: inVisual, mutating: true, exec: actVisualMode},
	ActVisualUpper:      {name: "VisualUpper", desc: "Uppercase the selection", modes: inVisual, mutating: true, exec: actVisualMode},
	ActVisualIndent:     {name: "VisualIndent", desc: "Indent the selection", modes: inVisual, mutating: true, exec: actVisualMode},
	ActVisualOutdent:    {name: "VisualOutdent", desc: "Outdent the selection", modes: inVisual, mutating: true, exec: actVisualMode},
	ActVisualOther:      {name: "VisualOther", desc: "Go to the selection's other end", modes: inVisual, exec: actVisualMode},
	ActDeleteWordBack:   {name: "DeleteWordBack", desc: "Delete the word before the cursor", modes: inInsert, mutating: true, exec: actInsertAssist},
	ActDeleteLineBack:   {name: "DeleteLineBack", desc: "Delete back to the indent", modes: inInsert, mutating: true, exec: actInsertAssist},
	ActInsertOneShot:    {name: "InsertOneShot", desc: "Run one Normal-mode command", modes: inInsert, exec: actInsertAssist},
	ActVisualBlock:      {name: "VisualBlock", desc: "Select a block", modes: inNormal | inVisual, exec: actVisualMode},
	ActVisualOtherCol:   {name: "VisualOtherCol", desc: "Go to the block's other column", modes: inVisual, exec: actVisualMode},
	ActVisualInsert:     {name: "VisualInsert", desc: "Insert before the block", modes: inVisual, mutating: true, exec: actVisualMode},
	ActVisualAppend:     {name: "VisualAppend", desc: "Append after the block", modes: inVisual, mutating: true, exec: actVisualMode},
	ActSentenceForward:  {name: "SentenceForward", desc: "Next sentence", modes: inNormal | inVisual, exec: actMotion},
	ActSentenceBack:     {name: "SentenceBack", desc: "Previous sentence", modes: inNormal | inVisual, exec: actMotion},
	ActStarForward:      {name: "StarForward", desc: "Find the word under the cursor", modes: inNormal},
	ActStarBack:         {name: "StarBack", desc: "Find the word under the cursor, back", modes: inNormal},
	ActReplaceMode:      {name: "ReplaceMode", desc: "Replace mode", modes: inNormal, mutating: true, exec: actInsertAssist},
	ActInsertTab:        {name: "InsertTab", desc: "Insert a tab (in a table, the next cell; Shift, the previous)", modes: inInsert, mutating: true, exec: actInsertEdit},
	ActNewline:          {name: "Newline", desc: "Break the line, keeping its indent", modes: inInsert, mutating: true, exec: actInsertEdit},
	ActBackspace:        {name: "Backspace", desc: "Delete the character before the cursor", modes: inInsert, mutating: true, exec: actInsertEdit},
	ActDeleteForward:    {name: "DeleteForward", desc: "Delete the character under the cursor", modes: inInsert, mutating: true, exec: actInsertEdit},
}

// Action is one enumerated Editor operation a chord can bind.
type Action uint8

const (
	// ActUnbound removes a default binding (overlay-only sentinel).
	ActUnbound Action = iota

	// Motions (Normal + Visual, count-prefixable).
	ActLeft
	ActDown
	ActUp
	ActRight
	ActLineStart
	ActLineEnd
	ActWordForward
	ActWordBack
	ActWordEnd
	ActParaForward
	ActParaBack
	ActGoBottom
	ActPageUp
	ActPageDown

	// Double-key prefixes (the one-key pending buffer).
	ActDeletePrefix // d → dd
	ActYankPrefix   // y → yy
	ActGoPrefix     // g → gg

	// Insert entries (Normal only).
	ActInsert          // i
	ActAppend          // a
	ActInsertLineStart // I
	ActAppendLineEnd   // A
	ActOpenBelow       // o
	ActOpenAbove       // O

	// Edits (Normal only).
	ActDeleteChar  // x
	ActDeleteToEnd // D
	ActPasteAfter  // p
	ActPasteBefore // P
	ActUndo        // u
	ActRedo        // Ctrl-R

	// Visual-mode entry/exit and operations.
	ActVisual       // v (Normal: enter; Visual: exit)
	ActVisualLine   // V (Normal: enter; Visual: switch/exit)
	ActVisualYank   // y in visual
	ActVisualDelete // d / x in visual

	// General non-modal & standard editor actions (Nano / TextEdit).
	ActCut       // Cut selection or current line (Nano Ctrl+K, Standard Ctrl+X)
	ActCopy      // Copy selection or line (Standard Ctrl+C)
	ActPaste     // Paste at cursor (Nano Ctrl+U, Standard Ctrl+V)
	ActSelectAll // Select entire buffer (Standard Ctrl+A)
	ActEscape    // Escape / return to Normal mode

	// ActToggleRendered switches an editor with a Rendered view between it and Raw (Ctrl+T).
	// An editor without one does not take it: the key bubbles.
	ActToggleRendered

	// ActFindNext and ActFindPrev go to the active find's next and previous match (n, N in the Vim
	// keyset), wrapping. With no active find the key is not consumed: it reaches the application.
	ActFindNext
	ActFindPrev

	// ActCommandLine opens the editor's ':' prompt (the Vim keyset's ':'), when its widget has
	// one (SetCommandLine). Without one the key is not consumed.
	ActCommandLine

	// Vim line and character motions (Normal + Visual, count-prefixable): appended, so no
	// earlier action's value moves.
	ActLineDownFirst   // Enter and + (Vim): count lines down, to the first non-blank
	ActLineUpFirst     // - (Vim): count lines up, to the first non-blank
	ActFirstNonBlank   // ^ (Vim): the line's first non-blank
	ActLeftWrap        // Backspace (Vim): left, onto the previous line's end
	ActFindCharForward // f{char} (Vim): the countth {char} after the cursor on the line
	ActFindCharBack    // F{char} (Vim): the countth {char} before the cursor on the line
	ActTillCharForward // t{char} (Vim): just before the countth {char} after the cursor
	ActTillCharBack    // T{char} (Vim): just after the countth {char} before the cursor
	ActFindCharRepeat  // ; (Vim): the last f, F, t or T again
	ActFindCharReverse // , (Vim): the last f, F, t or T, the other way
	ActMatchPair       // % (Vim): the bracket matching the one at or after the cursor; N% goes to N% of the text
	ActViewTop         // H (Vim): the countth line from the view's top
	ActViewMiddle      // M (Vim): the view's middle line
	ActViewBottom      // L (Vim): the countth line from the view's bottom
	ActHalfPageDown    // Ctrl-d (Vim): half a page down, or count lines
	ActHalfPageUp      // Ctrl-u (Vim): half a page up, or count lines
	ActWORDForward     // W (Vim): the next whitespace-delimited WORD
	ActWORDBack        // B (Vim): the previous WORD's start
	ActWORDEnd         // E (Vim): the WORD's end
	ActWordEndBack     // ge (Vim): back to the previous word's end
	ActWORDEndBack     // gE (Vim): back to the previous WORD's end
	ActChangePrefix    // c{motion}, cc (Vim): change — delete, then type in its place
	ActChangeToEnd     // C (Vim): change to the line's end (c$)
	ActSubstChar       // s (Vim): change count characters (cl)
	ActSubstLine       // S (Vim): change the line (cc)
	ActYankToEnd       // Y (Vim, as Neovim maps it): yank to the line's end (y$)
	ActReplaceChar     // r{char} (Vim): replace count clusters with {char}
	ActJoin            // J (Vim): join count lines, a blank between
	ActJoinRaw         // gJ (Vim): join count lines as they are
	ActToggleCase      // ~ (Vim): toggle the case of count clusters, and move past them
	ActCaseToggleOp    // g~{motion} (Vim): toggle the case of the text
	ActLowerOp         // gu{motion} (Vim): lowercase the text
	ActUpperOp         // gU{motion} (Vim): uppercase the text
	ActIndentPrefix    // >{motion}, >> (Vim): indent the lines by one unit
	ActOutdentPrefix   // <{motion}, << (Vim): outdent the lines by one unit
	ActRepeat          // . (Vim): repeat the last change
	ActRegisterPrefix  // "{reg} (Vim): name the register the next yank, delete or put uses
	ActMarkSet         // m{a-z} (Vim): set a mark at the cursor
	ActMarkLine        // '{a-z} (Vim): to the mark's line, first non-blank ('' the previous jump)
	ActMarkExact       // `{a-z} (Vim): to the mark's exact place (`` the previous jump)
	ActMacroRecord     // q{a-z} … q (Vim): record a macro into the register (A-Z append)
	ActMacroPlay       // @{a-z} (Vim): play the register's macro (@@ the last played)
	ActJumpBack        // Ctrl-o (Vim): back along the jump list
	ActJumpForward     // Ctrl-i (Vim): forward along the jump list

	ActVisualChange     // c, s (Vim Visual): change the selection
	ActVisualReplace    // r{c} (Vim Visual): every character of the selection becomes c
	ActVisualToggleCase // ~ (Vim Visual): toggle the selection's case
	ActVisualLower      // u (Vim Visual): lowercase the selection
	ActVisualUpper      // U (Vim Visual): uppercase the selection
	ActVisualIndent     // > (Vim Visual): indent the selected lines
	ActVisualOutdent    // < (Vim Visual): outdent the selected lines
	ActVisualOther      // o (Vim Visual): to the selection's other end

	ActDeleteWordBack // Ctrl-w (Vim Insert): delete the word before the cursor
	ActDeleteLineBack // Ctrl-u (Vim Insert): delete back to the indent, then the line's start
	ActInsertOneShot  // Ctrl-o (Vim Insert): one Normal-mode command, then Insert again

	ActVisualBlock    // Ctrl-v (Vim Normal: enter Visual block; Visual: switch/exit)
	ActVisualOtherCol // O (Vim Visual): in a block, the cursor and the anchor swap columns
	ActVisualInsert   // I (Vim Visual): Insert before the block, on each line; else at the first line's start
	ActVisualAppend   // A (Vim Visual): Insert after the block, on each line; else at the last line's end

	ActSentenceForward // ) (Vim): to the next sentence's start
	ActSentenceBack    // ( (Vim): to this sentence's start, or the previous one's
	ActStarForward     // * (Vim): the whole word under the cursor, found on
	ActStarBack        // # (Vim): the whole word under the cursor, found back
	ActReplaceMode     // R (Vim): Replace mode, typing overwrites

	// Insert's editing keys, as commands (appended, so every value above is kept).
	ActInsertTab     // Tab (Insert): a tab; in a table, the next cell (Shift: the previous)
	ActNewline       // Enter (Insert): break the line, keeping its indent
	ActBackspace     // Backspace (Insert): delete the cluster before the cursor
	ActDeleteForward // Delete (Insert): delete the cluster under the cursor

	actMax // sentinel for validation
)

// String returns the canonical identifier name for an Action.
func (a Action) String() string {
	if a < actMax {
		return actionSpecs[a].name
	}
	return fmt.Sprintf("Action(%d)", a)
}

// Description returns a concise human-readable explanation of the action.
func (a Action) Description() string {
	if a < actMax {
		return actionSpecs[a].desc
	}
	return "Custom action"
}

// actionModes reports which binding classes accept an action.
func actionModes(a Action) (normal, visual, insert bool) {
	if a >= actMax {
		return false, false, false
	}
	m := actionSpecs[a].modes
	return m&inNormal != 0, m&inVisual != 0, m&inInsert != 0
}
