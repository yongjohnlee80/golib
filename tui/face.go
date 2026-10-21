package tui

import (
	"slices"

	"github.com/yongjohnlee80/golib/logger"
)

// CELL FACES — a subtree's cells drawn in a font family of their own, at the window's cell size:
// a terminal in its own monospace, say, beside the page's.
//
// A CellFacer names its family; while the App paints it and everything under it, each cell
// written carries that family as Cell.Face, an index into the App's face table. A node painted
// later — a menu, a dialog, a tooltip over it — writes face 0, the window's, so it wins cell by
// cell as its colours do, and its going repaints the cells beneath through the ordinary diff.
//
// The table is interned by family and never reuses an index, so a family changed is a Cell
// changed. It is bounded (maxCellFaces): past it a family is drawn in the window's face, and said
// once. A backend that draws fonts (FaceHost) is handed the table when it starts and whenever it
// grows; every other backend has no font to change and ignores Face.

// faceTable is the font faces cells name: faceNames[i] is Cell.Face i, faceIDs its index by family;
// faceFull is the table refusing a family, said once. offFace is the face in effect inside a
// RenderOffscreen's draw (offDepth > 0), for a nested call with no face of its own (offscreen.go).
type faceTable struct {
	faceNames []string
	faceIDs   map[string]uint16
	faceFull  bool
	offFace   uint16
	offDepth  int
}

// CellFacer is a component whose subtree's cells are drawn in another font family. "" is no
// family of its own: the subtree keeps the face it is painted in.
type CellFacer interface {
	Component
	CellFace() string
}

// FaceHost is a backend that draws cells in fonts: SetCellFaces hands it the face table, index i
// being Cell.Face i (index 0, "", is the window's own). It is called on the loop, before any frame
// that uses a new index; the slice is the backend's to keep.
type FaceHost interface {
	SetCellFaces(families []string)
}

// maxCellFaces bounds the face table, the window's own included.
const maxCellFaces = 64

// faceIndex is family's index in the face table, interned on first use. Past maxCellFaces it is 0.
func (a *App) faceIndex(family string) uint16 {
	if i, ok := a.faceIDs[family]; ok {
		return i
	}
	if len(a.faceNames) == 0 {
		a.faceNames = []string{""}
	}
	if len(a.faceNames) >= maxCellFaces {
		if !a.faceFull {
			a.faceFull = true
			logger.Warning(a.cfg.logger, nil, map[string]any{"tui": "too many cell faces: drawn in the window's", "family": family, "max": maxCellFaces})
		}
		return 0
	}
	i := uint16(len(a.faceNames))
	a.faceNames = append(a.faceNames, family)
	if a.faceIDs == nil {
		a.faceIDs = map[string]uint16{}
	}
	a.faceIDs[family] = i
	a.sendFaces()
	return i
}

// sendFaces hands the face table to a backend that draws fonts: when it grows, and when the
// backend starts.
func (a *App) sendFaces() {
	if h, ok := a.backend.(FaceHost); ok && len(a.faceNames) > 1 {
		h.SetCellFaces(slices.Clone(a.faceNames))
	}
}
