# collections

Generic collection types and functional slice operations for Go. Zero external dependencies.

## Install

```bash
go get github.com/yongjohnlee80/golib
```

```go
import "github.com/yongjohnlee80/golib/collections"
```

## Set

An unordered collection of unique elements backed by `map[T]struct{}`.

```go
s := collections.NewSet(1, 2, 3)
s.Add(4, 5)
s.Remove(1)
s.Has(2)    // true
s.Len()     // 4
s.Values()  // []int{2, 3, 4, 5} (order not guaranteed)
```

### Set Operations

```go
a := collections.NewSet(1, 2, 3)
b := collections.NewSet(3, 4, 5)

a.Union(b)         // {1, 2, 3, 4, 5}
a.Intersect(b)     // {3}
a.Diff(b)          // {1, 2}        — in a but not b
a.SymmetricDiff(b) // {1, 2, 4, 5}  — in either but not both

a.SubsetOf(b)      // false
a.SupersetOf(b)    // false
a.Equal(b)         // false
a.Clone()          // independent copy
```

`Union` and `Intersect` iterate the smaller set for optimal performance.

## Slice Operations

Functional `Map`, `Filter`, and `Reduce` whose shapes mirror the stdlib
`slices` package conventions (element-only callbacks; `-Indexed` variants when
the position matters). Inspired by [cyc-ttn/go-collections](https://github.com/cyc-ttn/go-collections).

### Map

```go
strs := collections.Map([]int{1, 2, 3}, strconv.Itoa) // ["1" "2" "3"]

// Map + filter in one pass with the indexed variant:
doubledEvens := collections.MapIndexed([]int{1, 2, 3, 4}, func(v, _ int) (int, bool) {
    return v * 2, v%2 == 0
}) // [4 8]
```

### Filter

```go
evens := collections.Filter([]int{1, 2, 3, 4, 5, 6}, func(v int) bool {
    return v%2 == 0
}) // [2 4 6]

everyOther := collections.FilterIndexed([]string{"a", "b", "c", "d"}, func(_ string, idx int) bool {
    return idx%2 == 0
}) // ["a" "c"]
```

### Reduce

```go
sum := collections.Reduce([]int{1, 2, 3, 4, 5}, 0, func(acc, v int) int {
    return acc + v
}) // 15
```

### Set iteration

```go
for v := range set.All() { // iter.Seq[T]; range-over-func
    ...
}
```

## Acknowledgements

The `Map`, `Filter`, and `Reduce` function signatures were inspired by [cyc-ttn/go-collections](https://github.com/cyc-ttn/go-collections).

## License

[MIT](../LICENSE)
