package decl

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// HOT RELOAD — the running program follows its QML files, for development.
//
//	p, err := tuidecl.NewProgram(
//	    tuidecl.Layout(os.DirFS("ui"), "editor.qml"),     // from disk, not embedded
//	    tuidecl.Themes(os.DirFS("ui"), "themes", "editor.theme", "1.0"),
//	    tuidecl.HotReload(tuidecl.OnReloadError(showInStatusLine)),
//	    …)
//
// While it runs, the Program polls every file it read through Layout, Themes
// and Components. A change is acted on once the files hold still for one
// interval — an editor that saves in two writes is one reload — and then, on
// the UI loop, the engine's component cache is cleared (Qt's
// QQmlEngine::clearComponentCache) and the layout reconciled: what can be
// patched is patched, keeping its identity and state — focus, scroll, text
// typed; what cannot is rebuilt, and the Result says which and why.
//
// A save caught half-written — a document that ends mid-construct — is waited
// out without a word. Any other refusal goes to OnReloadError, and the screen
// stays as it was. A reload that failed PART-WAY leaves a tree that only
// Destroy may touch; the next good save then builds the screen afresh from the
// files, carrying the host's current source values across.
//
// Go is the boundary. Handlers, functions and the host's own code are compiled:
// changing one is a rebuild and a restart. A reload re-reads documents, never
// what the host injects.
//
// Without HotReload nothing is polled, and an embedded document is simply the
// program.

// HotReload makes a running Program follow its QML files. The layout must be
// given with [Layout], so there is a file to follow.
func HotReload(opts ...HotReloadOption) ProgramOption {
	h := &hotReload{interval: 250 * time.Millisecond}
	for _, o := range opts {
		if o != nil {
			o(h)
		}
	}
	return func(c *programConfig) { c.hot = h }
}

// HotReloadOption configures [HotReload].
type HotReloadOption func(*hotReload)

// ReloadInterval is how often the files are polled, and how long they must
// hold still before a reload. Default 250ms.
func ReloadInterval(d time.Duration) HotReloadOption {
	return func(h *hotReload) {
		if d > 0 {
			h.interval = d
		}
	}
}

// OnReload receives each applied reload's Result, on the UI loop.
func OnReload(fn func(decl.Result)) HotReloadOption {
	return func(h *hotReload) { h.onReload = fn }
}

// OnReloadError receives each refused reload, on the UI loop; the screen is
// kept as it was. Unset, refusals go to the Program's error sink.
func OnReloadError(fn func(error)) HotReloadOption {
	return func(h *hotReload) { h.onError = fn }
}

type hotReload struct {
	interval time.Duration
	onReload func(decl.Result)
	onError  func(error)
}

// watchedDir is a directory whose `.qml` files a Program read.
type watchedDir struct {
	fsys fs.FS
	dir  string
}

// snapshot is every followed file's content, by a name unique to its source.
type snapshot map[string][sha256.Size]byte

func (a snapshot) equal(b snapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// snapshot reads every followed file. A file that cannot be read now — being
// replaced, say — is recorded as missing, which is a change like any other.
func (p *Program) snapshot() snapshot {
	out := snapshot{}
	if src, err := fs.ReadFile(p.cfg.layoutFS, p.cfg.layoutFile); err == nil {
		out["layout:"+p.cfg.layoutFile] = sha256.Sum256(src)
	}
	for i, w := range p.cfg.watched {
		entries, err := fs.ReadDir(w.fsys, w.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || path.Ext(e.Name()) != ".qml" {
				continue
			}
			file := path.Join(w.dir, e.Name())
			if src, err := fs.ReadFile(w.fsys, file); err == nil {
				out[fmt.Sprintf("%d:%s", i, file)] = sha256.Sum256(src)
			}
		}
	}
	return out
}

// follow polls until ctx ends. A snapshot that differs from the last one
// applied is acted on once it has held for one interval.
func (p *Program) follow(ctx context.Context) {
	f := follower{applied: p.snapshot()}
	tick := time.NewTicker(p.cfg.hot.interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if f.step(p.snapshot()) {
			p.schedule(p.hotReload)
		}
	}
}

// follower is the debounce: the files as last applied, and as last seen
// changed but not yet held still.
type follower struct{ applied, pending snapshot }

// step takes one poll's snapshot and reports whether to reload now: when the
// files differ from what was applied AND are what the previous poll saw.
func (f *follower) step(now snapshot) bool {
	switch {
	case now.equal(f.applied):
		f.pending = nil // changed and changed back: nothing to do
	case f.pending == nil || !now.equal(f.pending):
		f.pending = now // changed; wait for it to hold still
	default:
		f.applied, f.pending = now, nil
		return true
	}
	return false
}

// hotReload runs on the UI loop: clear the cache, re-read the layout, and
// reconcile — or, after a part-way failure, remount.
func (p *Program) hotReload() {
	h := p.cfg.hot
	refuse := func(err error) {
		switch {
		case h.onError != nil:
			h.onError(err)
		case p.cfg.sink != nil:
			p.cfg.sink(err)
		default:
			p.keep(err)
		}
	}
	src, err := fs.ReadFile(p.cfg.layoutFS, p.cfg.layoutFile)
	if err != nil {
		refuse(fmt.Errorf("hot reload: %w", err))
		return
	}
	spec, err := parseReload(p.cfg.layoutFile, src)
	if err != nil {
		if errors.Is(err, decl.ErrIncomplete) {
			return // a save caught half-written; the finished one follows
		}
		refuse(err)
		return
	}
	var res decl.Result
	if p.tree.Failed() {
		res, err = p.remount(spec)
	} else {
		if err = p.tree.ClearComponentCache(); err == nil {
			res, err = p.tree.Reconcile(spec)
		}
		if err == nil && res.RootReplaced {
			p.showRoot()
		}
	}
	if err != nil {
		refuse(err)
		return
	}
	if h.onReload != nil {
		h.onReload(res)
	}
}

// remount builds the screen afresh from the files, and shows it, after a
// reload that failed part-way. The host's sources keep their current values;
// everything else — focus, scroll, typed text — starts over, as a restart would.
func (p *Program) remount(spec qml.SpecTree) (decl.Result, error) {
	c := p.cfg
	if c.sink == nil {
		// Handler errors are THIS Program's to keep and return from Run.
		c.sink = p.keep
	}
	c.sources = make(map[string]any, len(p.cfg.sources))
	for name, v := range p.cfg.sources {
		c.sources[name] = v
		if cur, ok := p.tree.Source(name); ok {
			c.sources[name] = cur
		}
	}
	fresh, err := mount(c, spec)
	if err != nil {
		return decl.Result{}, err
	}
	old := p.tree
	p.tree, p.adapter, p.root = fresh.tree, fresh.adapter, fresh.root
	// The fresh tree schedules through its own Program value; point it here.
	fresh.redirect(p)
	p.app.SetRoot(p.root)
	res := decl.Result{RootReplaced: true, Created: p.tree.Len()}
	return res, old.Destroy()
}

// redirect hands a Program built for a remount over to the one that runs it:
// work it had queued goes to the running Program's loop.
func (p *Program) redirect(to *Program) {
	p.mu.Lock()
	pending := p.pending
	p.pending, p.app = nil, to.app
	p.mu.Unlock()
	for _, fn := range pending {
		to.schedule(fn)
	}
}
