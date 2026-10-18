package main

/*
#cgo CFLAGS: -Werror -xobjective-c -fobjc-arc
#cgo LDFLAGS: -framework Metal -framework IOSurface -framework CoreFoundation
#include <stdlib.h>
#include "metal_darwin.h"
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"image"
	"unsafe"

	"github.com/yongjohnlee80/gio/op/extimage"
	"github.com/yongjohnlee80/golib/gui"
)

const gpuProducerName = "metal"

// newGPUProducer is the Metal producer for a window with caps. With zeroCopy it needs the
// window's Metal device and its IOSurface tuple; without, any IOSurface tuple will do, the CPU
// driver's included.
func newGPUProducer(caps extimage.Caps, zeroCopy bool) (producer, error) {
	return newMetalProducer(caps, zeroCopy)
}

// metalProducer renders on its own MTLDevice, the window's GPU when the window is Metal, and
// hands the window each picture as an IOSurface.
type metalProducer struct {
	m      *C.m3d
	device string
	how    string

	pool targetPool[*mtlTarget]
}

type mtlTarget struct {
	t       *C.m3d_target
	surface C.uintptr_t
	size    image.Point
}

func newMetalProducer(caps extimage.Caps, zeroCopy bool) (*metalProducer, error) {
	desc := extimage.Desc{Kind: extimage.KindIOSurface, Format: extimage.FormatBGRA8, Size: image.Pt(1, 1)}
	t, ok := caps.Match(desc)
	if !ok || (zeroCopy && t.Path != extimage.PathZeroCopy) {
		return nil, fmt.Errorf("scene3d: the window (%s) draws no IOSurface without a copy", caps.Driver)
	}
	how := "copy"
	if t.Path == extimage.PathZeroCopy {
		how = "zero-copy"
	}
	var errBuf [256]C.char
	match := C.int(0)
	if caps.Driver == "metal" {
		match = 1
	}
	var m *C.m3d
	if C.m3d_new(C.uint64_t(binary.LittleEndian.Uint64(caps.DeviceID[:8])), match, &m, &errBuf[0], C.int(len(errBuf))) != 0 {
		C.m3d_free(m)
		return nil, fmt.Errorf("scene3d: metal: %s", C.GoString(&errBuf[0]))
	}
	ms := newMesh()
	atlas := newAtlas()
	if C.m3d_scene(m, unsafe.Pointer(&ms.verts[0]), C.uint32_t(len(ms.verts)),
		C.uint32_t(ms.floor[0]), C.uint32_t(ms.floor[1]), C.uint32_t(ms.crate[0]), C.uint32_t(ms.crate[1]),
		(*C.uint8_t)(unsafe.Pointer(&atlas.Pix[0])), C.int(atlasW), C.int(atlasH), &errBuf[0], C.int(len(errBuf))) != 0 {
		C.m3d_free(m)
		return nil, fmt.Errorf("scene3d: metal scene: %s", C.GoString(&errBuf[0]))
	}
	p := &metalProducer{m: m, device: C.GoString(C.m3d_device_name(m)), how: how}
	p.pool = targetPool[*mtlTarget]{
		sizeOf:     func(tg *mtlTarget) image.Point { return tg.size },
		freeTarget: func(tg *mtlTarget) { C.m3d_target_free(tg.t) },
		freeDevice: func() { C.m3d_free(m) },
	}
	return p, nil
}

func (p *metalProducer) name() string { return "metal" }

func (p *metalProducer) path() string { return p.how }

func (p *metalProducer) render(t float64, size image.Point) (*gui.Frame, error) {
	tg, err := p.target(size)
	if err != nil {
		return nil, err
	}
	draws := drawsAt(t, size)
	var errBuf [256]C.char
	if C.m3d_render(p.m, tg.t, (*C.float)(unsafe.Pointer(&draws[0])), &errBuf[0], C.int(len(errBuf))) != 0 {
		p.put(tg)
		return nil, fmt.Errorf("scene3d: metal render: %s", C.GoString(&errBuf[0]))
	}
	return extimage.NewFrame(extimage.Desc{
		Kind:      extimage.KindIOSurface,
		Format:    extimage.FormatBGRA8,
		Size:      size,
		IOSurface: uintptr(tg.surface),
	}, func() { p.put(tg) })
}

// target is a free target of size, or a new one.
func (p *metalProducer) target(size image.Point) (*mtlTarget, error) {
	if tg, ok := p.pool.take(size); ok {
		return tg, nil
	}
	tg := &mtlTarget{size: size}
	var errBuf [256]C.char
	if C.m3d_target_new(p.m, C.int(size.X), C.int(size.Y), &tg.t, &tg.surface, &errBuf[0], C.int(len(errBuf))) != 0 {
		C.m3d_target_free(tg.t)
		return nil, fmt.Errorf("scene3d: metal target: %s", C.GoString(&errBuf[0]))
	}
	p.pool.add()
	return tg, nil
}

// put is a frame's release.
func (p *metalProducer) put(tg *mtlTarget) { p.pool.put(tg) }

// close frees the targets no frame holds; the device goes with the last target (targetPool).
func (p *metalProducer) close() { p.pool.close() }
