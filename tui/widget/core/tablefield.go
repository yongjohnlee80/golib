package core

import "github.com/yongjohnlee80/golib/tui/style"

// TableField is one column as a source names it.
type TableField struct {
	Title string
	// Width is the column's width in cells; 0 measures it from the title and the loaded cells.
	Width int
	// Align is where a cell's text sits in its column; a Markdown delimiter's :--: says which.
	Align style.Align
}
