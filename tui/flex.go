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

// Flex is the linear container (ADR-0004 §2.7.2): fixed children are
// measured first, then the remainder of the main axis is distributed to
// weighted children by integer largest-remainder — deterministic and
// gap-free by construction (Σ assigned == R always, every run, every
// platform; ties broken by lowest child index). Cross-axis: children get
// the flex's cross extent as a tight constraint (stretch).
//
// Weights live in a side table keyed by the child itself; zero-weight
// (fixed) children have no entry. Remove shadows the promoted method to
// drop the weight entry alongside the child.
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

// AddWeighted appends a weighted child (weight ≥ 1; ADR-0004 §2.7.2).
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

// Layout implements the ADR-0004 §2.7.2 algorithm: fixed first, then
// largest-remainder distribution over weights, then placement in
// declaration order.
func (f *Flex) Layout(c Constraints) Size {
	horiz := f.dir == Horizontal
	mainMax, crossMax := c.MaxW, c.MaxH
	if !horiz {
		mainMax, crossMax = c.MaxH, c.MaxW
	}

	sizes := make([]Size, len(f.Items()))

	// Pass 1 — fixed children: loose main-axis constraints, tight cross
	// (stretch), each measured extent consumed from the remainder.
	used := 0
	wsum := 0
	for i, it := range f.Items() {
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
		shares := make(map[int]int, len(f.Items())) // item index → main-axis cells
		type rem struct{ idx, rem int }
		var rems []rem
		assigned := 0
		for i, it := range f.Items() {
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
		for i, it := range f.Items() {
			if f.weights[it] == 0 {
				continue
			}
			sizes[i] = f.Ctx().LayoutChild(it, f.childConstraints(shares[i], true, crossMax))
		}
	} else if wsum > 0 {
		// Unbounded main axis: there is no remainder to split — weighted
		// children size to content like fixed ones.
		for i, it := range f.Items() {
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
	for i, it := range f.Items() {
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

// HandleEvent consumes nothing; events bubble through (ADR-0004 §2.5).
func (f *Flex) HandleEvent(Event) bool { return false }
