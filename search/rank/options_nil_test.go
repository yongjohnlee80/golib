package rank

import (
	"context"
	"testing"
)

// A nil option is no option: NewTEI, NewRerankAPI and NewSearcher skip it, as every constructor in golib does, and the
// options after it still apply.
func TestTheConstructorsSkipANilOption(t *testing.T) {
	// No server answers: each constructor applies its options, then fails to reach one.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, build := range map[string]func(applied *bool){
		"NewTEI": func(applied *bool) {
			_, _ = NewTEI(ctx, "http://127.0.0.1:1", "", nil, nil, func(*TEI) { *applied = true })
		},
		"NewRerankAPI": func(applied *bool) {
			NewRerankAPI("http://127.0.0.1:1", "", "m", nil, nil, func(*RerankAPI) { *applied = true })
		},
		"NewSearcher": func(applied *bool) {
			NewSearcher(nil, nil, nil, nil, func(*Searcher) { *applied = true })
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
