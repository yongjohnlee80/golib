package gui

import (
	"image"
	"math"
	"slices"

	"github.com/yongjohnlee80/gio/unit"
	"github.com/yongjohnlee80/golib/tui"
)

// Track is one child's size along its parent's axis: an exact dp length (Fixed) or a share of
// the length the fixed tracks leave (Flex).
type Track struct {
	dp   unit.Dp
	flex float32 // > 0 for Flex
}

// Fixed is an exact length in dp, rounded to whole device pixels.
func Fixed(d unit.Dp) Track { return Track{dp: max(d, 0)} }

// Flex is a share of the length left after the fixed tracks, by weight among the flex siblings.
// A weight of 0 or less is taken as 1.
func Flex(weight float32) Track {
	if !(weight > 0) {
		weight = 1
	}
	return Track{flex: weight}
}

// Node is a Panes tree node: a Row, a Column, or a pane (Cells, Pixels).
type Node interface{ stageNode() }

// Item is one child of a Row or Column and its Track.
type Item struct {
	track Track
	node  Node
}

// Sized pairs a child with its track.
func Sized(t Track, n Node) Item { return Item{track: t, node: n} }

// Row lays its children out left to right. It keeps its own copy of items: the tree is fixed once
// built, whatever the caller does with its slice afterwards.
func Row(items ...Item) Node { return &split{items: slices.Clone(items)} }

// Column lays its children out top to bottom. Like Row, it keeps its own copy of items.
func Column(items ...Item) Node { return &split{vertical: true, items: slices.Clone(items)} }

// Cells is a cell pane: c lays out in whole cells and is drawn as tui draws. Its band is the
// whole cells that fit its pane; the pixels left under a cell are the page background.
func Cells(c tui.Component) Node { return &leaf{c: c} }

// Pixels is a pixel pane: c draws itself natively (it reports a View through
// tui.NativeReporter), on a canvas that is exactly its pane, in dp. Its band is the cells that
// cover its pane, rounded up.
func Pixels(c tui.Component) Node { return &leaf{c: c, pixels: true} }

type split struct {
	vertical bool
	items    []Item
}

type leaf struct {
	c      tui.Component
	pixels bool
}

func (*split) stageNode() {}
func (*leaf) stageNode()  {}

// leaves is n's panes in tree order: the order of a layout's panes.
func leaves(n Node) []*leaf {
	switch n := n.(type) {
	case *leaf:
		return []*leaf{n}
	case *split:
		var out []*leaf
		for _, it := range n.items {
			if it.node != nil {
				out = append(out, leaves(it.node)...)
			}
		}
		return out
	}
	return nil
}

// fit resolves n inside px (device pixels) and appends each pane's rectangle, in tree order.
// Fixed tracks take their length in order and, past the parent's length, get what is left (0
// when nothing is). Flex tracks share the rest by weight; the last takes the rounding
// remainder, so the children tile their parent along its axis exactly. A split with no flex
// child and room to spare appends that spare rectangle to gaps: no pane covers it.
func fit(n Node, px image.Rectangle, scale float32, out []image.Rectangle, gaps *[]image.Rectangle) []image.Rectangle {
	switch n := n.(type) {
	case *leaf:
		return append(out, px)
	case *split:
		length := px.Dx()
		if n.vertical {
			length = px.Dy()
		}
		sizes := make([]int, len(n.items))
		rest := length
		var weights float32
		for i, it := range n.items {
			if it.track.flex > 0 {
				weights += it.track.flex
				continue
			}
			v := min(max(int(math.Round(float64(float32(it.track.dp)*scale))), 0), rest)
			sizes[i] = v
			rest -= v
		}
		last, used := -1, 0
		for i, it := range n.items {
			if it.track.flex > 0 {
				sizes[i] = int(float32(rest) * it.track.flex / weights)
				used += sizes[i]
				last = i
			}
		}
		if last >= 0 {
			sizes[last] += rest - used
		}
		at := px.Min
		for i, it := range n.items {
			r := image.Rectangle{Min: at, Max: image.Pt(px.Max.X, px.Max.Y)}
			if n.vertical {
				r.Max.Y = at.Y + sizes[i]
				at.Y += sizes[i]
			} else {
				r.Max.X = at.X + sizes[i]
				at.X += sizes[i]
			}
			if it.node != nil {
				out = fit(it.node, r, scale, out, gaps)
			} else {
				*gaps = append(*gaps, r)
			}
		}
		if spare := (image.Rectangle{Min: at, Max: px.Max}); !spare.Empty() {
			if n.vertical {
				spare.Min.X = px.Min.X
			} else {
				spare.Min.Y = px.Min.Y
			}
			*gaps = append(*gaps, spare)
		}
		return out
	}
	return out
}

