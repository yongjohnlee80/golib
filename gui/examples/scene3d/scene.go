package main

import (
	"image"
	"image/color"
	"math"
)

// The scene every producer draws: a textured crate turning over a checkered floor, lit by one
// directional light. Its mesh, texture and matrices are plain data, so the CPU, Vulkan and Metal
// producers draw the same picture.

// vertex is one mesh vertex: position, normal and texture coordinate.
type vertex struct {
	pos, normal [3]float32
	uv          [2]float32
}

// mesh is the scene's geometry: the crate's 36 vertices, then the floor's 6. Each draws as a
// triangle list, with depth testing and no face culling.
type mesh struct {
	verts        []vertex
	crate, floor [2]int // first vertex, count
}

// atlasW, atlasH is the texture atlas: the crate's face in the left half, the floor's tile in the
// right half.
const atlasW, atlasH = 512, 256

func newMesh() mesh {
	var m mesh
	// Each face: its normal, and the two axes its square spans. UVs are the atlas's left half.
	faces := []struct{ n, u, v [3]float32 }{
		{n: [3]float32{0, 0, 1}, u: [3]float32{1, 0, 0}, v: [3]float32{0, 1, 0}},
		{n: [3]float32{0, 0, -1}, u: [3]float32{-1, 0, 0}, v: [3]float32{0, 1, 0}},
		{n: [3]float32{1, 0, 0}, u: [3]float32{0, 0, -1}, v: [3]float32{0, 1, 0}},
		{n: [3]float32{-1, 0, 0}, u: [3]float32{0, 0, 1}, v: [3]float32{0, 1, 0}},
		{n: [3]float32{0, 1, 0}, u: [3]float32{1, 0, 0}, v: [3]float32{0, 0, -1}},
		{n: [3]float32{0, -1, 0}, u: [3]float32{1, 0, 0}, v: [3]float32{0, 0, 1}},
	}
	corner := func(f struct{ n, u, v [3]float32 }, su, sv float32) vertex {
		var p [3]float32
		for i := range p {
			p[i] = f.n[i]*0.5 + f.u[i]*su*0.5 + f.v[i]*sv*0.5
		}
		// v grows downwards in the texture: the face's top edge (sv = 1) is v = 0.
		return vertex{pos: p, normal: f.n, uv: [2]float32{(su + 1) / 4, (1 - sv) / 2}}
	}
	m.crate[0] = len(m.verts)
	for _, f := range faces {
		a, b, c, d := corner(f, -1, -1), corner(f, 1, -1), corner(f, 1, 1), corner(f, -1, 1)
		m.verts = append(m.verts, a, b, c, a, c, d) // counter-clockwise seen from outside
	}
	m.crate[1] = len(m.verts) - m.crate[0]
	// The floor: a square of side 8 below the turning crate's reach, over the atlas's right half
	// once.
	const side, y = 4, -1.1
	up := [3]float32{0, 1, 0}
	fv := func(x, z, u, v float32) vertex {
		return vertex{pos: [3]float32{x, y, z}, normal: up, uv: [2]float32{u, v}}
	}
	a, b, c, d := fv(-side, side, 0.5, 1), fv(side, side, 1, 1), fv(side, -side, 1, 0), fv(-side, -side, 0.5, 0)
	m.floor[0] = len(m.verts)
	m.verts = append(m.verts, a, b, c, a, c, d) // counter-clockwise seen from above
	m.floor[1] = 6
	return m
}

// newAtlas draws the texture atlas: a crate face (planks in a frame, with a cross brace) and the
// floor (a two-tone checker of 8×8 squares, one scene unit each, with grout lines).
func newAtlas() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, atlasW, atlasH))
	wood := func(x, y int) color.RGBA {
		grain := int(18 * math.Sin(float64(y)*0.35+math.Sin(float64(x)*0.05)*3)) // -18..18
		plank := 0
		if (y/32)%2 == 1 {
			plank = 14
		}
		return color.RGBA{R: uint8(0xb0 - plank + grain), G: uint8(0x78 - plank + grain/2), B: uint8(0x40 - plank/2), A: 0xff}
	}
	const half = atlasH // the crate's half is square
	for y := range atlasH {
		for x := range half {
			c := wood(x, y)
			frame := x < 20 || y < 20 || x >= half-20 || y >= half-20
			brace := abs(x-y) < 14 || abs(x-(half-1-y)) < 14
			if frame || brace {
				c = color.RGBA{R: 0x8a, G: 0x58, B: 0x2c, A: 0xff}
			}
			if y%32 == 0 && !frame && !brace {
				c = color.RGBA{R: 0x5a, G: 0x38, B: 0x1c, A: 0xff} // the gap between planks
			}
			img.SetRGBA(x, y, c)
		}
		for x := range half {
			c := color.RGBA{R: 0xd8, G: 0xd8, B: 0xe0, A: 0xff}
			if (x/32+y/32)%2 == 1 {
				c = color.RGBA{R: 0x30, G: 0x58, B: 0x80, A: 0xff}
			}
			if x%32 < 1 || y%32 < 1 {
				c = color.RGBA{R: 0x60, G: 0x60, B: 0x68, A: 0xff}
			}
			img.SetRGBA(half+x, y, c)
		}
	}
	return img
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// mat4 is a column-major 4×4 matrix, as GLSL and MSL lay one out.
type mat4 [16]float32

