package vector

import (
	"cmp"
	"errors"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
)

// tiedCodes is n codes of 4 bits each, so many share a distance and the tie-break decides.
func tiedCodes(n int) map[int][]Code[int64] {
	r := rand.New(rand.NewPCG(5, 6))
	out := map[int][]Code[int64]{}
	for i := range n {
		out[i%37] = append(out[i%37], Code[int64]{Chunk: int64(i), Bits: []uint64{r.Uint64() & 0xf}})
	}
	return out
}

// TestNearestSeqIsNearestsOrder: taken whole, NearestSeq yields Nearest's order, ties included;
// stopped after k, its first k.
func TestNearestSeqIsNearestsOrder(t *testing.T) {
	x := NewIndex("m", 1, tiedCodes(500))
	q := []uint64{0b1010}
	want := Nearest(x.Codes(), q, cmp.Compare[int64])
	if got := slices.Collect(NearestSeq(x.Codes(), q, cmp.Compare[int64])); !reflect.DeepEqual(got, want) {
		t.Fatalf("NearestSeq's order differs from Nearest's:\n%v\n%v", got[:20], want[:20])
	}
	var first []int64
	for c := range NearestSeq(x.Codes(), q, cmp.Compare[int64]) {
		first = append(first, c)
		if len(first) == 7 {
			break
		}
	}
	if !reflect.DeepEqual(first, want[:7]) {
		t.Fatalf("NearestSeq's first 7 %v, Nearest's %v", first, want[:7])
	}
	if got := slices.Collect(NearestSeq(NewIndex[int, int64]("m", 0, nil).Codes(), q, cmp.Compare[int64])); len(got) != 0 {
		t.Fatalf("an empty index yields %v", got)
	}
}

// TestTwoStageSeqIsTwoStage: over the same order, with a fetch that drops some ids (stale, or
// filtered out), TwoStageSeq returns what TwoStage returns: n below, at and above what survives,
// with SkipRest, and with n of 0.
func TestTwoStageSeqIsTwoStage(t *testing.T) {
	x := NewIndex("m", 1, tiedCodes(300))
	q := []uint64{0b0110}
	order := Nearest(x.Codes(), q, cmp.Compare[int64])
	qf := []float32{1, 2, 3}
	vecOf := func(id int64) []float32 { return []float32{float32(id % 7), float32(id % 5), float32(id % 3)} }
	less := func(a, b int64) bool { return a < b }
	for _, tc := range []struct {
		name         string
		window, n    int
		skipAfterOne bool
	}{
		{"few", 20, 5, false}, {"many", 20, 90, false}, {"more than survive", 50, 1000, false},
		{"window of one", 1, 3, false}, {"skip rest", 20, 90, true}, {"none wanted", 20, 0, false},
	} {
		fetches := 0
		fetch := func(ids []int64) ([]Vec[int64], error) {
			fetches++
			if tc.skipAfterOne && fetches > 1 {
				return nil, SkipRest
			}
			var out []Vec[int64]
			for _, id := range ids {
				if id%3 != 0 { // a third are stale
					out = append(out, Vec[int64]{Item: id, F32: vecOf(id)})
				}
			}
			return out, nil
		}
		want, err1 := TwoStage(order, qf, tc.window, tc.n, fetch, less)
		fetches = 0
		got, err2 := TwoStageSeq(NearestSeq(x.Codes(), q, cmp.Compare[int64]), qf, tc.window, tc.n, fetch, less)
		if err1 != nil || err2 != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: TwoStageSeq %v (%v), TwoStage %v (%v)", tc.name, got, err2, want, err1)
		}
	}
	boom := errors.New("boom")
	if _, err := TwoStageSeq(slices.Values(order), qf, 10, 5, func([]int64) ([]Vec[int64], error) { return nil, boom }, less); !errors.Is(err, boom) {
		t.Errorf("a fetch's error came back as %v", err)
	}
}

// TestAFetchMayKeepItsWindow: each window handed to fetch is its own, as TwoStage's sub-slices of
// the order were, so a fetch that keeps one sees it unchanged after the next.
func TestAFetchMayKeepItsWindow(t *testing.T) {
	var kept [][]int64
	fetch := func(ids []int64) ([]Vec[int64], error) {
		kept = append(kept, ids)
		return nil, nil
	}
	if _, err := TwoStageSeq(slices.Values([]int64{1, 2, 3, 4, 5}), nil, 2, 10, fetch, func(a, b int64) bool { return a < b }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kept, [][]int64{{1, 2}, {3, 4}, {5}}) {
		t.Fatalf("the windows fetch kept: %v", kept)
	}
}

// TestNextCopiesOnlyTheShardsItChanges: a commit that changes one document copies that document's
// shard and shares the others; the index it was made from is unchanged.
func TestNextCopiesOnlyTheShardsItChanges(t *testing.T) {
	x := NewIndex("m", 1, tiedCodes(3000))
	before := slices.Collect(x.Codes())
	y := x.Next(2, []int{5}, map[int][]Code[int64]{5: {{Chunk: 99999, Bits: []uint64{1}}}})
	copied := 0
	for i := range shardCount {
		if reflect.ValueOf(x.shards[i]).Pointer() != reflect.ValueOf(y.shards[i]).Pointer() {
			copied++
		}
	}
	if copied != 1 {
		t.Fatalf("a one-document commit copied %d shards, want 1", copied)
	}
	if after := slices.Collect(x.Codes()); len(after) != len(before) {
		t.Fatalf("Next changed the index it was made from: %d codes, had %d", len(after), len(before))
	}
	if got := y.shards[y.shardOf(5)][5]; len(got) != 1 || got[0].Chunk != 99999 {
		t.Fatalf("the changed document's codes in the next index: %v", got)
	}
}
