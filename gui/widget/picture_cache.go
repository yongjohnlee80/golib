package widget

import (
	"cmp"
	"errors"
	"image"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/logger"
)

// PICTURE CACHE — a diagram's or chart's picture drawn once into an image by its window's
// PictureBaker, and painted from that image after: a page of charts scrolled would otherwise fill
// every chart's vector paths at every frame. The image is one picture's (each slot's Ready picture
// is one cachedPicture, and another request is another slot), at one scale and one generation of
// the window's fonts: a change of any of them draws it again, and two pictures never share one,
// whatever drew them. The picture is painted for its image on its view's loop (View's contract);
// a worker puts that on the GPU. Until its image is drawn, and where none can be (no window, no
// headless GPU, a picture past the cache's size), a picture is drawn as it is. The cache keeps
// PictureCacheLimit baked pixels, the least recently painted going first, but never lets go of an
// image painted within onScreenFor for another: when the pictures on screen together are more than
// the limit, those past it stay vector rather than each baking over the others at every frame. It
// lets a picture's images go when its slot drops it (forget), and a window's when the window
// closes.

// PictureCacheLimit is the baked pixels the cache keeps across windows: 48 Mpx, about 192 MB of
// RGBA. A picture larger than it is drawn as it is.
const PictureCacheLimit = 48 << 20

// onScreenFor is how recently an image was painted for it to count as on screen: no picture is
// baked by letting it go. A view paints its pictures together, a frame apart at most, so a second
// covers the slowest frame of a page drawing its pictures from their paths.
const onScreenFor = time.Second

// pictureNow is the cache's clock (a cell's to set).
var pictureNow = time.Now

