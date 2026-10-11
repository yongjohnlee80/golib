package gui

import "testing"

func TestSwapIntervalZeroOnWaylandKeepsTheUsersValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want map[string]string
	}{
		{"wayland", map[string]string{"WAYLAND_DISPLAY": "wayland-1"},
			map[string]string{"WAYLAND_DISPLAY": "wayland-1", "__GL_SYNC_TO_VBLANK": "0", "vblank_mode": "0"}},
		{"the user's values stay", map[string]string{"WAYLAND_DISPLAY": "wayland-1", "__GL_SYNC_TO_VBLANK": "1", "vblank_mode": "3"},
			map[string]string{"WAYLAND_DISPLAY": "wayland-1", "__GL_SYNC_TO_VBLANK": "1", "vblank_mode": "3"}},
		{"x11 is left alone", map[string]string{"DISPLAY": ":0"}, map[string]string{"DISPLAY": ":0"}},
		{"an empty WAYLAND_DISPLAY is not wayland", map[string]string{"WAYLAND_DISPLAY": ""}, map[string]string{"WAYLAND_DISPLAY": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{}
			for k, v := range tc.env {
				env[k] = v
			}
			lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
			swapIntervalZero(lookup, func(k, v string) error { env[k] = v; return nil })
			if len(env) != len(tc.want) {
				t.Fatalf("env = %v, want %v", env, tc.want)
			}
			for k, v := range tc.want {
				if env[k] != v {
					t.Fatalf("env = %v, want %v", env, tc.want)
				}
			}
		})
	}
}