func identity() mat4 { return mat4{0: 1, 5: 1, 10: 1, 15: 1} }

func (a mat4) mul(b mat4) mat4 {
	var r mat4
	for c := range 4 {
		for row := range 4 {
			var s float32
			for k := range 4 {
				s += a[k*4+row] * b[c*4+k]
			}
			r[c*4+row] = s
		}
	}
	return r
}

// apply is a·(x, y, z, w).
func (a mat4) apply(v [4]float32) [4]float32 {
	var r [4]float32
	for row := range 4 {
		r[row] = a[row]*v[0] + a[4+row]*v[1] + a[8+row]*v[2] + a[12+row]*v[3]
	}
	return r
}

func translate(x, y, z float32) mat4 {
	m := identity()
	m[12], m[13], m[14] = x, y, z
	return m
}

func rotateY(a float64) mat4 {
	s, c := float32(math.Sin(a)), float32(math.Cos(a))
	m := identity()
	m[0], m[2], m[8], m[10] = c, -s, s, c
	return m
}

func rotateX(a float64) mat4 {
	s, c := float32(math.Sin(a)), float32(math.Cos(a))
	m := identity()
	m[5], m[6], m[9], m[10] = c, s, -s, c
	return m
}

// perspective maps view space (camera at the origin, looking down -z) to clip space with depth
// 0 at near and 1 at far, as Vulkan and Metal take it. Clip y points up; a producer whose
// framebuffer's y points down (Vulkan) flips it in its viewport.
func perspective(fovY, aspect, near, far float32) mat4 {
	f := 1 / float32(math.Tan(float64(fovY)/2))
	return mat4{
		0:  f / aspect,
		5:  f,
		10: far / (near - far), 11: -1,
		14: near * far / (near - far),
	}
}

// pose is the scene at time t (seconds): the crate's and the floor's model-view-projection and
// model matrices, for a viewport of aspect w/h.
type pose struct {
	crateMVP, crateModel mat4
	floorMVP, floorModel mat4
}

func poseAt(t float64, aspect float32) pose {
	proj := perspective(math.Pi/4, aspect, 0.1, 50)
	view := translate(0, -0.6, -4.2).mul(rotateX(0.38))
	crate := translate(0, 0.25*float32(math.Sin(t*1.3)), 0).mul(rotateY(t * 0.9)).mul(rotateX(t * 0.4))
	floor := rotateY(t * 0.15)
	vp := proj.mul(view)
	return pose{crateMVP: vp.mul(crate), crateModel: crate, floorMVP: vp.mul(floor), floorModel: floor}
}

// drawsAt is the scene's two draws at t for a GPU producer's push constants: the floor's mvp and
// model, then the crate's.
func drawsAt(t float64, size image.Point) [64]float32 {
	ps := poseAt(t, float32(size.X)/float32(size.Y))
	var d [64]float32
	copy(d[0:], ps.floorMVP[:])
	copy(d[16:], ps.floorModel[:])
	copy(d[32:], ps.crateMVP[:])
	copy(d[48:], ps.crateModel[:])
	return d
}

// lightDir is the direction towards the light, in world space, normalized.
var lightDir = func() [3]float32 {
	v := [3]float32{0.4, 0.8, 0.45}
	n := float32(math.Sqrt(float64(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])))
	return [3]float32{v[0] / n, v[1] / n, v[2] / n}
}()

// ambient is the light every surface gets; the directional light adds the rest.
const ambient = 0.35

// clearColour is the sky behind the scene.
var clearColour = color.RGBA{R: 0x1a, G: 0x1d, B: 0x2b, A: 0xff}
