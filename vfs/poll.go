package vfs

import (
	"context"
	"errors"
	"time"

	"github.com/yongjohnlee80/golib/errs"
)

// Poll watches any driver by listing dir every interval and comparing versions with the previous
// listing: a new entry is OpCreate, a changed Version is OpWrite, a vanished one is OpRemove. The first
// listing is the baseline and produces no events. It returns once that baseline is taken.
//
// Poll only observes: a change undone between two listings, or a rename, is not reported as the same
// events a native watcher would give. What it guarantees is convergence — after a change holds for one
// interval, the events describe it. A listing that fails (dir removed, permissions) yields one
// OpOverflow for dir and the next successful listing becomes the new baseline.
//
// The goroutine ends and the channel closes when ctx ends. Cost: one ReadDir (or Walk, with
// [Recursive]) per interval.
func Poll(ctx context.Context, fsys FS, dir string, interval time.Duration, opts ...WatchOption) (<-chan Event, error) {
	if interval <= 0 {
		return nil, errs.Wrap(errs.ErrInvalidArgument, "vfs.Poll: interval must be positive, got %v", interval)
	}
	cfg := ResolveWatch(opts)
	base, err := snapshot(ctx, fsys, dir, cfg.Recursive)
	if err != nil {
		return nil, err
	}
	out := make(chan Event, 64)
	go func() {
		defer close(out)
		t := time.NewTicker(interval)
		defer t.Stop()
		send := func(ev Event) bool {
			select {
			case out <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			next, err := snapshot(ctx, fsys, dir, cfg.Recursive)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				if !send(Event{Path: dir, Op: OpOverflow}) {
					return
				}
				base = nil // the next good listing becomes the baseline
				continue
			}
			if base != nil {
				for p, v := range next {
					old, ok := base[p]
					switch {
					case !ok:
						if !send(Event{Path: p, Op: OpCreate}) {
							return
						}
					case old != v:
						if !send(Event{Path: p, Op: OpWrite}) {
							return
						}
					}
				}
				for p := range base {
					if _, ok := next[p]; !ok {
						if !send(Event{Path: p, Op: OpRemove}) {
							return
						}
					}
				}
			}
			base = next
		}
	}()
	return out, nil
}

// snapshot maps every entry under dir to its Version.
func snapshot(ctx context.Context, fsys FS, dir string, recursive bool) (map[string]Version, error) {
	m := make(map[string]Version)
	if !recursive {
		entries, err := fsys.ReadDir(ctx, dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			m[e.Path] = e.Version
		}
		return m, nil
	}
	for e, err := range Walk(ctx, fsys, dir) {
		if err != nil {
			return nil, err
		}
		m[e.Path] = e.Version
	}
	return m, nil
}
