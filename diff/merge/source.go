// Package merge is what a merge view merges and the state of the merge, apart from any renderer:
// a MergeSource gives the view its texts and takes its result, and a Model holds the hunks, the
// result and how each change was resolved. The terminal and the GUI views share it, and a source
// is tested without a view.
package merge

import (
	"context"
	"errors"
	"strings"
)

// MergeSource is what a merge view merges: its texts in, its result out. A view never loads or
// writes a text itself, so a buffer against its file, a remote object, or git's stages are each a
// source, and the view is the same for all of them.
type MergeSource interface {
	// Load answers the texts and where each came from. A text that cannot be merged as lines
	// (binary, undecodable) is ErrNotText, which the view shows in place of the panes.
	Load(ctx context.Context) (Input, error)
	// Apply takes the resolved result and writes it where the source keeps it (a buffer, the
	// working tree). An error keeps the view open, said in its bar.
	Apply(ctx context.Context, result string) error
	// Changed reports that a side changed under the view (the file on disk again, a new commit).
	// The view offers to load again. A nil channel is a source that never changes.
	Changed() <-chan struct{}
}

// ErrNotText is a source's answer for a text that cannot be merged as lines.
var ErrNotText = errors.New("merge: not text")

// Input is a merge's texts. A two-way merge has no Base and HasBase false.
type Input struct {
	Base, Left, Right string
	HasBase           bool
	// BaseFrom, LeftFrom and RightFrom are where each text came from; ResultTo is where Apply
	// writes. ResultTo equal to LeftFrom or RightFrom names the side the result replaces: an
	// unresolved two-way conflict keeps that side's lines, and the result is written with its line
	// endings.
	BaseFrom, LeftFrom, RightFrom Origin
	ResultTo                      Origin
}

// OriginKind is what kind of place a text came from.
type OriginKind uint8

const (
	// Buffer is an open document, its Location the buffer's name and its Revision its version.
	Buffer OriginKind = iota
	// File is a local file, its Location the path and its Revision its mtime or a digest.
	File
	// Remote is an object elsewhere (gs://bucket/key, s3://…, https://…), its Revision the
	// object's generation or etag.
	Remote
	// Git is a git object, its Location the repository-relative path and its Revision a commit or a
	// ref.
	Git
)

// String is the kind as one lower-case word: "buffer", "file", "remote" or "git". A view
// translates it before showing it.
func (k OriginKind) String() string {
	switch k {
	case File:
		return "file"
	case Remote:
		return "remote"
	case Git:
		return "git"
	}
	return "buffer"
}

// Origin is where a text came from, so a view can say so in the pane's header and its details.
// The view never fetches from it: loading is the source's.
type Origin struct {
	Kind     OriginKind
	Location string
	Revision string
	// Label is a short header text, as the source words it ("On disk", "Your unsaved edits").
	Label string
}

// Title is the origin's header text: its Label, else its Location, else its short revision.
func (o Origin) Title() string {
	switch {
	case o.Label != "":
		return o.Label
	case o.Location != "":
		return o.Location
	}
	return o.ShortRevision()
}

// ShortRevision is the revision as a header shows it: a git commit's hash cut to seven characters,
// any other revision as it is.
func (o Origin) ShortRevision() string {
	if o.Kind == Git && len(o.Revision) > 7 && strings.Trim(o.Revision, "0123456789abcdefABCDEF") == "" {
		return o.Revision[:7]
	}
	return o.Revision
}
