package decltest

import (
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// MINIMAL DECLARATIONS — one per standard type: the smallest document that mounts it, for CheckStyle
// to mount a style's replacement in. Each is a Window's body; the type it is for is declared in it
// once, with `id: subject`. A Window's own entry is its body, the Window being the subject.
// TestEveryStandardTypeHasAMinimalDeclaration keeps the table complete, and each entry mounting
// under the standard vocabulary.

// minimal is a type's declaration and what its program needs beyond the document.
type minimal struct {
	body string
	opts func() []tuidecl.ProgramOption
}

// minimals are the standard types' minimal declarations, by type.
var minimals = map[string]minimal{
	"Window":            {body: `Text { text: "x" }`},
	"MenuBar":           {body: `MenuBar { id: subject; Dock.edge: Tui.Top; Menu { title: "&File"; MenuItem { text: "&Open" } } }`},
	"Menu":              {body: `MenuBar { Dock.edge: Tui.Top; Menu { id: subject; title: "&File"; MenuItem { text: "&Open" } } }`},
	"MenuItem":          {body: `MenuBar { Dock.edge: Tui.Top; Menu { title: "&File"; MenuItem { id: subject; text: "&Open" } } }`},
	"MenuSeparator":     {body: `MenuBar { Dock.edge: Tui.Top; Menu { title: "&File"; MenuItem { text: "&Open" } MenuSeparator { id: subject } MenuItem { text: "&Save" } } }`},
	"Shortcut":          {body: `Shortcut { id: subject; sequence: "Ctrl+Q" } Text { text: "x" }`},
	"FocusScope":        {body: `FocusScope { id: subject; Text { text: "x" } }`},
	"Frame":             {body: `Frame { id: subject; Text { text: "x" } }`},
	"ScrollView":        {body: `ScrollView { id: subject; Text { text: "x" } }`},
	"Editor":            {body: `Editor { id: subject }`},
	"Image":             {body: `Image { id: subject }`},
	"HTMLView":          {body: `HTMLView { id: subject }`},
	"SyntaxHighlighter": {body: `Editor { SyntaxHighlighter { id: subject; definition: "Go" } }`},
	"StatusBar":         {body: `StatusBar { id: subject; Text { text: "x" } }`},
	"Text":              {body: `Text { id: subject; text: "x" }`},
	"Terminal":          {body: `Terminal { id: subject }`},
	"TerminalTabs":      {body: `TerminalTabs { id: subject }`},
	"Button":            {body: `Button { id: subject; text: "b" }`},
	"CheckBox":          {body: `CheckBox { id: subject; text: "c" }`},
	"Split":             {body: `Split { id: subject; Text { text: "a" } Text { text: "b" } }`},
	"Flex":              {body: `Flex { id: subject; Text { text: "x" } }`},
	"Flow":              {body: `Flow { id: subject; Text { text: "x" } }`},
	"Dialog":            {body: `Text { text: "x" } Dialog { id: subject; title: "d"; Text { text: "y" } }`},
	"DialogButtonBox": {body: `Text { text: "x" } Dialog { title: "d"; Text { text: "y" }
  DialogButtonBox { id: subject; Button { text: "&OK"; DialogButtonBox.buttonRole: DialogButtonBox.AcceptRole } } }`},
	"ListView":         {body: `ListView { id: subject }`},
	"ComboBox":         {body: `ComboBox { id: subject }`},
	"TableView":        {body: `TableView { id: subject }`},
	"TableViewColumn":  {body: `TableView { TableViewColumn { id: subject; role: "a"; title: "A" } }`},
	"TabView":          {body: `TabView { id: subject; Tab { title: "t"; Text { text: "x" } } }`},
	"Tab":              {body: `TabView { Tab { id: subject; title: "t"; Text { text: "x" } } }`},
	"TreeView":         {body: `TreeView { id: subject }`},
	"FileDialog":       {body: `Text { text: "x" } FileDialog { id: subject; title: "f" }`},
	"FolderDialog":     {body: `Text { text: "x" } FolderDialog { id: subject; title: "f" }`},
	"Drawer":           {body: `Text { text: "x" } Drawer { id: subject; Text { text: "y" } }`},
	"FloatWindow":      {body: `Text { text: "x" } FloatWindow { id: subject; Text { text: "y" } }`},
	"TileManager":      {body: `TileManager { id: subject; Tile { edge: Tui.Center; Text { text: "x" } } }`},
	"Tile":             {body: `TileManager { Tile { id: subject; edge: Tui.Center; Text { text: "x" } } }`},
	"MarkdownRenderer": {body: `Editor { MarkdownRenderer { id: subject } }`},
	"HTMLDocumentView": {body: `Editor { HTMLDocumentView { id: subject } }`},
	"WindowMod":        {body: `WindowMod { id: subject; Text { text: "x" } }`},
	"WindowButtons":    {body: `WindowMod { Flex { WindowButtons { id: subject } } }`},
	"WindowTaskbar": {body: `WindowTaskbar { id: subject } Text { text: "x" }`,
		opts: func() []tuidecl.ProgramOption {
			return []tuidecl.ProgramOption{tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows()))}
		}},
}

// minimalDocument is typ's minimal declaration as a document: in a Window that carries a palette,
// so the type's Restyle runs with a non-empty one. A Window's own is the Window, given the id.
func minimalDocument(typ string, m minimal) []byte {
	id := "window"
	if typ == "Window" {
		id = "subject"
	}
	return []byte("import tui 1.0\nWindow { id: " + id + "; palette.window: \"#202020\"; palette.text: \"#e0e0e0\"\n" + m.body + "\n}")
}