// pictureBaker is what the cache asks of a window's gui.PictureBaker.
type pictureBaker interface {
	Record(v gui.View, size gui.Size, scale float32) (*gui.Recording, error)
	Draw(rec *gui.Recording) (gui.Baked, error)
	OnRelease(fn func())
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

// bakeKey is one baked picture: its window, the picture (cachedPicture.id), the scale and font
// generation it was drawn at.
type bakeKey struct {
	baker pictureBaker
	pic   uint64
	scale float32
	fonts uint64
}

type bakedPicture struct {
	b    gui.Baked
	used uint64    // the cache's tick at its last paint: the least recently painted goes first
	seen time.Time // its last paint: an image painted within onScreenFor is on screen
}

type bakeJob struct {
	key  bakeKey
	rec  *gui.Recording
	wake func()
}

// pictureCache is every window's baked pictures, and the one worker that draws them in turn.
type pictureCache struct {
	mu      sync.Mutex
	limit   int
	pixels  int
	tick    uint64
	baked   map[bakeKey]*bakedPicture
	pending map[bakeKey]time.Time // asked for, when: a draw not still asked for is not kept
	skip    map[bakeKey]bool      // never baked: past the limit, or its bake failed
	dead    map[pictureBaker]bool // no headless GPU: its window's pictures stay as they are
	watched map[pictureBaker]bool // its release lets its entries go (OnRelease)
	jobs    chan bakeJob
	bakes   atomic.Int64 // draws done and kept or not, for the cells
}

func newPictureCache(limit int) *pictureCache {
	return &pictureCache{limit: limit, baked: map[bakeKey]*bakedPicture{}, pending: map[bakeKey]time.Time{},
		skip: map[bakeKey]bool{}, dead: map[pictureBaker]bool{}, watched: map[pictureBaker]bool{}}
}

var pictures = newPictureCache(PictureCacheLimit)

// skipLimit bounds the pictures kept as never to be baked: past it they are forgotten, and asked
// about again (a size is checked at once; a failed bake is tried once more).
const skipLimit = 4096

// pictureIDs numbers the cachedPictures.
var pictureIDs atomic.Uint64

// get is k's image, marked as just used.
func (pc *pictureCache) get(k bakeKey) (gui.Baked, bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	e, ok := pc.baked[k]
	if !ok {
		return gui.Baked{}, false
	}
	pc.tick++
	e.used, e.seen = pc.tick, pictureNow()
	return e.b, true
}

// reserve claims k for a bake of px pixels: false when it is drawn, asked for, never to be, its
// window has no GPU, or it would only fit by letting an image on screen go (it is asked again at
// its next paint). watch is whether k's window is new to the cache: its release is to be heard.
func (pc *pictureCache) reserve(k bakeKey, px image.Point) (ok, watch bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	_, asked := pc.pending[k]
	if pc.dead[k.baker] || asked || pc.skip[k] || pc.baked[k] != nil {
		return false, false
	}
	if px.X*px.Y > pc.limit || px.X <= 0 || px.Y <= 0 {
		pc.skipOne(k)
		return false, false
	}
	now := pictureNow()
	if _, fits := pc.room(px.X*px.Y, now); !fits {
		return false, false
	}
	pc.pending[k] = now
	if !pc.watched[k.baker] {
		pc.watched[k.baker] = true
		return true, true
	}
	return true, false
}

// failed takes a recording's or a draw's failure: no GPU marks the window's baker, another the
// picture, each said once. Under pc.mu.
func (pc *pictureCache) failed(k bakeKey, err error) {
	switch {
	case errors.Is(err, gui.ErrBakerClosed):
	case errors.Is(err, gui.ErrNoGPU):
		if !pc.dead[k.baker] {
			pc.dead[k.baker] = true
			logPicture("no headless GPU: the window's charts and diagrams are drawn from their paths at every frame", err)
		}
	default:
		pc.skipOne(k)
		logPicture("a picture could not be drawn into an image: it is drawn from its paths", err)
	}
}

// skipOne keeps k as never to be baked, the kept ones forgotten past skipLimit. Under pc.mu.
func (pc *pictureCache) skipOne(k bakeKey) {
	if len(pc.skip) >= skipLimit {
		clear(pc.skip)
	}
	pc.skip[k] = true
}

// enqueue hands j to the worker, starting it the first time; a full queue lets k go, to be asked
// again at the next paint.
func (pc *pictureCache) enqueue(j bakeJob) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if pc.jobs == nil {
		pc.jobs = make(chan bakeJob, 64)
		go pc.work(pc.jobs)
	}
	select {
	case pc.jobs <- j:
	default:
		delete(pc.pending, j.key)
	}
}

// work draws each job in turn, off the loops.
func (pc *pictureCache) work(jobs <-chan bakeJob) {
	for j := range jobs {
		b, err := j.key.baker.Draw(j.rec)
		pc.mu.Lock()
		asked, still := pc.pending[j.key]
		delete(pc.pending, j.key)
		kept := false
		switch {
		case !still || !pc.watched[j.key.baker]: // its slot or its window let it go while it was drawn
		case err != nil:
			pc.failed(j.key, err)
		default:
			kept = pc.put(j.key, b, asked)
		}
		pc.mu.Unlock()
		pc.bakes.Add(1)
		if kept && j.wake != nil {
			j.wake()
		}
	}
}

// put keeps b as k's image, asked for at asked, letting the least recently painted go until it
// fits: false, and nothing let go, when that would let go of an image painted within onScreenFor
// of asked or since, on screen with k's picture. Under pc.mu.
func (pc *pictureCache) put(k bakeKey, b gui.Baked, asked time.Time) bool {
	px := b.Pixels.X * b.Pixels.Y
	out, fits := pc.room(px, asked)
	if !fits {
		return false
	}
	for _, key := range out {
		pc.drop(key)
	}
	pc.tick++
	pc.baked[k] = &bakedPicture{b: b, used: pc.tick, seen: asked}
	pc.pixels += px
	return true
}

