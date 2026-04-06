package collections

import (
	"fmt"
	"strconv"
	"testing"
)

// ---------------------------------------------------------------------------
// Map
// ---------------------------------------------------------------------------

func TestMap_IntToString(t *testing.T) {
	t.Parallel()

	src := []int{1, 2, 3}
	got := Map(src, func(_ []string, v int, _ int) (string, bool) {
		return strconv.Itoa(v), true
	})

	want := []string{"1", "2", "3"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMap_SkipFalse(t *testing.T) {
	t.Parallel()

	src := []int{1, 2, 3, 4, 5}
	got := Map(src, func(_ []int, v int, _ int) (int, bool) {
		return v * 2, v%2 == 0
	})

	want := []int{4, 8}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestMap_AggAccess(t *testing.T) {
	t.Parallel()

	src := []int{10, 20, 30}
	got := Map(src, func(agg []string, v int, _ int) (string, bool) {
		return fmt.Sprintf("len=%d,val=%d", len(agg), v), true
	})

	want := []string{"len=0,val=10", "len=1,val=20", "len=2,val=30"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMap_IndexAccess(t *testing.T) {
	t.Parallel()

	src := []string{"a", "b", "c"}
	got := Map(src, func(_ []string, v string, idx int) (string, bool) {
		return fmt.Sprintf("%d:%s", idx, v), true
	})

	want := []string{"0:a", "1:b", "2:c"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMap_Empty(t *testing.T) {
	t.Parallel()

	got := Map([]int{}, func(_ []int, v int, _ int) (int, bool) {
		return v, true
	})
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestMap_Nil(t *testing.T) {
	t.Parallel()

	got := Map(nil, func(_ []int, v int, _ int) (int, bool) {
		return v, true
	})
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// Filter
// ---------------------------------------------------------------------------

func TestFilter_Even(t *testing.T) {
	t.Parallel()

	src := []int{1, 2, 3, 4, 5, 6}
	got := Filter(src, func(_ []int, v int, _ int) bool {
		return v%2 == 0
	})

	want := []int{2, 4, 6}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestFilter_AggDedup(t *testing.T) {
	t.Parallel()

	src := []string{"a", "b", "a", "c", "b"}
	got := Filter(src, func(agg []string, s string, _ int) bool {
		for _, a := range agg {
			if a == s {
				return false
			}
		}
		return true
	})

	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFilter_IndexAccess(t *testing.T) {
	t.Parallel()

	// Keep only elements at even indices.
	src := []string{"a", "b", "c", "d", "e"}
	got := Filter(src, func(_ []string, _ string, idx int) bool {
		return idx%2 == 0
	})

	want := []string{"a", "c", "e"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFilter_None(t *testing.T) {
	t.Parallel()

	got := Filter([]int{1, 2, 3}, func(_ []int, _ int, _ int) bool {
		return false
	})
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestFilter_All(t *testing.T) {
	t.Parallel()

	src := []int{1, 2, 3}
	got := Filter(src, func(_ []int, _ int, _ int) bool {
		return true
	})
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
}

func TestFilter_Empty(t *testing.T) {
	t.Parallel()

	got := Filter([]int{}, func(_ []int, v int, _ int) bool {
		return true
	})
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestFilter_Nil(t *testing.T) {
	t.Parallel()

	got := Filter(nil, func(_ []int, v int, _ int) bool {
		return true
	})
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// Reduce
// ---------------------------------------------------------------------------

func TestReduce_Sum(t *testing.T) {
	t.Parallel()

	src := []int{1, 2, 3, 4, 5}
	got := Reduce(src, func(_ []int, acc int, v int, _ int) int {
		return acc + v
	})
	if got != 15 {
		t.Errorf("sum = %d, want 15", got)
	}
}

func TestReduce_TypeConversion(t *testing.T) {
	t.Parallel()

	src := []int{1, 2, 3}
	got := Reduce(src, func(_ []string, acc string, v int, _ int) string {
		if acc != "" {
			acc += ","
		}
		return acc + strconv.Itoa(v)
	})
	if got != "1,2,3" {
		t.Errorf("got %q, want %q", got, "1,2,3")
	}
}

func TestReduce_IndexAccess(t *testing.T) {
	t.Parallel()

	src := []string{"a", "b", "c"}
	got := Reduce(src, func(_ []string, acc string, v string, idx int) string {
		return acc + fmt.Sprintf("%d:%s ", idx, v)
	})
	if got != "0:a 1:b 2:c " {
		t.Errorf("got %q, want %q", got, "0:a 1:b 2:c ")
	}
}

func TestReduce_AggAccess(t *testing.T) {
	t.Parallel()

	src := []int{2, 3, 4}
	got := Reduce(src, func(agg []int, acc int, v int, idx int) int {
		if idx == 0 {
			return v
		}
		return acc * v
	})
	if got != 24 {
		t.Errorf("product = %d, want 24", got)
	}
}

func TestReduce_Empty(t *testing.T) {
	t.Parallel()

	got := Reduce([]int{}, func(_ []int, acc int, v int, _ int) int {
		return acc + v
	})
	if got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestReduce_Nil(t *testing.T) {
	t.Parallel()

	got := Reduce(nil, func(_ []int, acc int, v int, _ int) int {
		return acc + v
	})
	if got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestReduce_Single(t *testing.T) {
	t.Parallel()

	got := Reduce([]int{42}, func(_ []int, acc int, v int, _ int) int {
		return acc + v
	})
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}
