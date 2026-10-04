package vector

import (
	"bytes"
	"cmp"
	"errors"
	"math"
	"reflect"
	"slices"
	"testing"
)

func TestMath(t *testing.T) {
	n := Normalize([]float32{3, 4})
	if n[0] != 0.6 || n[1] != 0.8 {
		t.Errorf("Normalize(3, 4) = %v", n)
	}
	if z := Normalize([]float32{0, 0}); z[0] != 0 || z[1] != 0 {
		t.Errorf("a zero vector: %v", z)
	}
	if d := Dot([]float32{1, 2, 3}, []float32{4, 5}); d != 14 {
		t.Errorf("Dot over the common length = %v", d)
	}
	v := make([]float32, 70)
	v[0], v[63], v[64], v[69] = 1, 0.5, -1, 2
	b := SignBits(v)
	if len(b) != 2 || b[0] != 1|1<<63 || b[1] != 1<<5 {
		t.Errorf("SignBits = %b", b)
	}
	if h := Hamming([]uint64{0b1011, 0xff}, []uint64{0b0001}); h != 2 {
		t.Errorf("Hamming over the common length = %d", h)
	}
	if got := DecodeBits(EncodeBits(b)); !reflect.DeepEqual(got, b) {
		t.Errorf("bits round trip: %v", got)
	}
	f := []float32{1.5, -2, float32(math.Pi)}
	if got := DecodeFloats(EncodeFloats(f)); !reflect.DeepEqual(got, f) {
		t.Errorf("floats round trip: %v", got)
	}
	// the encodings are little-endian and fixed-size: stored bytes stay readable
	if !bytes.Equal(EncodeBits([]uint64{1}), []byte{1, 0, 0, 0, 0, 0, 0, 0}) || !bytes.Equal(EncodeFloats([]float32{1}), []byte{0, 0, 0x80, 0x3f}) {
		t.Error("the byte layout changed")
	}
}

func codesOf[D comparable, C any](x *Index[D, C]) []C {
	var out []C
	for c := range x.Codes() {
		out = append(out, c.Chunk)
	}
	return out
}

func TestIndexNext(t *testing.T) {
	keep := []Code[int64]{{1, []uint64{1}}, {2, []uint64{2}}}
	x := NewIndex("m", 5, map[string][]Code[int64]{"a": keep, "b": {{3, []uint64{3}}}})
	same := x.Next(6, nil, nil)
	if same.Watermark() != 6 || same.Model() != "m" || !reflect.DeepEqual(sorted(codesOf(same)), []int64{1, 2, 3}) {
		t.Errorf("nothing changed: %+v", same)
	}
	y := x.Next(7, []string{"b", "c"}, map[string][]Code[int64]{"c": {{9, []uint64{9}}}})
	if !reflect.DeepEqual(sorted(codesOf(y)), []int64{1, 2, 9}) {
		t.Errorf("b changed to nothing, c added: %v", codesOf(y))
	}
	if &y.docs["a"][0] != &keep[0] {
		t.Error("an unchanged document's codes were copied, not shared")
	}
	if !reflect.DeepEqual(sorted(codesOf(x)), []int64{1, 2, 3}) || x.Watermark() != 5 {
		t.Error("Next changed the index it was made from")
	}
	if n := len(codesOf(NewIndex[string, int64]("m", 0, nil))); n != 0 {
		t.Errorf("an empty index has %d codes", n)
	}
}

func sorted(s []int64) []int64 { slices.Sort(s); return s }

func TestIndexUsable(t *testing.T) {
	x := NewIndex[string, int64]("m", 5, nil)
	for _, c := range []struct {
		model string
		mark  int64
		want  bool
	}{{"m", 5, true}, {"m", 4, false}, {"m", 6, false}, {"other", 5, false}} {
		if got := x.Usable(c.model, c.mark); got != c.want {
			t.Errorf("Usable(%q, %d) = %v", c.model, c.mark, got)
		}
	}
	var none *Index[string, int64]
	if none.Usable("m", 5) {
		t.Error("a nil index is usable")
	}
}

