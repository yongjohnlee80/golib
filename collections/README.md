# collections

Generic collection types and functional slice operations. Zero dependencies.
The slice helpers mirror the stdlib `slices` conventions (element-only
callbacks, with `-Indexed` variants when position matters).

```bash
go get github.com/yongjohnlee80/golib/collections
```

```go
import "github.com/yongjohnlee80/golib/collections"
```

## Set[T]

`Set[T]` is an unordered collection of unique `comparable` elements, backed by a
`map[T]struct{}`.

```go
s := collections.NewSet(1, 2, 3)
s.Add(4, 5)
s.Remove(1)
s.Has(2)     // true
s.Len()      // 4
s.Values()   // []int{...} in unspecified order
```

### Set algebra

`Union`, `Intersect`, `Diff`, `SymmetricDiff`, and `Clone` all return **new**
sets and never mutate their operands (`Add`/`Remove` are the only mutators).
`Union`/`Intersect` iterate the smaller set for efficiency.

```go
a := collections.NewSet(1, 2, 3)
b := collections.NewSet(2, 3, 4)

a.Union(b)          // {1,2,3,4}
a.Intersect(b)      // {2,3}
a.Diff(b)           // {1}      — in a, not in b
a.SymmetricDiff(b)  // {1,4}    — in exactly one
a.Clone()           // independent copy
```

### Comparisons

```go
a.SubsetOf(b)    // every element of a is in b
a.SupersetOf(b)  // a contains every element of b
a.Equal(b)       // same elements (same length + subset)
```

### Iteration

`All()` returns an `iter.Seq[T]` for range-over-func:

```go
for v := range s.All() {
    fmt.Println(v)
}
```

## Functional slice operations

Non-mutating; each returns a new slice pre-sized to the source.

```go
// Map — transform every element.
names := collections.Map(users, func(u User) string { return u.Name })

// MapIndexed — map and filter in one pass; the bool includes/excludes.
evensDoubled := collections.MapIndexed([]int{1, 2, 3, 4}, func(v, _ int) (int, bool) {
    return v * 2, v%2 == 0
}) // [4, 8]

// Filter — keep elements where the predicate is true.
active := collections.Filter(users, func(u User) bool { return u.Active })

// FilterIndexed — filter with access to the index.
everyOther := collections.FilterIndexed(xs, func(_ T, i int) bool { return i%2 == 0 })

// Reduce — left fold from an initial accumulator.
total := collections.Reduce(orders, 0.0, func(sum float64, o Order) float64 {
    return sum + o.Amount
})
```

## Gotchas

- `Values()` and `All()` yield elements in unspecified order (map iteration).
- A `nil` `Set[T]` panics on `Add`/`Remove` (assignment to a nil map). Always
  construct with `NewSet` (or `Clone`/`Union`/… which allocate). Read-only ops
  (`Has`, `Len`, `Values`, `SubsetOf`) tolerate a nil set.
- `Set` is not safe for concurrent mutation; guard it with
  [`threadsafe`](../threadsafe) if shared across goroutines.

## Acknowledgements

The functional slice operations were inspired by
[cyc-ttn/go-collections](https://github.com/cyc-ttn/go-collections), reshaped
to the stdlib `slices` callback conventions.

## License

See [LICENSE](../LICENSE).