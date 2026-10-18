package main

import (
	"image"
	"math"
	"runtime"
	"sync"

	"github.com/yongjohnlee80/gio/op/extimage"
	"github.com/yongjohnlee80/golib/gui"
)

// cpuProducer draws the scene in Go, into memory frames: the producer every window draws, on any
// renderer, through the CPU driver's upload. It is the fallback, and the reference picture the
// GPU producers are tested against.
type cpuProducer struct {
	mesh  mesh
	atlas *image.RGBA

	mu    sync.Mutex
	free  [][]byte // pixel buffers no frame holds
	size  image.Point
	depth []float32 // the depth buffer, reused
}

func newCPUProducer() *cpuProducer {
	return &cpuProducer{mesh: newMesh(), atlas: newAtlas()}
}

func (p *cpuProducer) name() string { return "cpu" }

func (p *cpuProducer) path() string { return "copy" }

// render draws the scene at time t into a size frame; one goroutine calls it. The frame's
// release returns its buffer to the pool.
func (p *cpuProducer) render(t float64, size image.Point) (*gui.Frame, error) {
	buf := p.buffer(size)
	if len(p.depth) != size.X*size.Y {
		p.depth = make([]float32, size.X*size.Y)
	}
	p.draw(buf, p.depth, size, poseAt(t, float32(size.X)/float32(size.Y)))
	return extimage.NewFrame(extimage.Desc{
		Kind:   extimage.KindMemory,
		Format: extimage.FormatRGBA8,
		Size:   size,
		Planes: []extimage.Plane{{Data: buf, Pitch: uint32(size.X * 4)}},
	}, func() { p.put(buf, size) })
}

// buffer is a free buffer for size, or a new one; buffers of another size are dropped.
func (p *cpuProducer) buffer(size image.Point) []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	if size != p.size {
		p.size, p.free = size, nil
	}
	if n := len(p.free); n > 0 {
		b := p.free[n-1]
		p.free = p.free[:n-1]
		return b
	}
	return make([]byte, size.X*size.Y*4)
}

// put is a frame's release: its buffer goes back to the pool if it is still the right size.
func (p *cpuProducer) put(b []byte, size image.Point) {
	p.mu.Lock()
	if size == p.size && len(p.free) < 4 {
		p.free = append(p.free, b)
	}
	p.mu.Unlock()
}

func (p *cpuProducer) close() {}

// draw rasterizes the scene into buf (RGBA8, size), one band of rows per CPU.
func (p *cpuProducer) draw(buf []byte, depth []float32, size image.Point, ps pose) {
	tris := p.triangles(ps, size)
	bands := min(runtime.GOMAXPROCS(0), max(size.Y/16, 1))
	var wg sync.WaitGroup
	for b := range bands {
		y0, y1 := size.Y*b/bands, size.Y*(b+1)/bands
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.band(buf, depth, size, tris, y0, y1)
		}()
	}
	wg.Wait()
}

// screenTri is a triangle in screen space: x, y in pixels, z in [0, 1], 1/w for perspective-
// correct interpolation, UV over w, and the triangle's light.
type screenTri struct {
	x, y, z, iw [3]float32
	uw, vw      [3]float32
	light       float32
}

// triangles is the scene's triangles in screen space, clipped at the near plane.
func (p *cpuProducer) triangles(ps pose, size image.Point) []screenTri {
	var out []screenTri
	emit := func(first, n int, mvp, model mat4) {
		for i := first; i < first+n; i += 3 {
			nv := p.mesh.verts[i].normal
			wn := model.apply([4]float32{nv[0], nv[1], nv[2], 0})
			light := ambient + (1-ambient)*max(0, wn[0]*lightDir[0]+wn[1]*lightDir[1]+wn[2]*lightDir[2])
			var poly [4]clipVert
			for k := range 3 {
				v := p.mesh.verts[i+k]
				poly[k] = clipVert{mvp.apply([4]float32{v.pos[0], v.pos[1], v.pos[2], 1}), v.uv}
			}
			clipped, n := clipNear(poly[:3])
			for k := 1; k+1 < n; k++ { // a fan over the clipped polygon
				out = append(out, project([3]clipVert{clipped[0], clipped[k], clipped[k+1]}, size, light))
			}
		}
	}
	emit(p.mesh.floor[0], p.mesh.floor[1], ps.floorMVP, ps.floorModel)
	emit(p.mesh.crate[0], p.mesh.crate[1], ps.crateMVP, ps.crateModel)
	return out
}

