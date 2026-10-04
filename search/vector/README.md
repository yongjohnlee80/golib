# search/vector — the math of semantic search

This package holds the math of semantic search over normalized float vectors:
- `Dot`, `Normalize`;
- 1-bit sign codes (`SignBits`) compared by `Hamming` distance;
- little-endian byte encodings for storing both.

Above those sits a per-model code index, built for a store that scans codes in memory and rescores
by vector.

## Install

```go
import "github.com/yongjohnlee80/golib/search/vector"
```

## The index and its watermark

An `Index` is the codes of one model at one committed state (its watermark), never changed once
made. After each commit, `Next` makes the next index, sharing the documents that did not change. A
commit that changes no code still moves the watermark. A query uses the index only when
`Usable(model, watermark)` holds for the model and watermark it read in its own snapshot. An older
index lacks chunks that became ready since. A newer one lacks chunks still alive in the query's
snapshot. In either case, it scans the stored codes in that snapshot instead.

```go
next := current.Next(commitSeq, changedDocs, freshCodes) // fresh and commitSeq read in one snapshot
published.Store(next)

if idx := published.Load(); idx.Usable(model, seqInThisTx) {
	order := vector.Nearest(idx.Codes(), vector.SignBits(q), cmp.Compare[int64])
	best, err := vector.TwoStage(order, q, 200, 50, fetchValidWithVectors, lessByPathThenOrd)
}
```

`Nearest` orders codes by distance, with ties broken by the caller's comparator, so a chunk key needs
no natural order. `TwoStage` hands its caller windows of that order, keeps the items still valid in
the caller's snapshot, and rescores them by dot product. It goes on until it has enough, so stale
codes cannot crowd out valid ones. A fetch returns `SkipRest` when no later window can hold a valid
item.

## License

See [LICENSE](../../LICENSE).
