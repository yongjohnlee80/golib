package widget

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/logger"
)

// PICTURE CACHE — a diagram's or chart's picture drawn once into an image by its window's
// PictureBaker, and painted from that image after: a page of charts scrolled would otherwise fill
// every chart's vector paths at every frame. The image is the picture's at one scale and one
// generation of the window's fonts, and a picture is its diagram's request (source, width, scale,
// theme: diagramKey), so a change of any of them draws it again. Until its image is drawn, and
// where none can be (no window, no headless GPU, a picture past the cache's size), a picture is
// drawn as it is. The cache keeps PictureCacheLimit baked pixels, the least recently painted going
// first.

// PictureCacheLimit is the baked pixels the cache keeps across windows: 48 Mpx, about 192 MB of
// RGBA. A picture larger than it is drawn as it is.
const PictureCacheLimit = 48 << 20

// pictureBaker is what the cache asks of a window's gui.PictureBaker.
type pictureBaker interface {
	Bake(v gui.View, size gui.Size, scale float32) (gui.Baked, error)
}

// bakerOf is the baker of the window c paints in, nil off a window.
var bakerOf = func(c gui.Canvas) pictureBaker {
	if b := gui.BakerOf(c); b != nil {
		return b
	}
	return nil
}

// pictureLog is where the cache says, once per window, why its pictures stay vector.
var pictureLog atomic.Pointer[logger.Logger]

// SetPictureLogger is where the picture cache says, at Info and once per window, why a window's
// pictures are drawn as they are rather than from images: no headless GPU, or a bake that failed.
func SetPictureLogger(l logger.Logger) {
	if l == nil {
		pictureLog.Store(nil)
		return
	}
	pictureLog.Store(&l)
}

// bakeKey is one baked picture: its window, its request, the scale and font generation it was
// drawn at.
type bakeKey struct {
	baker pictureBaker
	pic   diagramKey
	scale float32
	fonts uint64
}

type bakedPicture struct {
	b    gui.Baked
	used uint64
}

type bakeJob struct {
	key  bakeKey
	pic  gui.View
	size gui.Size
	wake func()
}

// pictureCache is every window's baked pictures, and the one worker that bakes them in turn.
type pictureCache struct {
	mu      sync.Mutex
	limit   int
	pixels  int
	tick    uint64
	baked   map[bakeKey]*bakedPicture
	pending map[bakeKey]bool
	skip    map[bakeKey]bool      // never baked: past the limit, or its bake failed
	dead    map[pictureBaker]bool // no headless GPU: its window's pictures stay as they are
	jobs    chan bakeJob
	bakes   atomic.Int64 // bakes done, for the cells
}

func newPictureCache(limit int) *pictureCache {
	return &pictureCache{limit: limit, baked: map[bakeKey]*bakedPicture{}, pending: map[bakeKey]bool{},
		skip: map[bakeKey]bool{}, dead: map[pictureBaker]bool{}}
}

var pictures = newPictureCache(PictureCacheLimit)

// get is k's image, marked as just used.
func (pc *pictureCache) get(k bakeKey) (gui.Baked, bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	e, ok := pc.baked[k]
	if !ok {
		return gui.Baked{}, false
	}
	pc.tick++
	e.used = pc.tick
	return e.b, true
}

// request asks for k's image, unless it is drawn, asked for, never to be, or its window has no GPU.
// The worker bakes it, then calls wake.
func (pc *pictureCache) request(j bakeJob) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if pc.dead[j.key.baker] || pc.pending[j.key] || pc.skip[j.key] || pc.baked[j.key] != nil {
		return
	}
	px := gui.BakedPixels(j.size, j.key.scale)
	if px.X*px.Y > pc.limit || px.X <= 0 || px.Y <= 0 {
		pc.skip[j.key] = true
		return
	}
	if pc.jobs == nil {
		pc.jobs = make(chan bakeJob, 64)
		go pc.work(pc.jobs)
	}
	select {
	case pc.jobs <- j:
		pc.pending[j.key] = true
	default: // the worker is behind: the next paint asks again
	}
}

// work bakes each job in turn, off the loops.
func (pc *pictureCache) work(jobs <-chan bakeJob) {
	for j := range jobs {
		b, err := j.key.baker.Bake(j.pic, j.size, j.key.scale)
		pc.bakes.Add(1)
		pc.mu.Lock()
		delete(pc.pending, j.key)
		switch {
		case errors.Is(err, gui.ErrNoGPU):
			if !pc.dead[j.key.baker] {
				pc.dead[j.key.baker] = true
				logPicture("no headless GPU: the window's charts and diagrams are drawn from their paths at every frame", err)
			}
		case err != nil:
			pc.skip[j.key] = true
			logPicture("a picture could not be drawn into an image: it is drawn from its paths", err)
		default:
			pc.put(j.key, b)
		}
		pc.mu.Unlock()
		if err == nil && j.wake != nil {
			j.wake()
		}
	}
}

// put keeps b as k's image, letting the least recently painted go until it fits.
func (pc *pictureCache) put(k bakeKey, b gui.Baked) {
	px := b.Pixels.X * b.Pixels.Y
	for pc.pixels+px > pc.limit && len(pc.baked) > 0 {
		var oldest bakeKey
		var at uint64
		first := true
		for key, e := range pc.baked {
			if first || e.used < at {
				oldest, at, first = key, e.used, false
			}
		}
		o := pc.baked[oldest]
		pc.pixels -= o.b.Pixels.X * o.b.Pixels.Y
		delete(pc.baked, oldest)
	}
	pc.tick++
	pc.baked[k] = &bakedPicture{b: b, used: pc.tick}
	pc.pixels += px
}

func logPicture(what string, err error) {
	if l := pictureLog.Load(); l != nil {
		logger.Info(*l, map[string]any{"event": "picture cache", "what": what, "reason": err.Error()})
	}
}

// cachedPicture is a diagram's picture (pic, of size, for its request key), painted from its
// baked image when its window has drawn one, and as it is until then. wake marks the view that
// shows it to paint again, when its image is drawn.
type cachedPicture struct {
	pic  gui.View
	key  diagramKey
	size gui.Size
	wake func()
}

func (p *cachedPicture) Paint(c gui.Canvas) {
	pb := bakerOf(c)
	if pb == nil {
		p.pic.Paint(c)
		return
	}
	k := bakeKey{baker: pb, pic: p.key, scale: c.Scale(), fonts: c.Fonts().Gen}
	if b, ok := pictures.get(k); ok {
		gui.DrawBaked(c, b, gui.Point{})
		return
	}
	p.pic.Paint(c)
	pictures.request(bakeJob{key: k, pic: p.pic, size: p.size, wake: p.wake})
}
