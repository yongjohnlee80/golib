package embed

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// picky rejects any batch holding a text with "bad", fails on "down", and waits on "slow" until
// its context ends. Its vectors are (len(text), 0, ...), of dims dimensions.
type picky struct {
	dims  int
	calls atomic.Int32
}

func (p *picky) Name() string { return "picky" }
func (p *picky) Model() Model { return Model{Provider: "picky", Name: "m", Dims: p.dims} }

func (p *picky) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	p.calls.Add(1)
	for _, t := range texts {
		switch {
		case strings.Contains(t, "slow"):
			<-ctx.Done()
			return nil, ctx.Err()
		case strings.Contains(t, "down"):
			return nil, errors.New("the provider is down")
		case strings.Contains(t, "bad"):
			return nil, fmt.Errorf("%w: too long", ErrRejected)
		}
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = make([]float32, p.dims)
		out[i][0] = float32(len(t))
	}
	return out, nil
}

func TestBisect(t *testing.T) {
	ctx := context.Background()
	p := &picky{dims: 3}
	texts := []string{"a", "bad1", "ccc", "dddd", "bad2", "ee", "f", "g"}
	vecs, rejected, err := Bisect(ctx, p, texts, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	var idx []int
	for _, r := range rejected {
		if !errors.Is(r.Err, ErrRejected) {
			t.Errorf("rejection %d: %v", r.Index, r.Err)
		}
		idx = append(idx, r.Index)
	}
	if !reflect.DeepEqual(idx, []int{1, 4}) {
		t.Errorf("rejected %v, want [1 4]", idx)
	}
	for i, v := range vecs {
		rejectedHere := i == 1 || i == 4
		if rejectedHere != (v == nil) {
			t.Errorf("text %d: vector %v", i, v)
		}
		if v != nil && (len(v) != 3 || math.Abs(float64(v[0])-1) > 1e-6) {
			t.Errorf("text %d: not normalized: %v", i, v)
		}
	}
	// [0,8) rejected; [0,4) rejected: [0,2) rejected ([0,1) ok, [1,2) refused), [2,4) ok; [4,8)
	// rejected: [4,6) rejected ([4,5) refused, [5,6) ok), [6,8) ok
	if p.calls.Load() != 11 {
		t.Errorf("%d calls", p.calls.Load())
	}

	if vecs, rej, err := Bisect(ctx, &picky{dims: 3}, []string{"a", "b"}, 3, 0); err != nil || len(rej) != 0 || len(vecs) != 2 {
		t.Errorf("a clean batch: %v %v %v", vecs, rej, err)
	}
	if vecs, rej, err := Bisect(ctx, p, nil, 3, 0); err != nil || len(vecs) != 0 || rej != nil {
		t.Errorf("no texts: %v %v %v", vecs, rej, err)
	}
	if _, _, err := Bisect(ctx, &picky{dims: 3}, []string{"a", "bad", "down"}, 3, 0); err == nil || errors.Is(err, ErrRejected) {
		t.Errorf("another failure fails the batch: %v", err)
	}
	if _, _, err := Bisect(ctx, &picky{dims: 4}, []string{"a"}, 3, 0); !errors.Is(err, ErrDims) {
		t.Errorf("the wrong size: %v", err)
	}
	start := time.Now()
	if _, _, err := Bisect(ctx, &picky{dims: 3}, []string{"slow"}, 3, 20*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Errorf("perCall bounds the call: %v after %v", err, time.Since(start))
	}
}
