package mermaid

import (
	"context"
	"slices"
	"strconv"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { register(pm.GitGraph, layAs(LayGitGraph), nil) }

// gitGlyph is a commit's glyph: a dot, a square for HIGHLIGHT, a crossed ring for REVERSE, a ring
// round a dot for a merge, a ring for a cherry-pick.
func gitGlyph(c pm.GitCommit) string {
	switch {
	case c.Merge:
		return "◉"
	case c.Picked >= 0:
		return "○"
	case c.Type == pm.GitHighlight:
		return "■"
	case c.Type == pm.GitReverse:
		return "⊗"
	}
	return "●"
}

// GitLanes is each branch's lane: branches with an order first, by it, then the rest in the
// order they were made, main always first, as Mermaid places them. Both renderers lane a gitGraph by it.
func GitLanes(d *pm.GitGraphDiagram) []int {
	idx := make([]int, len(d.Branches))
	for i := range idx {
		idx[i] = i
	}
	key := func(i int) (int, int) {
		switch {
		case i == 0:
			return 0, 0
		case d.Branches[i].Order >= 0:
			return 1, d.Branches[i].Order
		}
		return 2, i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		ga, oa := key(a)
		gb, ob := key(b)
		if ga != gb {
			return ga - gb
		}
		return oa - ob
	})
	lane := make([]int, len(d.Branches))
	for l, b := range idx {
		lane[b] = l
	}
	return lane
}

// LayGitGraph lays a gitGraph out in cells: a lane per branch, its name at the lane's start, and a
// commit per step along the time axis (across for LR, down for TB, up for BT), its glyph in its
// lane's series colour (Theme.Series). The lanes and the parents' lines are box-drawing lines: a
// branch's first commit turns off its parent's step, and a merge runs along the merged lane to the
// merge's step and turns into it, so the junctions show where branches leave and join. LR puts a
// commit's tag on the row above its lane and the id given in the source on the row below, a step
// wide enough that no label reaches the next commit's column, where another branch's line may
// pass; TB and BT write them on the row after the commit, beside its lane, the lanes wide enough
// that a label ends before the next lane's line.
func LayGitGraph(ctx context.Context, d *pm.GitGraphDiagram, m Measurer) (*Grid, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g := newGrid()
	lanes := GitLanes(d)
	vertical := d.Dir == pm.TB || d.Dir == pm.BT
	n := len(d.Commits)

	// the step: the widest tag or id, and a gap, so labels never meet the neighbouring columns
	step := 4
	nameW, sideW := 0, 0 // sideW: the widest label beside a commit (TB, BT)
	for _, b := range d.Branches {
		_, w, _ := capLabel(b.Name, RoleLabel, m)
		nameW = max(nameW, w)
	}
	for _, c := range d.Commits {
		if vertical {
			if s := gitSideLabel(c); s != "" {
				_, w, _ := capLabel(s, RoleLabel, m)
				sideW = max(sideW, w)
			}
			continue
		}
		for _, s := range []string{c.Tag, c.ID} {
			if s != "" {
				_, w, _ := capLabel(s, RoleLabel, m)
				step = max(step, w+2)
			}
		}
	}
	// a vertical lane is wide enough for its name and for the labels beside its commits, which
	// start two cells right of the lane and must end before the next lane's line
	laneW := max(nameW, sideW+2) + 3
	// at is a commit's cell: step s on lane l
	at := func(s, l int) cell {
		if vertical {
			t := 2 + 2*s // under the names (TB)
			if d.Dir == pm.BT {
				t = 2 * (n - 1 - s)
			}
			return cell{x: 2 + l*laneW, y: t}
		}
		return cell{x: nameW + 2 + s*step + step/2, y: 1 + l*3}
	}

	// each lane: from its first commit's parent (or the commit) to the graph's last step
	for b := range d.Branches {
		first := slices.IndexFunc(d.Commits, func(c pm.GitCommit) bool { return c.Branch == b })
		if first < 0 {
			continue
		}
		from := first
		if ps := d.Commits[first].Parents; len(ps) > 0 && d.Commits[ps[0]].Branch == b {
			from = ps[0]
		}
		g.run(at(from, lanes[b]), at(max(n-1, first), lanes[b]))
	}
	// each parent's line between lanes: a branch's first commit turns off its parent's step, and a
	// merge comes along the merged lane to its own step
	for i, c := range d.Commits {
		for k, par := range c.Parents {
			pc := d.Commits[par]
			if pc.Branch == c.Branch {
				continue // along the lane, drawn with it
			}
			if k == 0 {
				corner := at(par, lanes[c.Branch])
				g.run(at(par, lanes[pc.Branch]), corner)
				g.run(corner, at(i, lanes[c.Branch]))
			} else {
				corner := at(i, lanes[pc.Branch])
				g.run(at(par, lanes[pc.Branch]), corner)
				g.run(corner, at(i, lanes[c.Branch]))
			}
		}
	}
	// the commits over the lines, each in its lane's colour; their tags and ids
	for i, c := range d.Commits {
		p := at(i, lanes[c.Branch])
		g.fills[p], g.series[p] = gitGlyph(c), lanes[c.Branch]+1
		if vertical {
			// on the row after the commit: its own row may carry a branch leaving it sideways
			g.label(p.x+2, p.y+1, gitSideLabel(c), m)
		} else {
			for k, s := range []string{c.Tag, c.ID} {
				if s == "" {
					continue
				}
				_, w, _ := capLabel(s, RoleLabel, m)
				g.label(p.x-w/2, p.y-1+2*k, s, m)
			}
		}
		if c.Picked >= 0 {
			g.legend(legendRow{swatch: "○", label: "cherry-picked from " + gitCommitName(d, c.Picked), series: lanes[c.Branch] + 1})
		}
	}
	// the names, at each lane's start
	for b, br := range d.Branches {
		if vertical {
			y := 0
			if d.Dir == pm.BT {
				y = 2*max(n-1, 0) + 2
			}
			g.label(at(0, lanes[b]).x, y, br.Name, m)
		} else {
			g.label(0, at(0, lanes[b]).y, br.Name, m)
		}
	}
	return g.finish(m)
}

// gitSideLabel is what TB and BT write beside a commit: its tag and its id, one line.
func gitSideLabel(c pm.GitCommit) string {
	switch {
	case c.Tag != "" && c.ID != "":
		return c.Tag + " " + c.ID
	case c.Tag != "":
		return c.Tag
	}
	return c.ID
}

// gitCommitName is how the legend names commit i: its id, its tag, or its place on its branch.
func gitCommitName(d *pm.GitGraphDiagram, i int) string {
	c := d.Commits[i]
	switch {
	case c.ID != "":
		return c.ID
	case c.Tag != "":
		return c.Tag
	}
	nth := 0
	for _, o := range d.Commits[:i+1] {
		if o.Branch == c.Branch {
			nth++
		}
	}
	return d.Branches[c.Branch].Name + " commit " + strconv.Itoa(nth)
}
