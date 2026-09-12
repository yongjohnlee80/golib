package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// Tests for layout and constraint geometry helpers (boundedMax and subFrame).

func TestLayoutUtil_BoundedMax(t *testing.T) {
	tests := []struct {
		name string
		maxV int
		minV int
		want int
	}{
		{
			name: "unbounded max returns min",
			maxV: tui.Unbounded,
			minV: 42,
			want: 42,
		},
		{
			name: "bounded max returns max when greater than min",
			maxV: 80,
			minV: 20,
			want: 80,
		},
		{
			name: "bounded max returns max when equal to min",
			maxV: 50,
			minV: 50,
			want: 50,
		},
		{
			name: "bounded max returns max when smaller than min",
			maxV: 10,
			minV: 30,
			want: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := boundedMax(tt.maxV, tt.minV)
			if got != tt.want {
				t.Errorf("boundedMax(%d, %d) = %d, want %d", tt.maxV, tt.minV, got, tt.want)
			}
		})
	}
}

func TestLayoutUtil_SubFrame(t *testing.T) {
	tests := []struct {
		name  string
		maxV  int
		frame int
		want  int
	}{
		{
			name:  "unbounded max preserves unbounded",
			maxV:  tui.Unbounded,
			frame: 4,
			want:  tui.Unbounded,
		},
		{
			name:  "bounded max subtracts frame",
			maxV:  100,
			frame: 4,
			want:  96,
		},
		{
			name:  "bounded max exact match returns zero",
			maxV:  4,
			frame: 4,
			want:  0,
		},
		{
			name:  "bounded max underflow clamps at zero",
			maxV:  2,
			frame: 6,
			want:  0,
		},
		{
			name:  "zero max clamps at zero",
			maxV:  0,
			frame: 4,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := subFrame(tt.maxV, tt.frame)
			if got != tt.want {
				t.Errorf("subFrame(%d, %d) = %d, want %d", tt.maxV, tt.frame, got, tt.want)
			}
		})
	}
}
