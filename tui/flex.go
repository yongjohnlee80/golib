package tui

import "fmt"

// Direction selects a Flex's main axis.
type Direction uint8

const (
	Horizontal Direction = iota
	Vertical
)

// Flex is the linear layout container:
//
// # Sizing Algorithm & Integer Largest-Remainder Distribution
//
// Flex arranges child components along a single main axis ([Horizontal] or [Vertical]):
//
//	Horizontal Flex Layout:
//	┌─────────────┬───────────────────────────┬──────────────────────┐
//	│ Fixed Child │ Weighted Child (weight=2) │ Weighted Child (w=1) │
//	│ (e.g. 15 col│ (takes 2/3 of remainder)  │ (takes 1/3 remainder)│
//	└─────────────┴───────────────────────────┴──────────────────────┘
//	◄────────────────────── Main Axis Extent ────────────────────────►
//
// Main-axis extent is resolved in two passes, then the cross axis is stretched:
//
//  1. Pass 1 (Fixed Children): Unweighted children (added via Add) are measured
//     first with loose main-axis constraints and tight cross-axis constraints (stretch).
//     Their measured extents are deducted from the available main-axis extent to yield
//     the remainder R.
//  2. Pass 2 (Weighted Distribution): Weighted children (added via AddWeighted)
//     distribute remainder R using the integer largest-remainder method:
//     each child receives its floor share `floor(R * w_i / W_sum)`, and the remaining
//     fractional cells are distributed one by one to the children with the largest
//     remainders `(R * w_i) mod W_sum` (ties broken deterministically by lowest child index).
//     This guarantees zero gaps and exact total sizing: `sum(assigned) == R` across
//     all platforms and resolutions.
//  3. Cross-Axis Stretch: when the flex's own cross axis is FIXED (a tight
//     constraint — the screen, a split's pane, a frame), every child is stretched
//     across it. When it is only bounded, the flex is as wide (or tall) as its
//     widest child, as Qt's layouts are: a row of buttons in a column is one row.
//
// Weights live in an internal side table keyed by the Component value; unweighted
// children have no entry. Remove cleans up the side table entry alongside the child.
type Flex struct {
	MultiChild // order, mount mirror, Move/Children/Init
	geometry
	dir     Direction
	weights childMeta[int]
	sized   childMeta[int] // AddSized: a fixed main-axis extent
}

var _ Container = (*Flex)(nil)

// NewFlex builds an empty Flex with the given main axis.
func NewFlex(dir Direction) *Flex {
	f := &Flex{dir: dir}
	f.Label("Flex")
	return f
}

// Direction is the Flex's main axis.
func (f *Flex) Direction() Direction { return f.dir }

// AddWeighted appends a weighted child (weight >= 1).
func (f *Flex) AddWeighted(child Component, weight int) {
	if weight < 1 {
		panic(fmt.Sprintf("tui: Flex.AddWeighted: weight must be >= 1 (got %d)", weight))
	}
	if child == nil {
		panic("tui: Flex.AddWeighted: nil child")
	}
	f.MultiChild.Add(child)
	f.weights.set(child, weight)
}

// AddSized appends a child of a fixed main-axis extent: n cells wide in a row, n high in a column,
// whatever its content measures (Qt's Layout.preferredWidth), so the next child starts at the same
// place in every row (a label column). With less room left it gets what is left.
func (f *Flex) AddSized(child Component, n int) {
	if n < 0 {
		panic(fmt.Sprintf("tui: Flex.AddSized: size must be >= 0 (got %d)", n))
	}
	if child == nil {
		panic("tui: Flex.AddSized: nil child")
	}
	f.MultiChild.Add(child)
	f.sized.set(child, n)
}

// Remove unmounts child (cascade) and forgets it, weight entry included.
func (f *Flex) Remove(child Component) {
	delete(f.weights, child)
	delete(f.sized, child)
	f.MultiChild.remove(child)
}

// Layout implements the flex algorithm: fixed first, then
// largest-remainder distribution over weights, then placement in
// declaration order.
func (f *Flex) Layout(c Constraints) Size {
	p := f.pass(c)
	sizes := make([]Size, f.Len())
	used, wsum := f.measureFixed(p, sizes)
	f.measureWeighted(p, sizes, used, wsum)
	off, cross := f.place(sizes)

	// Container size: with weighted children the flex fills the bounded
	// main axis; otherwise it wraps its content. Cross: stretch when
	// bounded, else the children's max.
	mainSize := off
	if wsum > 0 && p.mainMax != Unbounded {
		mainSize = p.mainMax
	}
	crossSize := p.crossMax
	if !p.stretch {
		crossSize = cross
	}
	if f.dir == Horizontal {
		return c.Constrain(Size{W: mainSize, H: crossSize})
	}
	return c.Constrain(Size{W: crossSize, H: mainSize})
}

