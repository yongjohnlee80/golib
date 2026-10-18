package main

import (
	"fmt"
	"image"

	"github.com/yongjohnlee80/gio/op/extimage"
	"github.com/yongjohnlee80/golib/gui"
)

// producer renders the scene into frames of one kind, on its own device. One goroutine calls
// render; a frame's release may come from any goroutine, and never blocks.
type producer interface {
	name() string // "vulkan", "metal" or "cpu"
	path() string // how the window draws its frames: "zero-copy" or "copy"
	// render draws the scene at time t (seconds) into a complete frame of size pixels, holding
	// the caller's reference.
	render(t float64, size image.Point) (*gui.Frame, error)
	// close gives the device up once every frame it made is released: frames the window still
	// holds keep their memory until then. Call it once, after the last render.
	close()
}

// choose is the producer for a window with caps: want names one ("vulkan", "metal", "cpu"), or
// "auto" takes the platform's GPU producer when the window draws its frames without a copy, and
// the CPU's otherwise. A named producer draws through whichever path the window has, and one
// that fails to start is an error.
func choose(want string, caps extimage.Caps) (producer, error) {
	switch want {
	case "cpu":
		return newCPUProducer(), nil
	case "auto":
		if p, err := newGPUProducer(caps, true); err == nil {
			return p, nil
		}
		return newCPUProducer(), nil
	case gpuProducerName:
		return newGPUProducer(caps, false)
	}
	return nil, fmt.Errorf("scene3d: no producer %q here (auto, cpu, %s)", want, gpuProducerName)
}

// pathOf is how caps draws a frame like d: "zero-copy", "copy", or "refused".
func pathOf(caps extimage.Caps, d extimage.Desc) string {
	t, ok := caps.Match(d)
	switch {
	case !ok:
		return "refused"
	case t.Path == extimage.PathZeroCopy:
		return "zero-copy"
	}
	return "copy"
}
