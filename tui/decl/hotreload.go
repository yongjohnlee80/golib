package decl

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui/i18n"
	"github.com/yongjohnlee80/golib/tui/keymap"
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

// HotReload makes a running Program follow its QML files. A LayoutSource may
// follow Themes, Components or Translations without a layout file; reloads keep
// its last successfully applied source. A file-backed layout must be
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

// OnReload receives each applied reload's Result, on the UI loop, once the reload's layout
// and catalogs are both in place.
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
	// ticks, when set, are the polls in place of a ticker at interval: a test's own clock; polled,
	// when set, hears that a poll has been looked at and acted on
	ticks  <-chan time.Time
	polled chan<- struct{}
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
func (p *Program) snapshot() snapshot { return snapshotOf(p.cfg) }

func snapshotOf(c programConfig) snapshot {
	out := snapshot{}
	if c.layoutFS != nil {
		if src, err := fs.ReadFile(c.layoutFS, c.layoutFile); err == nil {
			out["layout:"+c.layoutFile] = sha256.Sum256(src)
		}
	}
	for i, w := range c.watched {
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
	// Key-mapping schemes are followed file by file. A missing file has no entry, so creating or
	// deleting it is a change; an unreadable one is recorded by its error, so the reload that
	// follows refuses it rather than reading it as absent.
	for i, f := range c.keymapFiles {
		k := fmt.Sprintf("keymap%d:%s", i, f.file)
		switch src, err := fs.ReadFile(f.fsys, f.file); {
		case err == nil:
			out[k] = sha256.Sum256(src)
		case !errors.Is(err, fs.ErrNotExist):
			out[k] = sha256.Sum256([]byte("unreadable: " + err.Error()))
		}
	}
	// Catalogs are followed as the layout's folders are: a saved translation is a change.
	for i, t := range c.translations {
		entries, err := fs.ReadDir(t.fsys, t.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasPrefix(name, t.prefix+"_") || path.Ext(name) != ".xml" {
				continue
			}
			file := path.Join(t.dir, name)
			if src, err := fs.ReadFile(t.fsys, file); err == nil {
				out[fmt.Sprintf("i18n%d:%s", i, file)] = sha256.Sum256(src)
			}
		}
	}
	return out
}

// follow polls until ctx ends. A snapshot that differs from the last one
// applied is acted on once it has held for one interval.
//
// The first "applied" is the files AS MOUNTED, not as they are when polling
// starts: Run starts this after the screen is up, and a save made in between
// would otherwise be the baseline — never seen as a change, never shown.
func (p *Program) follow(ctx context.Context) {
	f := follower{applied: p.mounted}
	ticks := p.cfg.hot.ticks
	if ticks == nil {
		tick := time.NewTicker(p.cfg.hot.interval)
		defer tick.Stop()
		ticks = tick.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
		if f.step(p.snapshot()) {
			p.schedule(p.hotReload)
		}
		if p.cfg.hot.polled != nil {
			p.cfg.hot.polled <- struct{}{}
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

// reloadCandidate is a hot reload read from the files and not yet applied: the catalogs and the key
// map are installed only once the layout has been, so a reload lands whole or not at all.
type reloadCandidate struct {
	catalogs *i18n.Set // nil: the Program has no translations
	keys     *keymap.Map
	spec     qml.SpecTree
}

// hotReload runs on the UI loop: read the files, then apply them (reloadSpec) and install the
// catalogs and the key map with the layout.
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
	c, err := p.loadReloadCandidate()
	switch {
	case errors.Is(err, decl.ErrIncomplete):
		return // a save caught half-written; the finished one follows
	case err != nil:
		refuse(err)
		return
	}
	if err := p.applyReloadCandidate(c); err != nil {
		refuse(err)
	}
}

// loadReloadCandidate reads a hot reload's catalogs, key map and layout. The catalogs and the key
// map are READ first and INSTALLED last (applyReloadCandidate): a bad catalog leaves the layout
// alone, and a good one saved beside a bad layout never relabels the old screen.
func (p *Program) loadReloadCandidate() (reloadCandidate, error) {
	var c reloadCandidate
	if len(p.cfg.translations) > 0 {
		s, err := loadTranslations(p.cfg)
		if err != nil {
			return c, fmt.Errorf("hot reload: %w", err)
		}
		c.catalogs = s
	}
	keys, err := loadKeymap(p.cfg)
	if err != nil {
		return c, fmt.Errorf("hot reload: %w", err)
	}
	c.keys = keys
	src := p.liveLayout
	if p.cfg.layoutFS != nil {
		if src, err = fs.ReadFile(p.cfg.layoutFS, p.cfg.layoutFile); err != nil {
			return c, fmt.Errorf("hot reload: %w", err)
		}
	}
	c.spec, err = parseReload(p.cfg.layoutFile, src)
	return c, err
}

// applyReloadCandidate applies a read reload: the layout first (reloadSpec, its keyId Shortcuts
// checked against the CANDIDATE map), then the catalogs and the key map, in the same frame as the
// layout, and OnReload sees both.
func (p *Program) applyReloadCandidate(c reloadCandidate) error {
	res, err := p.reloadSpec(c.spec, c.keys)
	if err != nil {
		return fmt.Errorf("hot reload: %w", err)
	}
	h := p.cfg.hot
	// They go to the App alone: the poller reads the config from its own goroutine.
	finish := func() {
		if c.catalogs != nil {
			p.app.ApplyTranslations(c.catalogs)
		}
		p.app.ApplyKeymap(c.keys)
		if h.onReload != nil {
			h.onReload(res)
		}
	}
	if res.RootReplaced {
		// The new root is mounted by an enqueued SetRoot, so the catalogs follow it in the
		// same drain: installed now, the old screen would paint a frame in the new catalogs.
		p.app.Update(finish)
		return nil
	}
	// Reconciled in place, the layout is live now: an enqueued install could land a frame
	// later, and that frame would show the new layout in the old catalogs.
	finish()
	return nil
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
//
// UNDER THE LOCK, as NewProgram does it: a provider of the fresh tree may be
// delivering from its own goroutine, and one that reached schedule between
// publishing the App and flushing the queue would overtake work queued before
// it. App.Update never blocks, so posting while holding the lock is safe.
func (p *Program) redirect(to *Program) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, fn := range p.pending {
		to.app.Update(fn)
	}
	p.pending, p.app = nil, to.app
}

// checkReloadKeys mounts spec apart from the live tree, with no App, checks its keyId Shortcuts against
// keys, and destroys it. Its sources are the live tree's, frozen: one provider delivers their current
// values once and never runs, so no host provider is subscribed twice. A layout that does not mount is
// the real reconcile's to report, so it passes here. Loop goroutine only: it reads the live tree.
func (p *Program) checkReloadKeys(spec qml.SpecTree, keys *keymap.Map) error {
	c := p.cfg
	c.sources, c.providers, c.keys = nil, []decl.Provider{frozenSources(p.tree.Sources())}, keys
	q, err := mount(c, spec)
	if err != nil {
		return nil
	}
	defer func() { _ = q.tree.Destroy() }()
	return q.adapter.checkShortcuts(keys)
}

// frozenSources is a provider of fixed values: delivered once, synchronously, and never again.
type frozenSources map[string]qml.SpecValue

func (f frozenSources) Subscribe(fn func(decl.Update)) ([]string, func() error, error) {
	names := slices.Sorted(maps.Keys(f))
	fn(decl.Update{Version: 1, Values: maps.Clone(f)})
	return names, func() error { return nil }, nil
}
