package mermaid

import (
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// A percentage is written with its decimal only when it has one, then %, from the model's rounding.
func TestPercentText(t *testing.T) {
	for share, want := range map[float64]string{0: "0%", 0.42: "42%", 0.125: "12.5%", 1: "100%", 1.0 / 3: "33.3%"} {
		if got := PercentText(pm.Percent(share)); got != want {
			t.Errorf("PercentText(Percent(%v)) = %q, want %q", share, got, want)
		}
	}
}