// clipVert is a vertex in clip space with its UV.
type clipVert struct {
	c  [4]float32
	uv [2]float32
}

// clipNear clips a triangle to z ≥ 0 (the near plane in a zero-to-one clip space), giving at
// most four vertices.
func clipNear(in []clipVert) ([4]clipVert, int) {
	var out [4]clipVert
	n := 0
	for i := range in {
		a, b := in[i], in[(i+1)%len(in)]
		if a.c[2] >= 0 {
			out[n] = a
			n++
		}
		if (a.c[2] >= 0) != (b.c[2] >= 0) {
			t := a.c[2] / (a.c[2] - b.c[2])
			var v clipVert
			for k := range 4 {
				v.c[k] = a.c[k] + (b.c[k]-a.c[k])*t
			}
			for k := range 2 {
				v.uv[k] = a.uv[k] + (b.uv[k]-a.uv[k])*t
			}
			out[n] = v
			n++
		}
	}
	return out, n
}

// project maps a clipped triangle to the screen: y down, as the frame's rows run.
func project(v [3]clipVert, size image.Point, light float32) screenTri {
	var t screenTri
	for k := range 3 {
		iw := 1 / v[k].c[3]
		t.x[k] = (v[k].c[0]*iw + 1) / 2 * float32(size.X)
		t.y[k] = (1 - v[k].c[1]*iw) / 2 * float32(size.Y)
		t.z[k] = v[k].c[2] * iw
		t.iw[k] = iw
		t.uw[k], t.vw[k] = v[k].uv[0]*iw, v[k].uv[1]*iw
	}
	t.light = light
	return t
}

// band draws rows y0 to y1: it clears them, then fills each triangle's pixels there that pass
// the depth test.
func (p *cpuProducer) band(buf []byte, depth []float32, size image.Point, tris []screenTri, y0, y1 int) {
	for y := y0; y < y1; y++ {
		row := buf[y*size.X*4 : (y+1)*size.X*4]
		for x := range size.X {
			row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = clearColour.R, clearColour.G, clearColour.B, 0xff
		}
		for x := range size.X {
			depth[y*size.X+x] = 1
		}
	}
	aw, ah := float32(p.atlas.Rect.Dx()), float32(p.atlas.Rect.Dy())
	pix, stride := p.atlas.Pix, p.atlas.Stride
	for i := range tris {
		t := &tris[i]
		area := (t.x[1]-t.x[0])*(t.y[2]-t.y[0]) - (t.x[2]-t.x[0])*(t.y[1]-t.y[0])
		if area == 0 {
			continue
		}
		minX := max(0, int(math.Floor(float64(min(t.x[0], t.x[1], t.x[2])))))
		maxX := min(size.X-1, int(math.Ceil(float64(max(t.x[0], t.x[1], t.x[2])))))
		minY := max(y0, int(math.Floor(float64(min(t.y[0], t.y[1], t.y[2])))))
		maxY := min(y1-1, int(math.Ceil(float64(max(t.y[0], t.y[1], t.y[2])))))
		for y := minY; y <= maxY; y++ {
			py := float32(y) + 0.5
			for x := minX; x <= maxX; x++ {
				px := float32(x) + 0.5
				// Barycentric weights; either winding is drawn (no culling).
				w0 := ((t.x[1]-px)*(t.y[2]-py) - (t.x[2]-px)*(t.y[1]-py)) / area
				w1 := ((t.x[2]-px)*(t.y[0]-py) - (t.x[0]-px)*(t.y[2]-py)) / area
				w2 := 1 - w0 - w1
				if w0 < 0 || w1 < 0 || w2 < 0 {
					continue
				}
				z := w0*t.z[0] + w1*t.z[1] + w2*t.z[2]
				di := y*size.X + x
				if z < 0 || z >= depth[di] {
					continue
				}
				depth[di] = z
				iw := w0*t.iw[0] + w1*t.iw[1] + w2*t.iw[2]
				u := (w0*t.uw[0] + w1*t.uw[1] + w2*t.uw[2]) / iw
				v := (w0*t.vw[0] + w1*t.vw[1] + w2*t.vw[2]) / iw
				tx := min(max(int(u*aw), 0), int(aw)-1)
				ty := min(max(int(v*ah), 0), int(ah)-1)
				s := pix[ty*stride+tx*4:]
				o := buf[di*4:]
				o[0] = uint8(float32(s[0]) * t.light)
				o[1] = uint8(float32(s[1]) * t.light)
				o[2] = uint8(float32(s[2]) * t.light)
				o[3] = 0xff
			}
		}
	}
}
