package mermaid_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

// A share is a percentage rounded once here, to one decimal, for every renderer.
func TestPercentIsOneRounding(t *testing.T) {
	for share, want := range map[float64]float64{0: 0, 0.42: 42, 0.125: 12.5, 1: 100, 1.0 / 3: 33.3, 0.0004: 0, 0.9996: 100} {
		if got := mermaid.Percent(share); got != want {
			t.Errorf("Percent(%v) = %v, want %v", share, got, want)
		}
	}
}
