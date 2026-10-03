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
