package widget

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
}
