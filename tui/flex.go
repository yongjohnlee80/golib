package tui

import (
	"fmt"
	"sort"
)

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
//  3. Cross-Axis Stretch: All children receive the flex's cross-axis dimension as
//     a tight constraint.
//
// Weights live in an internal side table keyed by the Component value; unweighted
// children have no entry. Remove cleans up the side table entry alongside the child.
type Flex struct {
	MultiChild // order, mount mirror, Move/Children/Init
	dir        Direction
	weights    map[Component]int
}

var _ Container = (*Flex)(nil)

// NewFlex builds an empty Flex with the given main axis.
func NewFlex(dir Direction) *Flex {
	f := &Flex{dir: dir}
	f.Label("Flex")
	return f
}

// AddWeighted appends a weighted child (weight >= 1).
func (f *Flex) AddWeighted(child Component, weight int) {
	if weight < 1 {
		panic(fmt.Sprintf("tui: Flex.AddWeighted: weight must be >= 1 (got %d)", weight))
	}
	if child == nil {
		panic("tui: Flex.AddWeighted: nil child")
	}
	f.MultiChild.Add(child)
	if f.weights == nil {
		f.weights = make(map[Component]int)
	}
	f.weights[child] = weight
}

// Remove unmounts child (cascade) and forgets it, weight entry included.
func (f *Flex) Remove(child Component) {
	delete(f.weights, child)
	f.MultiChild.remove(child)
}

// Layout implements the flex algorithm: fixed first, then
// largest-remainder distribution over weights, then placement in
// declaration order.
func (f *Flex) Layout(c Constraints) Size {
	horiz := f.dir == Horizontal
	mainMax, crossMax := c.MaxW, c.MaxH
	if !horiz {
		mainMax, crossMax = c.MaxH, c.MaxW
	}

	sizes := make([]Size, f.Len())

	// Pass 1 — fixed children: loose main-axis constraints, tight cross
	// (stretch), each measured extent consumed from the remainder.
	used := 0
	wsum := 0
	for i, it := range f.All() {
		w := f.weights[it]
		if w > 0 {
			wsum += w
			continue
		}
		avail := Unbounded
		if mainMax != Unbounded {
			avail = max(mainMax-used, 0)
		}
		sizes[i] = f.Ctx().LayoutChild(it, f.childConstraints(avail, false, crossMax))
		used += f.main(sizes[i])
	}

	// Pass 2 — weighted children split the remainder R by integer
	// largest-remainder: floor shares, then one extra cell each to the r
	// children with the largest fractional remainders (R·wᵢ mod Wsum),
	// ties broken by LOWEST child index (deterministic, gap-free).
	if wsum > 0 && mainMax != Unbounded {
		r := max(mainMax-used, 0)
		shares := make(map[int]int, f.Len()) // item index → main-axis cells
		type rem struct{ idx, rem int }
		var rems []rem
		assigned := 0
		for i, it := range f.All() {
			w := f.weights[it]
			if w == 0 {
				continue
			}
			shares[i] = r * w / wsum
			assigned += shares[i]
			rems = append(rems, rem{idx: i, rem: (r * w) % wsum})
		}
		sort.Slice(rems, func(a, b int) bool {
			if rems[a].rem != rems[b].rem {
				return rems[a].rem > rems[b].rem
			}
			return rems[a].idx < rems[b].idx
		})
		for i := 0; i < r-assigned; i++ {
			shares[rems[i].idx]++
		}
		for i, it := range f.All() {
			if f.weights[it] == 0 {
				continue
			}
			sizes[i] = f.Ctx().LayoutChild(it, f.childConstraints(shares[i], true, crossMax))
		}
	} else if wsum > 0 {
		// Unbounded main axis: there is no remainder to split — weighted
		// children size to content like fixed ones.
		for i, it := range f.All() {
			if f.weights[it] == 0 {
				continue
			}
			sizes[i] = f.Ctx().LayoutChild(it, f.childConstraints(Unbounded, false, crossMax))
			used += f.main(sizes[i])
		}
	}

	// Pass 3 — placement in declaration order along the main axis.
	off := 0
	cross := 0
	for i, it := range f.All() {
		sz := sizes[i]
		if horiz {
			f.Ctx().PlaceChild(it, Rect{X: off, Y: 0, W: sz.W, H: sz.H})
		} else {
			f.Ctx().PlaceChild(it, Rect{X: 0, Y: off, W: sz.W, H: sz.H})
		}
		off += f.main(sz)
		cross = max(cross, f.cross(sz))
	}

	// Container size: with weighted children the flex fills the bounded
	// main axis; otherwise it wraps its content. Cross: stretch when
	// bounded, else the children's max.
	mainSize := off
	if wsum > 0 && mainMax != Unbounded {
		mainSize = mainMax
	}
	crossSize := crossMax
	if crossMax == Unbounded {
		crossSize = cross
	}
	if horiz {
		return c.Constrain(Size{W: mainSize, H: crossSize})
	}
	return c.Constrain(Size{W: crossSize, H: mainSize})
}

// childConstraints builds a child's constraints: main axis loose up to
// avail (or tight when tightMain), cross axis tight to the flex's extent
// (stretch) unless unbounded.
func (f *Flex) childConstraints(avail int, tightMain bool, crossMax int) Constraints {
	mainMin := 0
	if tightMain {
		mainMin = avail
	}
	crossMin := 0
	if crossMax != Unbounded {
		crossMin = crossMax // tight cross: stretch
	}
	if f.dir == Horizontal {
		return Constraints{MinW: mainMin, MaxW: avail, MinH: crossMin, MaxH: crossMax}
	}
	return Constraints{MinW: crossMin, MaxW: crossMax, MinH: mainMin, MaxH: avail}
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

// Render paints nothing: a Flex is pure geometry; children paint themselves
// on the sub-Surfaces the framework hands them.
func (f *Flex) Render(Surface) {}

// HandleEvent consumes nothing; events bubble through.
func (f *Flex) HandleEvent(Event) bool { return false }