// bands gives every pane its band of the virtual grid, from its rectangle (rects, in tree order)
// and the cell size: floor for Cells, ceil for Pixels; an empty pane gets an empty band. Bands
// pack as the tree does: a Row's children side by side, a Column's stacked, each container's
// band the bounding box of its children's. Cells of a box no pane's band holds are holes.
func bands(n Node, rects []image.Rectangle, cell image.Point) (tui.Size, []paneMap) {
	var out []paneMap
	i := 0
	size := pack(n, tui.Point{}, rects, &i, cell, &out)
	return size, out
}

func pack(n Node, at tui.Point, rects []image.Rectangle, i *int, cell image.Point, out *[]paneMap) tui.Size {
	switch n := n.(type) {
	case *leaf:
		r := rects[*i]
		*i++
		var s tui.Size
		if !r.Empty() {
			if n.pixels {
				s = tui.Size{W: ceilDiv(r.Dx(), cell.X), H: ceilDiv(r.Dy(), cell.Y)}
			} else {
				s = tui.Size{W: r.Dx() / cell.X, H: r.Dy() / cell.Y}
			}
		}
		*out = append(*out, paneMap{
			band:   tui.Rect{X: at.X, Y: at.Y, W: s.W, H: s.H},
			origin: r.Min,
			clip:   r,
			pixels: n.pixels,
		})
		return s
	case *split:
		var s tui.Size
		for _, it := range n.items {
			if it.node == nil {
				continue
			}
			if n.vertical {
				c := pack(it.node, tui.Point{X: at.X, Y: at.Y + s.H}, rects, i, cell, out)
				s.W, s.H = max(s.W, c.W), s.H+c.H
			} else {
				c := pack(it.node, tui.Point{X: at.X + s.W, Y: at.Y}, rects, i, cell, out)
				s.W, s.H = s.W+c.W, max(s.H, c.H)
			}
		}
		return s
	}
	return tui.Size{}
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }

// paneMap places one pane: its band of virtual cells at a device-pixel origin, clipped to the
// pane. A pane fit to nothing has an empty band and an empty clip.
type paneMap struct {
	band   tui.Rect
	origin image.Point     // device pixels of the band's top-left cell: the pane's top-left
	clip   image.Rectangle // device pixels: the pane
	pixels bool
}

// paneLayout is one immutable layout snapshot. gen tells two snapshots of the same grid apart:
// a layout is paired with its paint by gen, never by its size.
type paneLayout struct {
	gen   uint64
	grid  tui.Size
	inner image.Rectangle   // device pixels: the window minus its padding
	panes []paneMap         // tree order
	gaps  []image.Rectangle // device pixels inside inner that no pane covers
}

// sameShape reports whether l and o place every pane identically, generation aside.
func (l *paneLayout) sameShape(o *paneLayout) bool {
	if l == nil || o == nil {
		return l == o
	}
	return l.grid == o.grid && l.inner == o.inner && slices.Equal(l.panes, o.panes) && slices.Equal(l.gaps, o.gaps)
}