// room is the images to let go, least recently painted first, for px more pixels to fit the
// limit: fits is false when one of them was painted within onScreenFor of at, or after it (on
// screen with the picture asking), or px is past the limit. Under pc.mu.
func (pc *pictureCache) room(px int, at time.Time) (out []bakeKey, fits bool) {
	over := pc.pixels + px - pc.limit
	if over <= 0 {
		return nil, true
	}
	if px > pc.limit {
		return nil, false
	}
	for key := range pc.baked {
		out = append(out, key)
	}
	slices.SortFunc(out, func(a, b bakeKey) int { return cmp.Compare(pc.baked[a].used, pc.baked[b].used) })
	since := at.Add(-onScreenFor)
	for i, key := range out {
		e := pc.baked[key]
		if e.seen.After(since) {
			return nil, false
		}
		if over -= e.b.Pixels.X * e.b.Pixels.Y; over <= 0 {
			return out[:i+1], true
		}
	}
	return nil, false
}

// drop lets k's image go. Under pc.mu.
func (pc *pictureCache) drop(k bakeKey) {
	if e := pc.baked[k]; e != nil {
		pc.pixels -= e.b.Pixels.X * e.b.Pixels.Y
		delete(pc.baked, k)
	}
}

// release lets everything of a closed window's baker go, so neither its images nor the window are
// kept.
func (pc *pictureCache) release(baker pictureBaker) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	for k := range pc.baked {
		if k.baker == baker {
			pc.drop(k)
		}
	}
	for k := range pc.pending {
		if k.baker == baker {
			delete(pc.pending, k)
		}
	}
	for k := range pc.skip {
		if k.baker == baker {
			delete(pc.skip, k)
		}
	}
	delete(pc.dead, baker)
	delete(pc.watched, baker)
}

// forget lets every image of picture id go, and its draws in flight: its slot dropped it.
func (pc *pictureCache) forget(id uint64) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	for k := range pc.baked {
		if k.pic == id {
			pc.drop(k)
		}
	}
	for k := range pc.pending {
		if k.pic == id {
			delete(pc.pending, k)
		}
	}
	for k := range pc.skip {
		if k.pic == id {
			delete(pc.skip, k)
		}
	}
}

func logPicture(what string, err error) {
	if l := pictureLog.Load(); l != nil {
		logger.Info(*l, map[string]any{"event": "picture cache", "what": what, "reason": err.Error()})
	}
}

// cachedPicture is a diagram's picture (pic, of size), painted from its baked image when its window
// has drawn one, and as it is until then. id is its own: no other picture's image is ever its.
// wake marks the view that shows it to paint again, when its image is drawn.
type cachedPicture struct {
	id   uint64
	pc   *pictureCache // the cache it was made under: its images are there
	pic  gui.View
	size gui.Size
	wake func()
}

func newCachedPicture(pic gui.View, size gui.Size, wake func()) *cachedPicture {
	return &cachedPicture{id: pictureIDs.Add(1), pc: pictures, pic: pic, size: size, wake: wake}
}

// Paint is on the view's loop (View's contract): it paints the picture's image, or the picture
// and, the first time, the picture again for its image, which the worker draws.
func (p *cachedPicture) Paint(c gui.Canvas) {
	pb := bakerOf(c)
	if pb == nil {
		p.pic.Paint(c)
		return
	}
	k := bakeKey{baker: pb, pic: p.id, scale: c.Scale(), fonts: c.Fonts().Gen}
	if b, ok := p.pc.get(k); ok {
		gui.DrawBaked(c, b, gui.Point{})
		return
	}
	p.pic.Paint(c)
	ok, watch := p.pc.reserve(k, gui.BakedPixels(p.size, k.scale))
	if !ok {
		return
	}
	if watch {
		pb.OnRelease(func() { p.pc.release(pb) })
	}
	rec, err := pb.Record(p.pic, p.size, k.scale)
	if err != nil {
		p.pc.mu.Lock()
		delete(p.pc.pending, k)
		p.pc.failed(k, err)
		p.pc.mu.Unlock()
		return
	}
	p.pc.enqueue(bakeJob{key: k, rec: rec, wake: p.wake})
}
