package gui

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"testing"

	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/golib/errs"
)

// Snapshot draws a view through the window's path at its scale: a red square inset in a blue view
// lands at its dp place times the scale, and the pixels are the size's whole dp times the scale.
func TestSnapshotDrawsAViewAtItsScale(t *testing.T) {
	red, blue := color.NRGBA{R: 0xc0, A: 0xff}, color.NRGBA{B: 0xc0, A: 0xff}
	v := ViewFunc(func(c Canvas) {
		c.FillRect(Rect{W: c.Size().W, H: c.Size().H}, Solid(blue))
		c.FillRect(Rect{X: 10, Y: 6, W: 20, H: 12}, Solid(red))
	})
	for _, s := range []int{1, 2} {
		img, err := Snapshot(v, Size{W: 40, H: 23.5}, s)
		if errors.Is(err, ErrNoGPU) {
			t.Skipf("no headless GPU here: %v", err)
		}
		if err != nil {
			t.Fatal(err) // the drawing or its read-back failed: a GPU here, and something broken
		}
		if img.Bounds() != image.Rect(0, 0, 40*s, 24*s) {
			t.Fatalf("at %dx the snapshot is %v, want %dx%d", s, img.Bounds(), 40*s, 24*s)
		}
		at := func(x, y int) color.RGBA { return img.RGBAAt(x*s, y*s) }
		if c := at(20, 12); c.R < 0xa0 || c.B > 0x20 {
			t.Errorf("at %dx (20,12) is %v, want the red square", s, c)
		}
		if c := at(5, 3); c.B < 0xa0 || c.R > 0x20 {
			t.Errorf("at %dx (5,3) is %v, want the blue view", s, c)
		}
		if c := at(31, 12); c.B < 0xa0 || c.R > 0x20 {
			t.Errorf("at %dx (31,12) is %v, want blue: the square ends at 30", s, c)
		}
	}
	img, err := Snapshot(v, Size{W: 4, H: 4}, 0)
	if err != nil {
		t.Fatalf("scale 0: %v", err) // a GPU answered above
	}
	if img.Bounds() != image.Rect(0, 0, 4, 4) {
		t.Fatalf("scale 0 gave %v, want scale 1's 4x4", img.Bounds())
	}
}

// failingTarget is a GPU that initialised and then fails to draw or to read back.
type failingTarget struct{ frame, shot error }

func (f failingTarget) Frame(*op.Ops) error          { return f.frame }
func (f failingTarget) Screenshot(*image.RGBA) error { return f.shot }

// A drawing or read-back that fails after the GPU initialised is an error a test fails on, never
// ErrNoGPU, which a test skips on.
func TestASnapshotsFailureAfterTheGPUIsNotNoGPU(t *testing.T) {
	boom := errors.New("boom")
	v := ViewFunc(func(Canvas) {})
	for _, c := range []struct {
		name string
		t    failingTarget
	}{{"drawing", failingTarget{frame: boom}}, {"reading back", failingTarget{shot: boom}}} {
		_, err := snapshotOn(c.t, v, Size{W: 4, H: 4}, 1)
		if !errors.Is(err, boom) || errors.Is(err, ErrNoGPU) {
			t.Errorf("%s failed: err %v; want the failure itself, not ErrNoGPU", c.name, err)
		}
	}
	if !errors.Is(ErrNoGPU, errs.ErrUnsupported) {
		t.Error("ErrNoGPU is not an unsupported condition")
	}
}

// A Snapshotter draws many snapshots on its one window, each its own: a view drawn twice is the same
// pixels, and a different view between them doesn't leak into the second.
func TestASnapshotterDrawsManySnapshotsOnOneWindow(t *testing.T) {
	s, err := NewSnapshotter(Size{W: 8, H: 8}, 1)
	if errors.Is(err, ErrNoGPU) {
		t.Skipf("no headless GPU here: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	fill := func(c color.NRGBA) View {
		return ViewFunc(func(cv Canvas) { cv.FillRect(Rect{W: 8, H: 8}, Solid(c)) })
	}
	red, blue := color.NRGBA{R: 0xc0, A: 0xff}, color.NRGBA{B: 0xc0, A: 0xff}
	a, err := s.Snap(fill(red))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Snap(fill(blue))
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Snap(fill(red))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Pix, c.Pix) || a.RGBAAt(4, 4).R < 0xa0 || b.RGBAAt(4, 4).B < 0xa0 || b.RGBAAt(4, 4).R > 0x20 {
		t.Fatalf("red %v, blue %v, red again %v; want each its own colour, red twice the same", a.RGBAAt(4, 4), b.RGBAAt(4, 4), c.RGBAAt(4, 4))
	}
}
