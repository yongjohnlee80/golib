package widget

import "github.com/yongjohnlee80/golib/tui"

// Raisable keeps a place in its parent's layout and draws its child in a non-modal Float over that
// place (OverNode), so the keyboard entering the child raises it above the panels, as it raises a
// panel (floatLayer's raise on a FocusEvent, OverlayHost.Raise), and a panel taking the keyboard
// raises above it again. Its place is what its parent's layout gives it; it paints nothing of its
// own, so where its Float is not shown the place is the parent's background.
//
// It finds the OverlayHost enclosing it, as a Select or a Menu finds where its popup goes, and its
// Float is one of that host's layers while the Raisable is mounted: the lowest (AttachBeneath), as
// an application's main window is, so a panel open when it arrives, or shown later without the
// keyboard (Float.Show raises it), is drawn above it until the keyboard enters it.
//
// SetMaximumWidth caps the child's width and centres it in the place, as a Box's does: the margins
// either side are no part of the Float, so a panel there is drawn and clicked as if the Raisable
// were not.
type Raisable struct {
	Base
	float    *Float
	maxW     int
	attached bool // the Float is one of the host's layers
}

// raisableRegion is the part of the place the child covers (all of it, or the capped width
// centred), which the Float is anchored over.
const raisableRegion tui.RegionID = "raisable"

var _ tui.Component = (*Raisable)(nil)

// NewRaisable wraps child, which is laid out in the Raisable's place.
func NewRaisable(child tui.Component) *Raisable {
	return &Raisable{float: NewFloat(child, WithModal(false))}
}

// Float is the Float the child is drawn in: a test or a host reads where it lies.
func (r *Raisable) Float() *Float { return r.float }

// SetMaximumWidth caps the child at w columns, centred in the place (CSS's max-width with auto
// margins); 0 removes the cap.
func (r *Raisable) SetMaximumWidth(w int) {
	r.maxW = max(w, 0)
	r.RequestLayout()
}

// Init attaches the Float to the enclosing OverlayHost once the loop turns, never while the tree
// is mounting, and Layout shows it; Unmount takes it off again, once. With no host above it, the
// child is never drawn: a Raisable needs one.
func (r *Raisable) Init(ctx *tui.Context) {
	r.Base.Init(ctx)
	host, ok := hostFor[*OverlayHost](ctx)
	if !ok {
		return
	}
	ctx.Update(func() {
		if ctx.Mounted() {
			host.AttachBeneath(r.float)
			r.attached = true
			ctx.RequestLayout()
		}
	})
	ctx.OnUnmount(func() {
		// taken out of a live tree, its layer leaves the host; in the App's teardown the host's
		// layers are unmounted already, and there is nothing to take off
		if r.attached {
			r.attached = false
			if fc := r.float.Context(); fc != nil && fc.Mounted() {
				host.Detach(r.float)
			}
		}
	})
}

// Layout takes the place its parent gives it (all of it, or its minimum where it is unbounded),
// declares the part the child covers, and anchors the Float over that part. The anchor is set on
// the Float directly: the Float lays out after the base in this same pass, so it needs no request.
func (r *Raisable) Layout(c tui.Constraints) tui.Size {
	size := c.Constrain(tui.Size{W: boundedMax(c.MaxW, c.MinW), H: boundedMax(c.MaxH, c.MinH)})
	part := tui.Rect{W: size.W, H: size.H}
	if r.maxW > 0 && size.W > r.maxW {
		part.X, part.W = (size.W-r.maxW)/2, r.maxW
	}
	ctx := r.Context()
	if a := OverNode(ctx.DeclareRegion(raisableRegion, part)); r.float.anchor != a {
		r.float.anchor = a
	}
	if r.attached && !r.float.Shown() { // the first layout, or laid out again after being hidden
		ctx.AfterLayout("raisable-show", func() {
			if r.attached && ctx.Mounted() && !r.float.Shown() {
				r.float.show() // where it stands: beneath the panels until the keyboard raises it
			}
		})
	}
	return size
}

// Render paints nothing: the Float draws the child.
func (r *Raisable) Render(tui.Surface) {}
