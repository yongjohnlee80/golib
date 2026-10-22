package gui

import "github.com/yongjohnlee80/golib/tui"

// imageTable is the Backend's images: the latch PlaceImage and DeleteImage fill, and the images
// shown, each decoded once per version, in the order placed. The tui loop owns it; a frame takes
// the latch's calls (apply) and then the list.
type imageTable struct {
	latch  tui.ImageLatch
	placed map[uint32]placedImage // by ID
	order  []uint32               // placement order
}

func newImageTable() imageTable { return imageTable{placed: map[uint32]placedImage{}} }

// apply applies the latched placements and deletes, in the order made. The PNG is decoded once
// per version: a placement that only moves or changes its Clip reuses it.
func (t *imageTable) apply() {
	for _, op := range t.latch.Take() {
		if op.Place == nil {
			t.remove(op.Delete)
			continue
		}
		p := *op.Place
		old, had := t.placed[p.ID]
		if had && old.Version == p.Version {
			old.ImagePlacement = p
			t.placed[p.ID] = old
			continue
		}
		img, ok := decodeImage(p)
		if !ok {
			continue // not a PNG: the cells beneath show, as on a terminal that refuses it
		}
		if !had {
			t.order = append(t.order, p.ID)
		}
		t.placed[p.ID] = img
	}
}

func (t *imageTable) remove(id uint32) {
	if _, ok := t.placed[id]; !ok {
		return
	}
	delete(t.placed, id)
	for i, v := range t.order {
		if v == id {
			t.order = append(t.order[:i], t.order[i+1:]...)
			break
		}
	}
}

// list is the images shown, in the order placed: a frame's snapshot.
func (t *imageTable) list() []placedImage {
	out := make([]placedImage, 0, len(t.order))
	for _, id := range t.order {
		out = append(out, t.placed[id])
	}
	return out
}
