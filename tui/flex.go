package tui

import (
	"fmt"
	"iter"
	"slices"
	"sort"
)

// Direction selects a Flex's main axis.
type Direction uint8

const (
	Horizontal Direction = iota
	Vertical
)

// flexItem is one Flex child: fixed (weight 0 — laid out first with loose
// main-axis constraints, its measured extent consumed) or weighted
// (weight ≥ 1 — receives a largest-remainder share of the remainder).
type flexItem struct {
	comp   Component
	weight int
}

// Flex is the linear container (ADR-0004 §2.7.2): fixed children are
// measured first, then the remainder of the main axis is distributed to
// weighted children by integer largest-remainder — deterministic and
// gap-free by construction (Σ assigned == R always, every run, every
// platform; ties broken by lowest child index). Cross-axis: children get
// the flex's cross extent as a tight constraint (stretch).
type Flex struct {
	dir   Direction
	items []flexItem
	ctx   *Context // non-nil exactly while mounted
}

var _ Container = (*Flex)(nil)

// NewFlex builds an empty Flex with the given main axis.
func NewFlex(dir Direction) *Flex { return &Flex{dir: dir} }

// Add appends fixed children (Container contract): mounted immediately if
// the Flex is mounted, otherwise deferred to Init.
func (f *Flex) Add(children ...Component) {
	for _, c := range children {
		f.add(flexItem{comp: c})
	}
}

// AddWeighted appends a weighted child (weight ≥ 1; ADR-0004 §2.7.2).
func (f *Flex) AddWeighted(child Component, weight int) {
	if weight < 1 {
		panic(fmt.Sprintf("tui: Flex.AddWeighted: weight must be >= 1 (got %d)", weight))
	}
	f.add(flexItem{comp: child, weight: weight})
}

func (f *Flex) add(it flexItem) {
	if it.comp == nil {
		panic("tui: Flex: nil child")
	}
	f.items = append(f.items, it)
	if f.ctx != nil {
		f.ctx.Mount(it.comp)
	}
}

// Move relocates child to index to (Container contract), preserving its
// mount — no unmount/Init cycle. An unmounted Flex reorders items only;
// the framework mirror happens at Init. Weights ride along with their
// child, so moves compose with AddWeighted.
func (f *Flex) Move(child Component, to int) {
	for i, it := range f.items {
		if it.comp == child {
			if to < 0 || to >= len(f.items) {
				panic(fmt.Sprintf("tui: Flex.Move: index %d out of range [0,%d)", to, len(f.items)))
			}
			if i == to {
				return
			}
			f.items = slices.Insert(slices.Delete(f.items, i, i+1), to, it)
			if f.ctx != nil {
				f.ctx.Move(child, to)
			}
			return
		}
	}
}

// Remove unmounts child (cascade) and forgets it.
func (f *Flex) Remove(child Component) {
	for i, it := range f.items {
		if it.comp == child {
			f.items = append(f.items[:i], f.items[i+1:]...)
			if f.ctx != nil {
				f.ctx.Unmount(child)
			}
			return
		}
	}
}

// Children enumerates in document order == focus order == paint order.
func (f *Flex) Children() iter.Seq[Component] {
	return func(yield func(Component) bool) {
		for _, it := range f.items {
			if !yield(it.comp) {
				return
			}
		}
	}
}

// Init mounts the deferred children. Re-entrant across remounts.
func (f *Flex) Init(ctx *Context) {
	f.ctx = ctx
	ctx.OnUnmount(func() { f.ctx = nil })
	for _, it := range f.items {
		ctx.Mount(it.comp)
	}
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

	sizes := make([]Size, len(f.items))

	// Pass 1 — fixed children: loose main-axis constraints, tight cross
	// (stretch), each measured extent consumed from the remainder.
	used := 0
	wsum := 0
	for i, it := range f.items {
		if it.weight > 0 {
			wsum += it.weight
			continue
		}
		avail := Unbounded
		if mainMax != Unbounded {
			avail = max(mainMax-used, 0)
		}
		sizes[i] = f.ctx.LayoutChild(it.comp, f.childConstraints(avail, false, crossMax))
		used += f.main(sizes[i])
	}

	// Pass 2 — weighted children split the remainder R by integer
	// largest-remainder: floor shares, then one extra cell each to the r
	// children with the largest fractional remainders (R·wᵢ mod Wsum),
	// ties broken by LOWEST child index (deterministic, gap-free).
	if wsum > 0 && mainMax != Unbounded {
		r := max(mainMax-used, 0)
		shares := make(map[int]int, len(f.items)) // item index → main-axis cells
		type rem struct{ idx, rem int }
		var rems []rem
		assigned := 0
		for i, it := range f.items {
			if it.weight == 0 {
				continue
			}
			shares[i] = r * it.weight / wsum
			assigned += shares[i]
			rems = append(rems, rem{idx: i, rem: (r * it.weight) % wsum})
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
		for i, it := range f.items {
			if it.weight == 0 {
				continue
			}
			sizes[i] = f.ctx.LayoutChild(it.comp, f.childConstraints(shares[i], true, crossMax))
		}
	} else if wsum > 0 {
		// Unbounded main axis: there is no remainder to split — weighted
		// children size to content like fixed ones.
		for i, it := range f.items {
			if it.weight == 0 {
				continue
			}
			sizes[i] = f.ctx.LayoutChild(it.comp, f.childConstraints(Unbounded, false, crossMax))
			used += f.main(sizes[i])
		}
	}

	// Pass 3 — placement in declaration order along the main axis.
	off := 0
	cross := 0
	for i, it := range f.items {
		sz := sizes[i]
		if horiz {
			f.ctx.PlaceChild(it.comp, Rect{X: off, Y: 0, W: sz.W, H: sz.H})
		} else {
			f.ctx.PlaceChild(it.comp, Rect{X: 0, Y: off, W: sz.W, H: sz.H})
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
