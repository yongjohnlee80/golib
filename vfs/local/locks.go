//go:build linux || darwin

package local

import (
	"slices"
	"sync"
)

// pathLocks serializes mutations per name, so a condition check and its commit are one step for every
// caller of this instance. Entries are reference-counted and dropped when unused.
type pathLocks struct {
	mu sync.Mutex
	m  map[string]*pathLock
}

type pathLock struct {
	mu   sync.Mutex
	refs int
}

// lock takes the locks for names in a fixed order (sorted, deduplicated) and returns the unlock.
func (p *pathLocks) lock(names ...string) (unlock func()) {
	names = slices.Clone(names)
	slices.Sort(names)
	names = slices.Compact(names)
	held := make([]*pathLock, 0, len(names))
	for _, n := range names {
		p.mu.Lock()
		if p.m == nil {
			p.m = map[string]*pathLock{}
		}
		l := p.m[n]
		if l == nil {
			l = &pathLock{}
			p.m[n] = l
		}
		l.refs++
		p.mu.Unlock()
		l.mu.Lock()
		held = append(held, l)
	}
	return func() {
		for i, l := range held {
			l.mu.Unlock()
			p.mu.Lock()
			if l.refs--; l.refs == 0 {
				delete(p.m, names[i])
			}
			p.mu.Unlock()
		}
	}
}
