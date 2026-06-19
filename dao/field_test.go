package dao

import "testing"

func TestField_WriteCol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		f    Field[any]
		want string
	}{
		{"qualified column", Field[any]{Column: "artist.name"}, "name"},
		{"bare column", Field[any]{Column: "name"}, "name"},
		{"explicit override", Field[any]{Column: "artist.name", WriteColumn: "display_name"}, "display_name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.f.writeCol(); got != tt.want {
				t.Errorf("writeCol() = %q, want %q", got, tt.want)
			}
		})
	}
}
