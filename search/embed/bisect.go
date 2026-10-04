package embed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yongjohnlee80/golib/search/vector"
)

// Rejection is one text a provider refused by itself: its index in the batch, and the provider's
// error (an ErrRejected).
type Rejection struct {
	Index int
	Err   error
}

// Bisect embeds texts with p, in one call when the provider takes them. A provider rejecting the
// input (ErrRejected), not failing as a whole, is narrowed down by halves to the single texts it
// rejects: they are returned as rejections, with nil vectors, and every other text is embedded. Any
// other failure fails the whole batch. Each vector is checked to have dims dimensions and is
// normalized. perCall, when positive, bounds each call to the provider.
func Bisect(ctx context.Context, p Provider, texts []string, dims int, perCall time.Duration) ([][]float32, []Rejection, error) {
	vecs := make([][]float32, len(texts))
	var rejected []Rejection
	var embed func(lo, hi int) error
	embed = func(lo, hi int) error {
		cctx, cancel := ctx, context.CancelFunc(func() {})
		if perCall > 0 {
			cctx, cancel = context.WithTimeout(ctx, perCall)
		}
		got, err := p.Embed(cctx, texts[lo:hi])
		cancel()
		switch {
		case errors.Is(err, ErrRejected) && hi-lo == 1:
			rejected = append(rejected, Rejection{Index: lo, Err: err})
			return nil
		case errors.Is(err, ErrRejected):
			mid := lo + (hi-lo)/2
			if err := embed(lo, mid); err != nil {
				return err
			}
			return embed(mid, hi)
		case err != nil:
			return err
		case len(got) != hi-lo:
			return fmt.Errorf("%w: %d vectors for %d texts", ErrDims, len(got), hi-lo)
		}
		for i, v := range got {
			if len(v) != dims {
				return fmt.Errorf("%w: %d dimensions, the model has %d", ErrDims, len(v), dims)
			}
			vecs[lo+i] = vector.Normalize(v)
		}
		return nil
	}
	if len(texts) == 0 {
		return vecs, nil, nil
	}
	if err := embed(0, len(texts)); err != nil {
		return nil, nil, err
	}
	return vecs, rejected, nil
}
