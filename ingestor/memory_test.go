package ingestor

import (
	"sync"
	"testing"
)

func TestMemoryLoader_Commit(t *testing.T) {
	t.Parallel()

	ml := NewMemoryLoader[int]("test")

	if err := ml.Commit(1, 2, 3); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if got := ml.Len(); got != 3 {
		t.Errorf("Len() = %d, want 3", got)
	}
	if got := ml.Total(); got != 3 {
		t.Errorf("Total() = %d, want 3", got)
	}
}

func TestMemoryLoader_Flush(t *testing.T) {
	t.Parallel()

	ml := NewMemoryLoader[string]("test")
	_ = ml.Commit("a", "b", "c")

	items, err := ml.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if len(items) != 3 {
		t.Errorf("Flush returned %d items, want 3", len(items))
	}
	if ml.Len() != 0 {
		t.Errorf("Len after Flush = %d, want 0", ml.Len())
	}
	if ml.Total() != 3 {
		t.Errorf("Total after Flush = %d, want 3", ml.Total())
	}
}

func TestMemoryLoader_Shift(t *testing.T) {
	t.Parallel()

	ml := NewMemoryLoader[int]("test")
	_ = ml.Commit(1, 2, 3, 4, 5)

	shifted := ml.Shift(3)
	if len(shifted) != 3 {
		t.Fatalf("Shift(3) returned %d items, want 3", len(shifted))
	}
	if shifted[0] != 1 || shifted[1] != 2 || shifted[2] != 3 {
		t.Errorf("Shift(3) = %v, want [1 2 3]", shifted)
	}
	if ml.Len() != 2 {
		t.Errorf("Len after Shift = %d, want 2", ml.Len())
	}
}

func TestMemoryLoader_ShiftMoreThanLen(t *testing.T) {
	t.Parallel()

	ml := NewMemoryLoader[int]("test")
	_ = ml.Commit(1, 2)

	shifted := ml.Shift(10)
	if len(shifted) != 2 {
		t.Errorf("Shift(10) returned %d items, want 2", len(shifted))
	}
	if ml.Len() != 0 {
		t.Errorf("Len after over-Shift = %d, want 0", ml.Len())
	}
}

func TestMemoryLoader_ShiftZero(t *testing.T) {
	t.Parallel()

	ml := NewMemoryLoader[int]("test")
	_ = ml.Commit(1, 2)

	shifted := ml.Shift(0)
	if shifted != nil {
		t.Errorf("Shift(0) = %v, want nil", shifted)
	}
}

func TestMemoryLoader_Description(t *testing.T) {
	t.Parallel()

	ml := NewMemoryLoader[int]("initial")
	if ml.Description() != "initial" {
		t.Errorf("Description() = %q, want %q", ml.Description(), "initial")
	}

	ml.SetDescription("updated")
	if ml.Description() != "updated" {
		t.Errorf("Description() = %q, want %q", ml.Description(), "updated")
	}
}

func TestMemoryLoader_ConcurrentCommit(t *testing.T) {
	t.Parallel()

	ml := NewMemoryLoader[int]("concurrent")
	const goroutines = 100
	const itemsPerGoroutine = 100

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < itemsPerGoroutine; j++ {
				_ = ml.Commit(j)
			}
		}()
	}
	wg.Wait()

	expected := uint64(goroutines * itemsPerGoroutine)
	if got := ml.Total(); got != expected {
		t.Errorf("Total() = %d, want %d", got, expected)
	}
	if got := ml.Len(); got != expected {
		t.Errorf("Len() = %d, want %d", got, expected)
	}
}

func TestMemoryLoader_InterfaceSatisfaction(t *testing.T) {
	var _ Ingestor[int] = NewMemoryLoader[int]("")
}
