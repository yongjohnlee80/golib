// Package vector provides numerical operations and data structures for high-performance
// vector similarity search over normalized floating-point embeddings.
//
// # Core Mathematical Operations
//
//   - Dot Product: Fast cosine similarity over unit-normalized vectors.
//   - 1-Bit Sign Quantization: [SignBits] compresses float32 vectors into 1-bit sign code words
//     (positive coordinates map to 1, non-positive to 0), enabling fast coarse retrieval
//     via bitwise XOR and POPCNT Hamming distance.
//   - Two-Stage Scanning: [TwoStage] implements a hierarchical candidate search:
//     1. Coarse Filter: Scans the compact 1-bit sign code index to find the nearest N candidates.
//     2. Fine Rescore: Re-scores top candidates using full float32 dot products for exact ranking.
//   - Concurrent Snapshot Index: [Index] provides an immutable, thread-safe in-memory vector
//     index bound to model fingerprints and commit sequence watermarks ([Index.Usable]).
//
// The package imports no other search modules and has zero external dependencies.
package vector