// flexPass is what one Layout is offered along each axis, and whether the cross axis is fixed, so
// children are stretched across it. It lives for the call: Layout keeps nothing in the Flex.
type flexPass struct {
	mainMax, crossMax int
	stretch           bool
}

func (f *Flex) pass(c Constraints) flexPass {
	p := flexPass{mainMax: c.MaxW, crossMax: c.MaxH}
	crossTight := c.MinH == c.MaxH
	if f.dir != Horizontal {
		p.mainMax, p.crossMax = c.MaxH, c.MaxW
		crossTight = c.MinW == c.MaxW
	}
	// STRETCH ONLY WHEN THE CROSS AXIS IS FIXED, as Qt's layouts do: a row
	// offered "up to ten rows high" is as high as its tallest child, not ten.
	// A parent that fixes the extent — the screen, a split's pane, a frame —
	// still stretches its children across it.
	p.stretch = crossTight && p.crossMax != Unbounded
	return p
}

// measureFixed is pass 1: each visible unweighted child is measured with loose main-axis
// constraints (a sized child's fixed extent, or what is left of it) and tight cross, its extent
// consumed from the remainder. It returns the main-axis cells used and the weighted children's
// weight sum.
func (f *Flex) measureFixed(p flexPass, sizes []Size) (used, wsum int) {
	for i, it := range f.All() {
		if hidden(it) {
			continue // a hidden child neither measures nor reserves a share
		}
		if w := f.weights[it]; w > 0 {
			wsum += w
			continue
		}
		avail := Unbounded
		if p.mainMax != Unbounded {
			avail = max(p.mainMax-used, 0)
		}
		if n, ok := f.sized[it]; ok { // a fixed extent, or what is left of it
			sizes[i] = f.Ctx().LayoutChild(it, f.childConstraints(p, min(n, avail), true))
		} else {
			sizes[i] = f.Ctx().LayoutChild(it, f.childConstraints(p, avail, false))
		}
		used += f.main(sizes[i])
	}
	return used, wsum
}

// measureWeighted is pass 2: the visible weighted children split the bounded remainder by
// largestRemainder, each laid out tight to its share. On an unbounded main axis there is no
// remainder to split, and they size to content like fixed ones.
func (f *Flex) measureWeighted(p flexPass, sizes []Size, used, wsum int) {
	if wsum == 0 {
		return
	}
	weights := make([]int, f.Len())
	for i, it := range f.All() {
		if !hidden(it) {
			weights[i] = f.weights[it]
		}
	}
	if p.mainMax == Unbounded {
		for i, it := range f.All() {
			if weights[i] > 0 {
				sizes[i] = f.Ctx().LayoutChild(it, f.childConstraints(p, Unbounded, false))
			}
		}
		return
	}
	shares := largestRemainder(max(p.mainMax-used, 0), weights)
	for i, it := range f.All() {
		if weights[i] > 0 {
			sizes[i] = f.Ctx().LayoutChild(it, f.childConstraints(p, shares[i], true))
		}
	}
}

// place is pass 3: the children in declaration order along the main axis. It returns the main-axis
// extent they cover and the largest cross-axis extent among them.
func (f *Flex) place(sizes []Size) (off, cross int) {
	for i, it := range f.All() {
		sz := sizes[i]
		if f.dir == Horizontal {
			f.Ctx().PlaceChild(it, Rect{X: off, Y: 0, W: sz.W, H: sz.H})
		} else {
			f.Ctx().PlaceChild(it, Rect{X: 0, Y: off, W: sz.W, H: sz.H})
		}
		off += f.main(sz)
		cross = max(cross, f.cross(sz))
	}
	return off, cross
}

// childConstraints builds a child's constraints: tight on the main axis to avail when tightMain,
// else loose up to it; tight on the cross axis when the pass stretches, else loose up to it.
func (f *Flex) childConstraints(p flexPass, avail int, tightMain bool) Constraints {
	mainMin := 0
	if tightMain {
		mainMin = avail
	}
	crossMin := 0
	if p.stretch {
		crossMin = p.crossMax // a fixed cross axis: stretch
	}
	if f.dir == Horizontal {
		return Constraints{MinW: mainMin, MaxW: avail, MinH: crossMin, MaxH: p.crossMax}
	}
	return Constraints{MinW: crossMin, MaxW: p.crossMax, MinH: mainMin, MaxH: avail}
}

func (f *Flex) main(s Size) int {
	if f.dir == Horizontal {
		return s.W
	}
	return s.H
}

func (f *Flex) cross(s Size) int {
	if f.dir == Horizontal {
		return s.H
	}
	return s.W
}
