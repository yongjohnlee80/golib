package mermaid

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

const gitDocSrc = `gitGraph
    commit id: "A"
    commit tag: "v1"
    branch develop
    checkout develop
    commit
    commit type: HIGHLIGHT
    checkout main
    merge develop
    commit type: REVERSE`

// A gitGraph is a lane per branch and a dot per commit along the time axis: each commit on its
// branch's lane, later commits further along, the merge a double ring, a tag in a box over its
// commit and an id written under it; a line for each parent link and one for each lane.
func TestAGitGraphLaysItsLanesAndCommits(t *testing.T) {
	l, err := lay(t, gitDocSrc, 2000)
	if err != nil {
		t.Fatal(err)
	}
	var dots []laidNode
	for _, n := range l.nodes {
		if n.form != formRound { // the tag's box is round; every other node is a commit
			dots = append(dots, n)
		}
	}
	if len(dots) != 6 {
		t.Fatalf("%d commit dots, want 6", len(dots))
	}
	// LR: time runs across, each commit right of the one before; main's lane above develop's
	for i := 1; i < len(dots); i++ {
		if dots[i].box.X <= dots[i-1].box.X {
			t.Errorf("commit %d at x %v, not right of commit %d at %v", i, dots[i].box.X, i-1, dots[i-1].box.X)
		}
	}
	mainY, devY := dots[0].box.Y+dots[0].box.H/2, dots[2].box.Y+dots[2].box.H/2
	if devY <= mainY {
		t.Errorf("develop's lane at y %v, not under main's at %v", devY, mainY)
	}
	if dots[4].form != formEnd || dots[3].form != formRect || dots[5].form != formCircle {
		t.Errorf("forms merge %v, highlight %v, reverse %v", dots[4].form, dots[3].form, dots[5].form)
	}
	texts := map[string]bool{}
	for _, tx := range l.texts {
		for _, ln := range tx.lines {
			texts[ln] = true
		}
	}
	for _, n := range l.nodes {
		for _, ln := range n.label.lines {
			texts[ln] = true
		}
	}
	for _, want := range []string{"main", "develop", "A", "v1"} {
		if !texts[want] {
			t.Errorf("%q is not written (%v)", want, texts)
		}
	}
	// 6 parent links (the merge has two), 2 lanes, 2 strokes for the reverse's cross
	if got := len(l.edges); got != 6+2+2 {
		t.Errorf("%d lines, want 10", got)
	}
}

// TB runs time down and the lanes across; BT runs it up.
func TestAGitGraphFollowsItsDirection(t *testing.T) {
	for _, c := range []struct {
		dir  string
		down bool
	}{{"TB", true}, {"BT", false}} {
		l, err := lay(t, "gitGraph "+c.dir+":\n  commit\n  commit", 2000)
		if err != nil {
			t.Fatal(err)
		}
		a, b := l.nodes[0].box, l.nodes[1].box
		if a.X != b.X || (b.Y > a.Y) != c.down {
			t.Errorf("%s: commits at %+v then %+v", c.dir, a, b)
		}
	}
	if _, ok := layers[pm.GitGraph]; !ok {
		t.Error("no layer for gitGraph")
	}
}

// TB (and BT) write a commit's tag and id beside it, right of its lane's line, never over the line
// that runs along the lane through the dot.
func TestAGitGraphsLabelsSitBesideAVerticalLane(t *testing.T) {
	l, err := lay(t, "gitGraph TB:\n  commit tag: \"v1.0.0\" id: \"base\"\n  commit", 2000)
	if err != nil {
		t.Fatal(err)
	}
	dot := l.nodes[0].box
	lineX := dot.X + dot.W/2
	var tagBox *laidNode
	for i := range l.nodes {
		if l.nodes[i].form == formRound {
			tagBox = &l.nodes[i]
		}
	}
	if tagBox == nil {
		t.Fatal("no tag box")
	}
	if tagBox.box.X <= lineX {
		t.Errorf("the tag's box from x %v crosses the lane's line at %v", tagBox.box.X, lineX)
	}
	for _, tx := range l.texts {
		if len(tx.lines) > 0 && tx.lines[0] == "base" && tx.box.X <= tagBox.box.X+tagBox.box.W {
			t.Errorf("the id at x %v is not right of the tag's box (ends %v)", tx.box.X, tagBox.box.X+tagBox.box.W)
		}
	}
}

// A REVERSE commit's cross is painted after its ring: the ring's fill would hide a cross drawn
// under it. On a recording canvas, after the ring's fill (its last ellipse) come its outline's
// stroke and then the cross's two: three strokes, where a cross drawn under the ring leaves one.
func TestAReverseCommitsCrossIsPaintedOverItsRing(t *testing.T) {
	l, err := lay(t, "gitGraph\n  commit type: REVERSE", 2000)
	if err != nil {
		t.Fatal(err)
	}
	rc := gui.NewRecordingCanvas(l.Size(), gui.Size{W: 8, H: 16})
	l.Paint(rc)
	lastEllipse, strokesAfter := -1, 0
	for i, c := range rc.Calls {
		if c.Op == "FillEllipse" {
			lastEllipse = i
		}
	}
	if lastEllipse < 0 {
		t.Fatalf("no ellipse painted: %v", rc.Calls)
	}
	for _, c := range rc.Calls[lastEllipse+1:] {
		if c.Op == "StrokePath" {
			strokesAfter++
		}
	}
	if strokesAfter != 3 {
		t.Errorf("%d strokes after the ring's fill, want 3 (its outline, then the cross's two)", strokesAfter)
	}
}
