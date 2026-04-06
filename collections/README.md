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

Functional `Map`, `Filter`, and `Reduce` inspired by [cyc-ttn/go-collections](https://github.com/cyc-ttn/go-collections).

All callbacks receive:
- **`agg`** — accumulated results so far (enables patterns like deduplication)
- **`idx`** — index of the current element in the source slice

### Map

Transform `[]S` into `[]T`. Return `(value, true)` to include, `(_, false)` to skip.

```go
strs := collections.Map([]int{1, 2, 3}, func(_ []string, v int, _ int) (string, bool) {
    return strconv.Itoa(v), true
})
// ["1", "2", "3"]

// Skip odd values
doubled := collections.Map([]int{1, 2, 3, 4}, func(_ []int, v int, _ int) (int, bool) {
    return v * 2, v%2 == 0
})
// [4, 8]
```

### Filter

Keep elements where the callback returns `true`.

```go
evens := collections.Filter([]int{1, 2, 3, 4, 5, 6}, func(_ []int, v int, _ int) bool {
    return v%2 == 0
})
// [2, 4, 6]

// Deduplicate using agg
unique := collections.Filter([]string{"a", "b", "a", "c"}, func(agg []string, s string, _ int) bool {
    for _, a := range agg {
        if a == s {
            return false
        }
    }
    return true
})
// ["a", "b", "c"]
```

### Reduce

Fold `[]S` into a single value of type `T`. The initial accumulator is the zero value of `T`.

```go
sum := collections.Reduce([]int{1, 2, 3, 4, 5}, func(_ []int, acc int, v int, _ int) int {
    return acc + v
})
// 15

csv := collections.Reduce([]int{1, 2, 3}, func(_ []string, acc string, v int, _ int) string {
    if acc != "" {
        acc += ","
    }
    return acc + strconv.Itoa(v)
})
// "1,2,3"
```

## Acknowledgements

The `Map`, `Filter`, and `Reduce` function signatures were inspired by [cyc-ttn/go-collections](https://github.com/cyc-ttn/go-collections).

## License

[MIT](../LICENSE)
