// Package diff compares texts by line: a two-way diff (Lines) and a merge of two sides against
// their common base, or of two sides alone (Merge3, Merge2), into hunks a view or a tool resolves
// (Resolve). It is pure: no UI, no files, no processes.
//
// A text is split into lines with its line ending and final newline kept (Split, Text), never
// normalised: a merge joins its result with the ending of the side it writes to (Text.Join).
//
// Lines finds the edits with the histogram heuristic (anchoring on the rarest lines first, as git's
// histogram diff does), falling back to Myers' algorithm where no rare line anchors a region. Past
// a size or time bound (Options) the diff of what remains is coarse: one block replaced by
// another, still correct, and said so (Diff.Coarse).
package diff
