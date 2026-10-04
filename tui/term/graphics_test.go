package term

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// started is a backend past its probe, its output written so far discarded.
func started(t *testing.T, opts ...Option) *termScript {
	t.Helper()
	s := newScript(t, opts...)
	s.respond("\x1b[?62c")
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestImagesRideInTheFrame(t *testing.T) {
	s := started(t)
	mark := len(s.w.String())
	png := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 2000) // 8000 bytes: three base64 chunks
	s.b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: 1001, PNG: png, Version: 1}, X: 4, Y: 2, Cols: 10, Rows: 3})
	if err := s.b.Flush(nil); err != nil {
		t.Fatal(err)
	}
	out := s.w.String()[mark:]
	data := base64.StdEncoding.EncodeToString(png)
	first := "\x1b[3;5H\x1b_Ga=T,f=100,t=d,C=1,q=2,i=1001,c=10,r=3,m=1;" + data[:4096] + "\x1b\\"
	if !strings.Contains(out, "\x1b_Ga=d,d=I,q=2,i=1001\x1b\\"+first) {
		t.Fatalf("the placement (an old one deleted, then at its cell, its first chunk) is not in %q", head(out))
	}
	if !strings.Contains(out, "\x1b_Gm=1;"+data[4096:8192]+"\x1b\\\x1b_Gm=0;"+data[8192:]+"\x1b\\") {
		t.Fatal("the later chunks are not sent in order, the last with m=0")
	}

	mark = len(s.w.String())
	s.b.DeleteImage(1001)
	if err := s.b.Flush(nil); err != nil {
		t.Fatal(err)
	}
	if out := s.w.String()[mark:]; !strings.Contains(out, "\x1b_Ga=d,d=I,q=2,i=1001\x1b\\") {
		t.Fatalf("the delete is not in %q", out)
	}
}

// Inside tmux every command is passed through, its ESCs doubled.
func TestImagesPassThroughTmux(t *testing.T) {
	env := func(k string) (string, bool) {
		if k == "TMUX" {
			return "/tmp/tmux-1/default,1,0", true
		}
		return "", false
	}
	s := started(t, WithEnv(env))
	mark := len(s.w.String())
	s.b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: 7, PNG: []byte("x"), Version: 1}, X: 0, Y: 0, Cols: 1, Rows: 1})
	if err := s.b.Flush(nil); err != nil {
		t.Fatal(err)
	}
	out := s.w.String()[mark:]
	want := "\x1bPtmux;\x1b\x1b_Ga=T,f=100,t=d,C=1,q=2,i=7,c=1,r=1,m=0;eA==\x1b\x1b\\\x1b\\"
	if !strings.Contains(out, want) {
		t.Fatalf("the passed-through placement %q is not in %q", want, out)
	}
}

// Teardown deletes the images this program placed, by id, and no others.
func TestTeardownDeletesOnlyItsImages(t *testing.T) {
	s := started(t)
	for _, id := range []uint32{11, 12} {
		s.b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: id, PNG: []byte("x"), Version: 1}, Cols: 1, Rows: 1})
	}
	s.b.DeleteImage(12)
	if err := s.b.Flush(nil); err != nil {
		t.Fatal(err)
	}
	mark := len(s.w.String())
	if err := s.b.Stop(); err != nil {
		t.Fatal(err)
	}
	out := s.w.String()[mark:]
	if !strings.Contains(out, "\x1b_Ga=d,d=I,q=2,i=11\x1b\\") {
		t.Fatalf("teardown left image 11: %q", out)
	}
	if strings.Contains(out, "i=12") || strings.Contains(out, "d=A") || strings.Contains(out, "d=a") {
		t.Fatalf("teardown deleted what it should not: %q", out)
	}
}

func head(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// An image placed again at the same version — scrolled to another clip — sends no bytes: the old
// placement goes (d=i, the data kept) and the held image is placed with its clip. A new version is
// sent whole, its clip with it; after a delete the next placement sends the bytes again.
func TestAClipChangeSendsNoBytes(t *testing.T) {
	s := started(t)
	png := []byte("png-bytes")
	data := base64.StdEncoding.EncodeToString(png)
	place := func(version uint64, clip tui.Rect) string {
		t.Helper()
		mark := len(s.w.String())
		s.b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: 21, PNG: png, Version: version, Clip: clip}, X: 0, Y: 0, Cols: 8, Rows: 4})
		if err := s.b.Flush(nil); err != nil {
			t.Fatal(err)
		}
		return s.w.String()[mark:]
	}
	if out := place(1, tui.Rect{}); !strings.Contains(out, "a=T,f=100,t=d,C=1,q=2,i=21,c=8,r=4,m=0;"+data) {
		t.Fatalf("the first placement is not sent whole: %q", out)
	}
	out := place(1, tui.Rect{X: 0, Y: 300, W: 800, H: 400})
	if strings.Contains(out, data) || strings.Contains(out, "d=I") {
		t.Fatalf("a scroll sent the image or freed it: %q", out)
	}
	if !strings.Contains(out, "\x1b_Ga=d,d=i,q=2,i=21\x1b\\") || !strings.Contains(out, "\x1b_Ga=p,C=1,q=2,i=21,c=8,r=4,x=0,y=300,w=800,h=400\x1b\\") {
		t.Fatalf("the scroll is not a placement of the held image with its clip: %q", out)
	}
	if out := place(2, tui.Rect{X: 0, Y: 0, W: 800, H: 400}); !strings.Contains(out, "a=T,f=100,t=d,C=1,q=2,i=21,c=8,r=4,x=0,y=0,w=800,h=400,m=0;"+data) {
		t.Fatalf("a new version is not sent whole with its clip: %q", out)
	}
	s.b.DeleteImage(21)
	if err := s.b.Flush(nil); err != nil {
		t.Fatal(err)
	}
	if out := place(2, tui.Rect{}); !strings.Contains(out, "a=T,") {
		t.Fatalf("after a delete the bytes are not sent again: %q", out)
	}
}
