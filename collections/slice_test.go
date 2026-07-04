package collections

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// --- Map ---------------------------------------------------------------------

func TestMap_IntToString(t *testing.T) {
	t.Parallel()
	got := Map([]int{1, 2, 3}, strconv.Itoa)
	want := []string{"1", "2", "3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Map = %v, want %v", got, want)
	}
}

func TestMap_Empty(t *testing.T) {
	t.Parallel()
	got := Map([]int{}, func(v int) int { return v })
	if len(got) != 0 {
		t.Errorf("Map(empty) = %v, want empty", got)
	}
}

func TestMap_Nil(t *testing.T) {
	t.Parallel()
	var src []int
	got := Map(src, func(v int) int { return v })
	if len(got) != 0 {
		t.Errorf("Map(nil) = %v, want empty", got)
	}
}

func TestMapIndexed_SkipFalse(t *testing.T) {
	t.Parallel()
	// Map and filter in one pass: keep doubled evens.
	got := MapIndexed([]int{1, 2, 3, 4}, func(v, _ int) (int, bool) {
		return v * 2, v%2 == 0
	})
	want := []int{4, 8}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MapIndexed = %v, want %v", got, want)
	}
}

func TestMapIndexed_IndexAccess(t *testing.T) {
	t.Parallel()
	got := MapIndexed([]string{"a", "b"}, func(s string, idx int) (string, bool) {
		return s + strconv.Itoa(idx), true
	})
	want := []string{"a0", "b1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MapIndexed = %v, want %v", got, want)
	}
}

// --- Filter ------------------------------------------------------------------

func TestFilter_Even(t *testing.T) {
	t.Parallel()
	got := Filter([]int{1, 2, 3, 4, 5, 6}, func(v int) bool { return v%2 == 0 })
	want := []int{2, 4, 6}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Filter = %v, want %v", got, want)
	}
}

func TestFilter_None(t *testing.T) {
	t.Parallel()
	got := Filter([]int{1, 3}, func(v int) bool { return v > 10 })
	if len(got) != 0 {
		t.Errorf("Filter = %v, want empty", got)
	}
}

func TestFilter_All(t *testing.T) {
	t.Parallel()
	src := []int{1, 2, 3}
	got := Filter(src, func(int) bool { return true })
	if !reflect.DeepEqual(got, src) {
		t.Errorf("Filter = %v, want %v", got, src)
	}
}

func TestFilter_NilAndEmpty(t *testing.T) {
	t.Parallel()
	var src []int
	if got := Filter(src, func(int) bool { return true }); len(got) != 0 {
		t.Errorf("Filter(nil) = %v, want empty", got)
	}
	if got := Filter([]int{}, func(int) bool { return true }); len(got) != 0 {
		t.Errorf("Filter(empty) = %v, want empty", got)
	}
}

func TestFilterIndexed_EveryOther(t *testing.T) {
	t.Parallel()
	got := FilterIndexed([]string{"a", "b", "c", "d"}, func(_ string, idx int) bool {
		return idx%2 == 0
	})
	want := []string{"a", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FilterIndexed = %v, want %v", got, want)
	}
}

// --- Reduce ------------------------------------------------------------------

func TestReduce_Sum(t *testing.T) {
	t.Parallel()
	got := Reduce([]int{1, 2, 3, 4}, 0, func(acc, v int) int { return acc + v })
	if got != 10 {
		t.Errorf("Reduce = %d, want 10", got)
	}
}

func TestReduce_InitValue(t *testing.T) {
	t.Parallel()
	got := Reduce([]int{1, 2}, 100, func(acc, v int) int { return acc + v })
	if got != 103 {
		t.Errorf("Reduce = %d, want 103", got)
	}
}

func TestReduce_TypeConversion(t *testing.T) {
	t.Parallel()
	got := Reduce([]int{1, 2, 3}, "", func(acc string, v int) string {
		if acc == "" {
			return strconv.Itoa(v)
		}
		return acc + "-" + strconv.Itoa(v)
	})
	if got != "1-2-3" {
		t.Errorf("Reduce = %q, want 1-2-3", got)
	}
}

func TestReduce_EmptyAndNil(t *testing.T) {
	t.Parallel()
	if got := Reduce([]int{}, 7, func(acc, v int) int { return acc + v }); got != 7 {
		t.Errorf("Reduce(empty) = %d, want init 7", got)
	}
	var src []int
	if got := Reduce(src, 7, func(acc, v int) int { return acc + v }); got != 7 {
		t.Errorf("Reduce(nil) = %d, want init 7", got)
	}
}

func TestReduce_Single(t *testing.T) {
	t.Parallel()
	got := Reduce([]string{"x"}, "", func(acc, s string) string { return acc + s })
	if got != "x" {
		t.Errorf("Reduce = %q, want x", got)
	}
}

func TestReduce_Join(t *testing.T) {
	t.Parallel()
	got := Reduce([]string{"a", "b", "c"}, nil, func(acc []string, s string) []string {
		return append(acc, strings.ToUpper(s))
	})
	if !reflect.DeepEqual(got, []string{"A", "B", "C"}) {
		t.Errorf("Reduce = %v", got)
	}
}

func BenchmarkReduce_Sum(b *testing.B) {
	src := make([]int, 1024)
	for i := range src {
		src[i] = i
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = Reduce(src, 0, func(acc, v int) int { return acc + v })
	}
}
