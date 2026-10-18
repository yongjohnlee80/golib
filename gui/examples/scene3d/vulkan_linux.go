package main

/*
#cgo LDFLAGS: -lvulkan
#include <stdlib.h>
#include "vulkan_linux.h"
*/
import "C"

import (
	_ "embed"
	"errors"
	"fmt"
	"image"
	"sync"
	"unsafe"

	"github.com/yongjohnlee80/gio/op/extimage"
	"github.com/yongjohnlee80/golib/gui"
)

//go:generate glslc --target-env=vulkan1.1 -O shaders/scene.vert -o shaders/scene.vert.spv
//go:generate glslc --target-env=vulkan1.1 -O shaders/scene.frag -o shaders/scene.frag.spv

var (
	//go:embed shaders/scene.vert.spv
	vertSPV []byte
	//go:embed shaders/scene.frag.spv
	fragSPV []byte
)

const gpuProducerName = "vulkan"

// newGPUProducer is the Vulkan producer for a window with caps. With zeroCopy it needs a tuple
// the window imports without a copy, on the window's own device; without, any dma-buf tuple the
// window draws will do, the CPU driver's LINEAR one included.
func newGPUProducer(caps extimage.Caps, zeroCopy bool) (producer, error) {
	return newVulkanProducer(caps, zeroCopy)
}

// vulkanProducer renders on its own VkDevice, on the window's GPU when the window is Vulkan, and
// hands the window each image as a dma-buf.
type vulkanProducer struct {
	s      *C.s3d
	device string
	format extimage.Format
	mods   []uint64 // the modifiers its targets may take, all of which the window draws
	how    string   // the window's path for them

	mu     sync.Mutex
	free   []*vkTarget // targets no frame holds
	stale  []*vkTarget // released targets of another size, freed by render
	size   image.Point
	live   int  // targets made and not yet freed
	closed bool // close ran: a target released after it is freed, and the last frees the device
}

// vkTarget is one exported image.
type vkTarget struct {
	t    *C.s3d_target
	ex   C.s3d_export
	size image.Point
}

func newVulkanProducer(caps extimage.Caps, zeroCopy bool) (*vulkanProducer, error) {
	sameDevice := caps.Driver == "vulkan"
	if zeroCopy && !sameDevice {
		return nil, fmt.Errorf("scene3d: the window draws with %q, so no frame of a Vulkan device is zero-copy", caps.Driver)
	}
	p, err := openVulkan(caps.DeviceID, sameDevice)
	if err != nil {
		return nil, err
	}
	if err := p.pick(caps, zeroCopy); err != nil {
		C.s3d_free(p.s)
		return nil, err
	}
	return p, nil
}

// openVulkan makes the device, the one whose UUID is uuid when match is set, and uploads the
// scene.
func openVulkan(uuid [16]byte, match bool) (*vulkanProducer, error) {
	var errBuf [256]C.char
	cerr := func() error { return errors.New(C.GoString(&errBuf[0])) }
	m := C.int(0)
	if match {
		m = 1
	}
	var s *C.s3d
	if C.s3d_new((*C.uint8_t)(unsafe.Pointer(&uuid[0])), m, &s, &errBuf[0], C.int(len(errBuf))) != 0 {
		C.s3d_free(s)
		return nil, fmt.Errorf("scene3d: vulkan: %w", cerr())
	}
	ms := newMesh()
	atlas := newAtlas()
	if C.s3d_scene(s, unsafe.Pointer(&ms.verts[0]), C.uint32_t(len(ms.verts)),
		C.uint32_t(ms.floor[0]), C.uint32_t(ms.floor[1]), C.uint32_t(ms.crate[0]), C.uint32_t(ms.crate[1]),
		(*C.uint8_t)(unsafe.Pointer(&atlas.Pix[0])), C.int(atlasW), C.int(atlasH),
		unsafe.Pointer(&vertSPV[0]), C.uint32_t(len(vertSPV)), unsafe.Pointer(&fragSPV[0]), C.uint32_t(len(fragSPV)),
		&errBuf[0], C.int(len(errBuf))) != 0 {
		C.s3d_free(s)
		return nil, fmt.Errorf("scene3d: vulkan scene: %w", cerr())
	}
	return &vulkanProducer{s: s, device: C.GoString(C.s3d_device_name(s))}, nil
}

// readback renders the scene at t into a target the host reads, and returns its pixels: the
// rendering checked where no device exports a dma-buf (Mesa's software Vulkan exports none).
// A producer that reads back makes no exported targets.
func (p *vulkanProducer) readback(t float64, size image.Point) (*image.RGBA, error) {
	var errBuf [256]C.char
	var tg *C.s3d_target
	defer func() { C.s3d_target_free(p.s, tg) }()
	if C.s3d_readback_new(p.s, C.int(size.X), C.int(size.Y), C.S3D_RGBA8, &tg, &errBuf[0], C.int(len(errBuf))) != 0 {
		return nil, fmt.Errorf("scene3d: vulkan read-back target: %s", C.GoString(&errBuf[0]))
	}
	draws := drawsAt(t, size)
	if C.s3d_render(p.s, tg, (*C.float)(unsafe.Pointer(&draws[0])), &errBuf[0], C.int(len(errBuf))) != 0 {
		return nil, fmt.Errorf("scene3d: vulkan render: %s", C.GoString(&errBuf[0]))
	}
	img := image.NewRGBA(image.Rectangle{Max: size})
	if C.s3d_read(p.s, tg, (*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), &errBuf[0], C.int(len(errBuf))) != 0 {
		return nil, fmt.Errorf("scene3d: vulkan read: %s", C.GoString(&errBuf[0]))
	}
	return img, nil
}

