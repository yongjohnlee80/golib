package pty

// Cmd describes the program to start.
type Cmd struct {
	// Path is the program, looked up in PATH when it has no slash.
	Path string
	// Args are the program's arguments, not counting the program itself.
	Args []string
	// Env is the child's environment; nil inherits the caller's.
	Env []string
	// Dir is the child's working directory; empty keeps the caller's.
	Dir string
	// Rows and Cols are the terminal's starting size; zero means 24 x 80.
	Rows, Cols int
}

const (
	defaultRows = 24
	defaultCols = 80
)

func (c Cmd) size() (rows, cols int) {
	rows, cols = c.Rows, c.Cols
	if rows <= 0 {
		rows = defaultRows
	}
	if cols <= 0 {
		cols = defaultCols
	}
	return rows, cols
}
