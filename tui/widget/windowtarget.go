package widget

import "github.com/yongjohnlee80/golib/tui"

type floatWindowPolicy struct {
	anchor     Anchor
	wPct, hPct int
}

type floatWindowTarget struct {
	float        *Float
	core         *WindowCore
	workArea     func(tui.Rect) tui.Rect
	bounds, area tui.Rect
	maximized    bool
	normal       floatWindowPolicy
}

func (t *floatWindowTarget) Bounds() tui.Rect { return t.bounds }
func (t *floatWindowTarget) RequestedBounds() tui.Rect {
	if t.maximized {
		return t.area
	}
	if r, ok := t.float.anchor.resolve(t.area); ok {
		return r // a live rectangle, when there is one, wins over its fallback
	}
	if a := t.float.anchor; a.atRect {
		return a.rect // the request as made; Bounds is where the area keeps it (Float.place)
	} else if a.atFraction {
		return fractionRect(a.frac, t.area)
	}
	return t.bounds
}
func (t *floatWindowTarget) Area() tui.Rect { return t.area }

func windowBoundsInArea(r, area tui.Rect) tui.Rect { return r.Within(area, 1) }

func (t *floatWindowTarget) SetBounds(r tui.Rect) bool {
	if t.area.Empty() || t.maximized {
		return false
	}
	r = windowBoundsInArea(r, t.area)
	f := t.float
	if f.anchor == AtRect(r) && f.wPct == 0 && f.hPct == 0 {
		return false
	}
	f.anchor, f.wPct, f.hPct = AtRect(r), 0, 0
	f.RequestLayout()
	return true
}

func (t *floatWindowTarget) policy() floatWindowPolicy {
	return floatWindowPolicy{t.float.anchor, t.float.wPct, t.float.hPct}
}

func (t *floatWindowTarget) apply(p floatWindowPolicy) {
	t.float.anchor, t.float.wPct, t.float.hPct = p.anchor, p.wPct, p.hPct
	t.float.RequestLayout()
}

func (t *floatWindowTarget) Checkpoint() func() {
	p, maximized, normal := t.policy(), t.maximized, t.normal
	return func() { t.maximized, t.normal = maximized, normal; t.apply(p) }
}

func (t *floatWindowTarget) ToggleMaximize() bool {
	if t.maximized {
		t.maximized = false
		t.apply(t.normal)
	} else {
		t.normal, t.maximized = t.policy(), true
		t.apply(floatWindowPolicy{Center, 100, 100})
	}
	return true
}

func (t *floatWindowTarget) Maximized() bool     { return t.maximized }
func (t *floatWindowTarget) Modal() bool         { return t.float.modal }
func (t *floatWindowTarget) WindowVisible() bool { return t.float.shown && t.float.Visible() }
func (t *floatWindowTarget) SetWindowVisible(v bool) bool {
	if !t.float.shown || t.float.Visible() == v {
		return false
	}
	t.float.SetVisible(v)
	return true
}
func (t *floatWindowTarget) CloseWindow() bool {
	if !t.float.shown {
		return false
	}
	t.float.SetVisible(true)
	t.float.Hide()
	return true
}

func (t *floatWindowTarget) ownerPlacementChanged() {
	if t.core != nil {
		t.core.cancelDrag()
	}
	t.maximized = false
}

func (l *floatLayer) layoutManaged(c tui.Constraints, w, h int) tui.Size {
	f, t := l.owner, l.owner.windowTarget
	area := tui.Rect{W: w, H: h}
	if t.workArea != nil {
		area = area.Intersect(t.workArea(area))
	}
	r := area
	if t.maximized {
		l.layoutChild(tui.Tight(tui.Size{W: r.W, H: r.H}))
	} else {
		r = f.place(area, l.layoutChild)
	}
	f.content = r
	l.ctx.PlaceChild(f.child, r)
	l.ctx.AfterLayout("window-placement", func() { t.bounds, t.area = r, area })
	return c.Constrain(tui.Size{W: w, H: h})
}
