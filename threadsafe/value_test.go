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

func TestValue_LockUnlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    Value[int]
	}{
		{"Synchronized", NewSynchronizedValue(42)},
		{"MultiRead", NewMultiReadSyncValue(42)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.v.Lock()
			if got != 42 {
				t.Errorf("Lock() = %d, want 42", got)
			}
			tt.v.Unlock()
		})
	}
}

func TestValue_InterfaceSatisfaction(t *testing.T) {
	// Compile-time checks that both types satisfy the Value interface.
	var _ Value[int] = NewSynchronizedValue(0)
	var _ Value[int] = NewMultiReadSyncValue(0)
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
