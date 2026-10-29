package vector

import (
	"cmp"
	"math/rand/v2"
	"testing"
)

// benchCodes is n random 1024-bit codes, keyed 0..n-1, spread over docs documents.
func benchCodes(n, docs int) map[int][]Code[int64] {
	r := rand.New(rand.NewPCG(1, 2))
	out := make(map[int][]Code[int64], docs)
	for i := range n {
		bits := make([]uint64, 16)
		for j := range bits {
			bits[j] = r.Uint64()
		}
		d := i % docs
		out[d] = append(out[d], Code[int64]{Chunk: int64(i), Bits: bits})
	}
	return out
}

func benchQuery() []uint64 {
	r := rand.New(rand.NewPCG(3, 4))
	q := make([]uint64, 16)
	for i := range q {
		q[i] = r.Uint64()
	}
	return q
}

// BenchmarkNearest orders 100k codes for one query, as a semantic search does on every query.
func BenchmarkNearest(b *testing.B) {
	x := NewIndex("m", 1, benchCodes(100_000, 20_000))
	q := benchQuery()
	b.ReportAllocs()
	for b.Loop() {
		_ = Nearest(x.Codes(), q, cmp.Compare[int64])
	}
}

// BenchmarkNearestThenTwoStage is a query's whole Hamming stage: order 100k codes, then rescore
// windows of 200 until 10 survive (every fetched item survives here, so one window).
func BenchmarkNearestThenTwoStage(b *testing.B) {
	x := NewIndex("m", 1, benchCodes(100_000, 20_000))
	q := benchQuery()
	qf := make([]float32, 1024)
	fetch := func(ids []int64) ([]Vec[int64], error) {
		out := make([]Vec[int64], len(ids))
		for i, id := range ids {
			out[i] = Vec[int64]{Item: id, F32: qf}
		}
		return out, nil
	}
	b.ReportAllocs()
	for b.Loop() {
		order := Nearest(x.Codes(), q, cmp.Compare[int64])
		if _, err := TwoStage(order, qf, 200, 10, fetch, func(a, b int64) bool { return a < b }); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkNextOneDocument publishes a commit that changed one document of 20k (a save).
func BenchmarkNextOneDocument(b *testing.B) {
	x := NewIndex("m", 1, benchCodes(100_000, 20_000))
	fresh := map[int][]Code[int64]{7: benchCodes(5, 1)[0]}
	b.ReportAllocs()
	for b.Loop() {
		_ = x.Next(2, []int{7}, fresh)
	}
}

// BenchmarkNextBatch publishes a commit that changed 64 documents of 20k (an embedding batch).
func BenchmarkNextBatch(b *testing.B) {
	x := NewIndex("m", 1, benchCodes(100_000, 20_000))
	changed := make([]int, 64)
	fresh := map[int][]Code[int64]{}
	for i := range changed {
		changed[i] = i * 311 % 20_000
		fresh[changed[i]] = benchCodes(5, 1)[0]
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = x.Next(2, changed, fresh)
	}
}

// BenchmarkNearestSeqThenTwoStageSeq is BenchmarkNearestThenTwoStage taking the order as it is
// made: only the windows TwoStage needs are ordered.
func BenchmarkNearestSeqThenTwoStageSeq(b *testing.B) {
	x := NewIndex("m", 1, benchCodes(100_000, 20_000))
	q := benchQuery()
	qf := make([]float32, 1024)
	fetch := func(ids []int64) ([]Vec[int64], error) {
		out := make([]Vec[int64], len(ids))
		for i, id := range ids {
			out[i] = Vec[int64]{Item: id, F32: qf}
		}
		return out, nil
	}
	b.ReportAllocs()
	for b.Loop() {
		order := NearestSeq(x.Codes(), q, cmp.Compare[int64])
		if _, err := TwoStageSeq(order, qf, 200, 10, fetch, func(a, b int64) bool { return a < b }); err != nil {
			b.Fatal(err)
		}
	}
}
