package tui

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestOnStoppingRunsOnceOnLoopAfterCaptureLossBeforeUnmount(t *testing.T) {
	for _, ending := range []string{"cancel", "quit", "backend EOF"} {
		t.Run(ending, func(t *testing.T) {
			log := &callLog{}
			root := &probe{pref: Size{W: 4, H: 2}}
			var loopID int64
			root.onInit = func(_ *probe, ctx *Context) { loopID = goid(); ctx.OnUnmount(func() { log.add("unmount") }) }
			taken := make(chan struct{})
			root.onEvent = func(p *probe, ev Event) bool {
				switch e := ev.(type) {
				case MouseEvent:
					if e.Kind == MousePress {
						if !p.ctx.CapturePointer() {
							t.Error("capture refused")
						}
						close(taken)
						return true
					}
				case PointerCaptureLostEvent:
					log.add("loss:" + e.Reason.String())
				}
				return false
			}
			tb := NewTestBackend(4, 2)
			app := NewApp(root, WithBackend(tb), WithMinFrameInterval(0), WithOnStopping(nil),
				WithOnStopping(func() {
					if goid() != loopID {
						t.Error("stopping callback left the loop goroutine")
					}
					if !root.ctx.Mounted() || root.ctx.HasPointerCapture() {
						t.Error("callback ran outside live-owner/post-capture-loss interval")
					}
					root.ctx.CancelGesture() // requires a handler or named Update phase, even with no capture
					log.add("stop:first")
				}), WithOnStopping(func() { log.add("stop:second") }))
			h := runApp(t, app, tb)
			h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft})
			select {
			case <-taken:
			case <-time.After(3 * time.Second):
				t.Fatal("press did not capture pointer")
			}
			switch ending {
			case "cancel":
				h.cancel()
			case "quit":
				h.onLoop(app.requestQuit)
			case "backend EOF":
				if err := tb.Stop(); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case result := <-h.resc:
				h.stopOnce.Do(func() { h.res = result })
				if result.err != nil || result.rec != nil {
					t.Fatalf("Run: err=%v panic=%v", result.err, result.rec)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Run did not stop")
			}
			want := []string{"loss:shutdown", "stop:first", "stop:second", "unmount"}
			if got := log.get(); !reflect.DeepEqual(got, want) {
				t.Fatalf("stop order %v, want %v", got, want)
			}
		})
	}
}

type stoppingAcquisitionBackend struct {
	*TestBackend
	startErr, sizeErr error
}

func (b *stoppingAcquisitionBackend) Start(ctx context.Context) error {
	if b.startErr != nil {
		return b.startErr
	}
	return b.TestBackend.Start(ctx)
}
func (b *stoppingAcquisitionBackend) Size() (Size, error) {
	if b.sizeErr != nil {
		return Size{}, b.sizeErr
	}
	return b.TestBackend.Size()
}

func TestOnStoppingDoesNotClaimPreMountAcquisitionFailures(t *testing.T) {
	for _, ending := range []string{"start", "size"} {
		t.Run(ending, func(t *testing.T) {
			want := errors.New("acquisition failed")
			backend := &stoppingAcquisitionBackend{TestBackend: NewTestBackend(4, 2)}
			if ending == "start" {
				backend.startErr = want
			} else {
				backend.sizeErr = want
			}
			calls := 0
			app := NewApp(&probe{}, WithBackend(backend), WithOnStopping(func() { calls++ }))
			if err := app.Run(context.Background()); !errors.Is(err, want) {
				t.Fatal(err)
			}
			if calls != 0 {
				t.Fatalf("stopping callback claimed a live loop before mount: %d", calls)
			}
		})
	}
}
