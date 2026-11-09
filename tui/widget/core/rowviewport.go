package widget

// rowviewport.go: the arithmetic of a column of rows seen through a window of them — List, Tree,
// the Select popup and DataTable's rows all scroll by it, so a fix to scrolling is made once.
// What a widget counts as its rows and its window height is its own (a header, borders, a filter
// line); these functions take both.
