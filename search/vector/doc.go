// Package vector is the math of semantic search over normalized float vectors: dot products, 1-bit
// sign codes and their Hamming distance, byte encodings, an immutable per-model code index with the
// watermark that tells a query whether it may use it, and the two-stage scan that ranks candidates
// by code and rescores them by vector.
//
//	idx := vector.NewIndex(model, commitSeq, codes)         // publish; Next after each commit
//	if idx.Usable(model, seqReadByTheQuery) {                // else scan the stored codes
//		order := vector.Nearest(idx.Codes(), vector.SignBits(q), cmp.Compare[int64])
//		best, err := vector.TwoStage(order, q, 200, 50, fetchValid, lessByPath)
//	}
//
// It imports no other search package.
package vector
