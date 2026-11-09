package core

// frame.go: the one border painter. Box, the modal card, a menu level, the command line's dropdown,
// a toast and the tile drop preview all frame themselves through it, so each honours the
// style.BorderStyle its style carries (a theme's ASCII, double or rounded border) rather than glyphs
// of its own.
