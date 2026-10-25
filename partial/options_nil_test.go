package partial

import "testing"

// A nil option is no option: Bind and ApplyRules skip it, as every constructor in golib does, and the
// options after it still apply.
func TestTheConstructorsSkipANilOption(t *testing.T) {
	for name, build := range map[string]func(applied *bool){
		"Bind": func(applied *bool) {
			_, _ = Bind[release]([]byte(`{"title":"x"}`), nil, func(*bindConfig) { *applied = true })
		},
		"ApplyRules": func(applied *bool) {
			p, err := Bind[release]([]byte(`{"title":"x"}`))
			if err != nil {
				panic(err)
			}
			_, _ = ApplyRules(&setterOnly{}, p, nil, func(*applyConfig) { *applied = true })
		},
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s with a nil option panicked: %v", name, r)
				}
			}()
			applied := false
			build(&applied)
			if !applied {
				t.Errorf("%s: the option after the nil was not applied", name)
			}
		}()
	}
}
