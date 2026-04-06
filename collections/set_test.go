package collections

import (
	"sort"
	"testing"
)

func TestNewSet(t *testing.T) {
	t.Parallel()

	s := NewSet(1, 2, 3, 2, 1)
	if s.Len() != 3 {
		t.Errorf("Len() = %d, want 3", s.Len())
	}
	for _, v := range []int{1, 2, 3} {
		if !s.Has(v) {
			t.Errorf("Has(%d) = false, want true", v)
		}
	}
}

func TestNewSet_Empty(t *testing.T) {
	t.Parallel()

	s := NewSet[string]()
	if s.Len() != 0 {
		t.Errorf("Len() = %d, want 0", s.Len())
	}
}

func TestSet_AddRemove(t *testing.T) {
	t.Parallel()

	s := NewSet[int]()
	s.Add(1, 2, 3)
	if s.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", s.Len())
	}

	s.Remove(2)
	if s.Has(2) {
		t.Error("Has(2) after Remove = true")
	}
	if s.Len() != 2 {
		t.Errorf("Len() = %d, want 2", s.Len())
	}

	// Remove nonexistent — no panic
	s.Remove(99)
	if s.Len() != 2 {
		t.Errorf("Len() = %d, want 2", s.Len())
	}
}

func TestSet_Values(t *testing.T) {
	t.Parallel()

	s := NewSet(3, 1, 2)
	vals := s.Values()
	sort.Ints(vals)
	if len(vals) != 3 || vals[0] != 1 || vals[1] != 2 || vals[2] != 3 {
		t.Errorf("Values() = %v, want [1 2 3]", vals)
	}
}

func TestSet_Clone(t *testing.T) {
	t.Parallel()

	s := NewSet(1, 2, 3)
	c := s.Clone()

	c.Add(4)
	if s.Has(4) {
		t.Error("Clone mutation leaked to original")
	}
	if !c.Has(4) {
		t.Error("Clone missing added element")
	}
}

func TestSet_Union(t *testing.T) {
	t.Parallel()

	a := NewSet(1, 2, 3)
	b := NewSet(3, 4, 5)
	u := a.Union(b)

	if u.Len() != 5 {
		t.Errorf("Union Len() = %d, want 5", u.Len())
	}
	for _, v := range []int{1, 2, 3, 4, 5} {
		if !u.Has(v) {
			t.Errorf("Union missing %d", v)
		}
	}
	// Originals unchanged
	if a.Len() != 3 || b.Len() != 3 {
		t.Error("Union mutated an input set")
	}
}

func TestSet_Intersect(t *testing.T) {
	t.Parallel()

	a := NewSet(1, 2, 3, 4)
	b := NewSet(3, 4, 5, 6)
	i := a.Intersect(b)

	if i.Len() != 2 {
		t.Errorf("Intersect Len() = %d, want 2", i.Len())
	}
	if !i.Has(3) || !i.Has(4) {
		t.Errorf("Intersect = %v, want {3, 4}", i.Values())
	}
}

func TestSet_Intersect_Disjoint(t *testing.T) {
	t.Parallel()

	a := NewSet(1, 2)
	b := NewSet(3, 4)
	if a.Intersect(b).Len() != 0 {
		t.Error("Intersect of disjoint sets should be empty")
	}
}

func TestSet_Diff(t *testing.T) {
	t.Parallel()

	a := NewSet(1, 2, 3, 4)
	b := NewSet(3, 4, 5)
	d := a.Diff(b)

	if d.Len() != 2 {
		t.Errorf("Diff Len() = %d, want 2", d.Len())
	}
	if !d.Has(1) || !d.Has(2) {
		t.Errorf("Diff = %v, want {1, 2}", d.Values())
	}
}

func TestSet_SymmetricDiff(t *testing.T) {
	t.Parallel()

	a := NewSet(1, 2, 3)
	b := NewSet(2, 3, 4)
	sd := a.SymmetricDiff(b)

	if sd.Len() != 2 {
		t.Errorf("SymmetricDiff Len() = %d, want 2", sd.Len())
	}
	if !sd.Has(1) || !sd.Has(4) {
		t.Errorf("SymmetricDiff = %v, want {1, 4}", sd.Values())
	}
}

func TestSet_SubsetOf(t *testing.T) {
	t.Parallel()

	a := NewSet(1, 2)
	b := NewSet(1, 2, 3, 4)

	if !a.SubsetOf(b) {
		t.Error("a should be subset of b")
	}
	if b.SubsetOf(a) {
		t.Error("b should not be subset of a")
	}

	// Equal sets are subsets of each other
	c := NewSet(1, 2)
	if !a.SubsetOf(c) {
		t.Error("equal sets should be subsets")
	}
}

func TestSet_SupersetOf(t *testing.T) {
	t.Parallel()

	a := NewSet(1, 2, 3, 4)
	b := NewSet(1, 2)

	if !a.SupersetOf(b) {
		t.Error("a should be superset of b")
	}
	if b.SupersetOf(a) {
		t.Error("b should not be superset of a")
	}
}

func TestSet_Equal(t *testing.T) {
	t.Parallel()

	a := NewSet(1, 2, 3)
	b := NewSet(3, 2, 1)
	c := NewSet(1, 2, 4)

	if !a.Equal(b) {
		t.Error("a and b should be equal")
	}
	if a.Equal(c) {
		t.Error("a and c should not be equal")
	}
	if a.Equal(NewSet(1, 2)) {
		t.Error("different sizes should not be equal")
	}
}

func TestSet_NilSafety(t *testing.T) {
	t.Parallel()

	var s Set[int]
	if s.Has(1) {
		t.Error("nil set Has should return false")
	}
	if s.Len() != 0 {
		t.Error("nil set Len should return 0")
	}
	if len(s.Values()) != 0 {
		t.Error("nil set Values should return empty")
	}
}

func BenchmarkSet_Add(b *testing.B) {
	s := NewSet[int]()
	for i := 0; i < b.N; i++ {
		s.Add(i)
	}
}

func BenchmarkSet_Has(b *testing.B) {
	s := NewSet[int]()
	for i := 0; i < 10_000; i++ {
		s.Add(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Has(i % 10_000)
	}
}

func BenchmarkSet_Intersect(b *testing.B) {
	a := make(Set[int], 1000)
	c := make(Set[int], 1000)
	for i := 0; i < 1000; i++ {
		a.Add(i)
		c.Add(i + 500)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Intersect(c)
	}
}