// key has no natural order: a 16-byte identifier.
type key [16]byte

func k(b byte) key { var x key; x[15] = b; x[0] = 255 - b; return x }

func compareKeys(a, b key) int { return bytes.Compare(a[:], b[:]) }

func TestNearestBreaksTiesByTheComparator(t *testing.T) {
	q := []uint64{0b1111}
	codes := []Code[key]{{k(3), []uint64{0b1111}}, {k(1), []uint64{0b0111}}, {k(2), []uint64{0b1111}}, {k(4), []uint64{0b0000}}}
	want := []key{k(3), k(2), k(1), k(4)} // distance 0 (k(3) < k(2) by bytes: 252 < 253), 1, 4
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {1, 3, 0, 2}} {
		var in []Code[key]
		for _, i := range order {
			in = append(in, codes[i])
		}
		if got := Nearest(slices.Values(in), q, compareKeys); !reflect.DeepEqual(got, want) {
			t.Errorf("order %v: %v, want %v", order, got, want)
		}
	}
	if got := Nearest(slices.Values([]Code[int64]{{2, []uint64{1}}, {1, []uint64{1}}}), []uint64{1}, cmp.Compare[int64]); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Errorf("int64 keys: %v", got)
	}
}

type item struct {
	path string
	ord  int
	f32  []float32
}

func lessItem(a, b item) bool {
	if a.path != b.path {
		return a.path < b.path
	}
	return a.ord < b.ord
}

// TestTwoStage: valid items behind more than one window of stale codes are reached; equal dot
// products order by less.
func TestTwoStage(t *testing.T) {
	var order []key
	vecs := map[key]item{}
	for i := range 450 { // 450 stale codes first
		order = append(order, k(byte(i%200)))
	}
	valid := []item{{"b.md", 0, []float32{1, 0}}, {"a.md", 1, []float32{1, 0}}, {"a.md", 0, []float32{0.5, 0.5}}, {"c.md", 0, []float32{0, 1}}}
	for i, it := range valid {
		kk := key{1, byte(i)}
		order = append(order, kk)
		vecs[kk] = it
	}
	var windows int
	fetch := func(ids []key) ([]Vec[item], error) {
		windows++
		var out []Vec[item]
		for _, id := range ids {
			if it, ok := vecs[id]; ok {
				out = append(out, Vec[item]{Item: it, F32: it.f32})
			}
		}
		return out, nil
	}
	got, err := TwoStage(order, []float32{1, 0}, 200, 3, fetch, lessItem)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, v := range got {
		names = append(names, v.Item.path+"#"+string(rune('0'+v.Item.ord)))
	}
	if want := []string{"a.md#1", "b.md#0", "a.md#0"}; !reflect.DeepEqual(names, want) {
		t.Errorf("TwoStage = %v, want %v", names, want)
	}
	if windows != 3 || got[0].Dot != 1 {
		t.Errorf("%d windows (want 3), dot %v", windows, got[0].Dot)
	}

	windows = 0
	stop := func(ids []key) ([]Vec[item], error) { windows++; return nil, SkipRest }
	if got, err := TwoStage(order, []float32{1, 0}, 200, 3, stop, lessItem); err != nil || len(got) != 0 || windows != 1 {
		t.Errorf("SkipRest: %v, %v after %d windows", got, err, windows)
	}
	boom := errors.New("boom")
	if _, err := TwoStage(order, []float32{1, 0}, 200, 3, func([]key) ([]Vec[item], error) { return nil, boom }, lessItem); !errors.Is(err, boom) {
		t.Errorf("a fetch error: %v", err)
	}
	if got, _ := TwoStage(order, []float32{1, 0}, 0, 0, fetch, lessItem); len(got) != 0 {
		t.Errorf("n = 0: %v", got)
	}
}