// pick chooses the targets' format and the modifiers they may take: those the window draws
// (zero-copy ones only, with zeroCopy) that the device renders to and exports.
func (p *vulkanProducer) pick(caps extimage.Caps, zeroCopy bool) error {
	var device []string // what the device exports, for the error
	for _, f := range []struct {
		fmt extimage.Format
		c   C.int
	}{{extimage.FormatBGRA8, C.S3D_BGRA8}, {extimage.FormatRGBA8, C.S3D_RGBA8}} {
		var buf [64]C.uint64_t
		n := int(C.s3d_modifiers(p.s, f.c, &buf[0], C.int(len(buf))))
		for _, m := range buf[:n] {
			device = append(device, fmt.Sprintf("%v:%#x", f.fmt, uint64(m)))
		}
		var zc, cp []uint64 // the window's zero-copy and copy paths
		for _, m := range buf[:n] {
			t, ok := caps.Match(extimage.Desc{Kind: extimage.KindDMABuf, Format: f.fmt, Modifier: uint64(m), Size: image.Pt(1, 1)})
			switch {
			case !ok:
			case t.Path == extimage.PathZeroCopy:
				zc = append(zc, uint64(m))
			default:
				cp = append(cp, uint64(m))
			}
		}
		// The driver picks any modifier of the list, so the list keeps to one path.
		mods, how := zc, "zero-copy"
		if len(mods) == 0 && !zeroCopy {
			mods, how = cp, "copy"
		}
		if len(mods) > 0 {
			p.format, p.mods, p.how = f.fmt, mods, how
			return nil
		}
	}
	var window []string
	for _, t := range caps.Tuples {
		if t.Kind == extimage.KindDMABuf {
			window = append(window, fmt.Sprintf("%v:%#x", t.Format, t.Modifier))
		}
	}
	return fmt.Errorf("scene3d: vulkan: %s exports no dma-buf the window draws (it exports %v; the window draws %v)", p.device, device, window)
}

func (p *vulkanProducer) name() string { return "vulkan" }

func (p *vulkanProducer) path() string { return p.how }

func (p *vulkanProducer) render(t float64, size image.Point) (*gui.Frame, error) {
	tg, err := p.target(size)
	if err != nil {
		return nil, err
	}
	draws := drawsAt(t, size)
	var errBuf [256]C.char
	if C.s3d_render(p.s, tg.t, (*C.float)(unsafe.Pointer(&draws[0])), &errBuf[0], C.int(len(errBuf))) != 0 {
		p.put(tg)
		return nil, fmt.Errorf("scene3d: vulkan render: %s", C.GoString(&errBuf[0]))
	}
	return extimage.NewFrame(extimage.Desc{
		Kind:     extimage.KindDMABuf,
		Format:   p.format,
		Size:     size,
		Planes:   []extimage.Plane{{FD: int(tg.ex.fd), Offset: uint32(tg.ex.offset), Pitch: uint32(tg.ex.pitch)}},
		Modifier: uint64(tg.ex.modifier),
	}, func() { p.put(tg) })
}

// target is a free target of size, or a new one. Released targets of another size are freed
// here, on the render goroutine, never in a frame's release.
func (p *vulkanProducer) target(size image.Point) (*vkTarget, error) {
	p.mu.Lock()
	if size != p.size {
		p.stale = append(p.stale, p.free...)
		p.free, p.size = nil, size
	}
	stale := p.stale
	p.stale = nil
	var tg *vkTarget
	if n := len(p.free); n > 0 {
		tg = p.free[n-1]
		p.free = p.free[:n-1]
	}
	p.mu.Unlock()
	p.freeTargets(stale)
	if tg != nil {
		return tg, nil
	}
	tg = &vkTarget{size: size}
	var errBuf [256]C.char
	if C.s3d_target_new(p.s, C.int(size.X), C.int(size.Y), p.formatC(), (*C.uint64_t)(unsafe.Pointer(&p.mods[0])), C.int(len(p.mods)),
		&tg.t, &tg.ex, &errBuf[0], C.int(len(errBuf))) != 0 {
		C.s3d_target_free(p.s, tg.t)
		return nil, fmt.Errorf("scene3d: vulkan target: %s", C.GoString(&errBuf[0]))
	}
	p.mu.Lock()
	p.live++
	p.mu.Unlock()
	return tg, nil
}

// freeTargets frees ts, then counts them gone; the count reaching zero after close frees the
// device. Counting after freeing means whoever frees the device does so after every other
// party's frees.
func (p *vulkanProducer) freeTargets(ts []*vkTarget) {
	for _, tg := range ts {
		C.s3d_target_free(p.s, tg.t)
	}
	p.mu.Lock()
	p.live -= len(ts)
	last := p.closed && p.live == 0
	p.mu.Unlock()
	if last {
		C.s3d_free(p.s)
	}
}

func (p *vulkanProducer) formatC() C.int {
	if p.format == extimage.FormatBGRA8 {
		return C.S3D_BGRA8
	}
	return C.S3D_RGBA8
}

// put is a frame's release: the target is free again, or stale if the size moved on, or freed
// once the producer is closed.
func (p *vulkanProducer) put(tg *vkTarget) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.freeTargets([]*vkTarget{tg})
		return
	}
	if tg.size == p.size {
		p.free = append(p.free, tg)
	} else {
		p.stale = append(p.stale, tg)
	}
	p.mu.Unlock()
}

// close frees every target no frame holds; a target the window still holds is freed as its
// frame is released, and the last one frees the device. Call it once, after the last render.
func (p *vulkanProducer) close() {
	p.mu.Lock()
	p.closed = true
	all := append(p.free, p.stale...)
	p.free, p.stale = nil, nil
	p.mu.Unlock()
	p.freeTargets(all)
}
