package threadsafe

import (
	"sync"
	"testing"
)

func TestSynchronizedValue_Update(t *testing.T) {
	t.Parallel()

	v := NewSynchronizedValue(0)
	count := 10_000

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v.Update(func(i int) int { return i + 1 })
		}()
	}
	wg.Wait()

	if got := v.Get(); got != count {
		t.Errorf("SynchronizedValue: got %d, want %d", got, count)
	}
}

func TestMultiReadSyncValue_Update(t *testing.T) {
	t.Parallel()

	v := NewMultiReadSyncValue(0)
	count := 10_000

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v.Update(func(i int) int { return i + 1 })
		}()
	}
	wg.Wait()

	if got := v.Get(); got != count {
		t.Errorf("MultiReadSyncValue: got %d, want %d", got, count)
	}
}

func TestValue_SetAndGet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    Value[string]
	}{
		{"Synchronized", NewSynchronizedValue("")},
		{"MultiRead", NewMultiReadSyncValue("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.v.Set("hello")
			if got := tt.v.Get(); got != "hello" {
				t.Errorf("Get() = %q, want %q", got, "hello")
			}
		})
	}
}

func TestValue_DoAndRDo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    Value[map[string]int]
	}{
		{"Synchronized", NewSynchronizedValue(map[string]int{"a": 1})},
		{"MultiRead", NewMultiReadSyncValue(map[string]int{"a": 1})},
		{"Atomic", NewAtomicValue(map[string]int{"a": 1})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got int
			tt.v.RDo(func(m map[string]int) { got = m["a"] })
			if got != 1 {
				t.Errorf("RDo read = %d, want 1", got)
			}
			tt.v.Do(func(m *map[string]int) { (*m)["b"] = 2 })
			tt.v.RDo(func(m map[string]int) { got = m["b"] })
			if got != 2 {
				t.Errorf("mutation through Do not visible, got %d", got)
			}
		})
	}
}

func TestAtomicValue_UpdateCAS(t *testing.T) {
	t.Parallel()
	a := NewAtomicValue(0)
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() { a.Update(func(v int) int { return v + 1 }) })
	}
	wg.Wait()
	if got := a.Get(); got != 100 {
		t.Errorf("Get() = %d, want 100 (lost CAS updates)", got)
	}
}

func TestAtomicValue_DoCopies(t *testing.T) {
	t.Parallel()
	a := NewAtomicValue([]int{1})
	before := a.Get()
	a.Do(func(s *[]int) { *s = append(*s, 2) })
	if len(before) != 1 {
		t.Error("Do must mutate a copy, not the shared snapshot")
	}
	if got := a.Get(); len(got) != 2 || got[1] != 2 {
		t.Errorf("Do result not installed: %v", got)
	}
}

func TestValue_InterfaceSatisfaction(t *testing.T) {
	// Compile-time checks that all implementations satisfy the Value interface.
	var _ Value[int] = NewSynchronizedValue(0)
	var _ Value[int] = NewMultiReadSyncValue(0)
	var _ Value[int] = NewAtomicValue(0)
}

func TestConcurrentReadWrite(t *testing.T) {
	t.Parallel()

	v := NewMultiReadSyncValue(0)
	const readers = 100
	const writers = 10
	const writes = 1000

	var wg sync.WaitGroup

	// Concurrent readers
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writes; j++ {
				_ = v.Get()
			}
		}()
	}

	// Concurrent writers
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writes; j++ {
				v.Update(func(i int) int { return i + 1 })
			}
		}()
	}

	wg.Wait()

	expected := writers * writes
	if got := v.Get(); got != expected {
		t.Errorf("ConcurrentReadWrite: got %d, want %d", got, expected)
	}
}
