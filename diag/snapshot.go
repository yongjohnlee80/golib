package diag

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"slices"
	"strings"
	"time"

	"github.com/yongjohnlee80/golib/logger"
)

// Site is one allocation stack in a snapshot: where memory still in use was allocated, not what
// retains it. One site per distinct full stack, flat; ranked by its estimated bytes.
type Site struct {
	InUse   int64    `json:"in_use"`  // estimated bytes in use, allocated here
	Objects int64    `json:"objects"` // estimated objects in use
	Stack   []string `json:"stack"`   // its first frames, "pkg.Func file:line", innermost first: display only
}

// Snapshot is a snapshot line of samples.jsonl: the heap profile written, and the top sites.
// Its sites, its heap_live and its file are close observations, not one: the memory profile is up
// to two GC cycles older than heap_live, and the file is a separate read of it.
type Snapshot struct {
	Kind     string    `json:"kind"` // "snapshot"
	At       time.Time `json:"at"`
	N        int       `json:"n"`
	HeapLive uint64    `json:"heap_live"`       // the sample that took it
	File     string    `json:"file,omitempty"`  // heap-<n>.pb.gz, in the monitor's directory
	Error    string    `json:"error,omitempty"` // why the file was not written
	SitesAge string    `json:"sites_age"`       // sitesAge: the sites' age against heap_live
	Top      []Site    `json:"top"`
	Groups   []Group   `json:"groups,omitempty"` // with WithGroups: every site summed by group
}

// Group is every site whose stack's first matching frame is the same package and type (WithGroups).
type Group struct {
	Group   string `json:"group"` // "pkg/path.Type", "pkg/path.Func", or "other"
	InUse   int64  `json:"in_use"`
	Objects int64  `json:"objects"`
}

const sitesAge = "up to two GC cycles"

// stackFrames is how many frames of a site's stack a snapshot line shows.
const stackFrames = 8

// snapshot writes heap-<n>.pb.gz and its line. Called with m.mu held.
func (m *Monitor) snapshot(s Sample) {
	m.n++
	m.last = s.HeapLive
	top, groups := sites(m.c.top, m.c.groups)
	snap := Snapshot{Kind: "snapshot", At: s.At, N: m.n, HeapLive: s.HeapLive, SitesAge: sitesAge, Top: top, Groups: groups}
	name := fmt.Sprintf("heap-%d.pb.gz", m.n)
	if err := writeHeapProfile(filepath.Join(m.dir, name)); err != nil {
		snap.Error = err.Error()
		if m.err == nil { // Close's first failure, as a samples write's is
			m.err = fmt.Errorf("diag: writing %s: %w", name, err)
		}
	} else {
		snap.File = name
	}
	m.write(snap)
	m.logSnapshot(snap)
}

// logSnapshot writes snap to the logger, if any: its top site and its largest groups.
func (m *Monitor) logSnapshot(snap Snapshot) {
	if m.c.log == nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "diag: snapshot %d at heap live %s", snap.N, size(snap.HeapLive))
	if snap.File != "" {
		fmt.Fprintf(&b, " (%s)", snap.File)
	}
	if len(snap.Top) > 0 && len(snap.Top[0].Stack) > 0 {
		fmt.Fprintf(&b, "; top site %s in %s", size(uint64(snap.Top[0].InUse)), snap.Top[0].Stack[0])
	}
	for i, g := range snap.Groups[:min(len(snap.Groups), 5)] {
		sep := ", "
		if i == 0 {
			sep = "; by group: "
		}
		fmt.Fprintf(&b, "%s%s %s", sep, g.Group, size(uint64(g.InUse)))
	}
	logger.Debug(m.c.log, b.String())
}

// writeHeapProfile writes the heap profile in pprof's gzipped protobuf form. The file is new:
// a snapshot is never overwritten.
func writeHeapProfile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// sites are the top allocation stacks by their estimated in-use bytes, scaled for
// MemProfileRate's sampling as runtime/pprof writes a heap profile; with prefixes, also every
// site summed by group (WithGroups), largest first.
func sites(top int, prefixes []string) ([]Site, []Group) {
	var recs []runtime.MemProfileRecord
	n, _ := runtime.MemProfile(nil, false)
	for {
		recs = make([]runtime.MemProfileRecord, n+n/4+16) // room for sites added meanwhile
		var ok bool
		if n, ok = runtime.MemProfile(recs, false); ok {
			recs = recs[:n]
			break
		}
	}
	// The profile keeps a record per stack and object size: each record is scaled by its own
	// size, then a stack's records are summed, so one site is one stack.
	rate := int64(runtime.MemProfileRate)
	byStack := map[string]int{} // a stack's key -> its index in out
	out := make([]Site, 0, len(recs))
	byGroup := map[string]*Group{}
	for i := range recs {
		objs, bytes := scaleHeapSample(recs[i].InUseObjects(), recs[i].InUseBytes(), rate)
		if bytes <= 0 {
			continue
		}
		pcs := recs[i].Stack()
		if len(prefixes) > 0 {
			name := groupOf(pcs, prefixes)
			g := byGroup[name]
			if g == nil {
				g = &Group{Group: name}
				byGroup[name] = g
			}
			g.InUse += bytes
			g.Objects += objs
		}
		key := stackKey(pcs)
		if j, ok := byStack[key]; ok {
			out[j].InUse += bytes
			out[j].Objects += objs
			continue
		}
		byStack[key] = len(out)
		out = append(out, Site{InUse: bytes, Objects: objs, Stack: frames(pcs)})
	}
	slices.SortStableFunc(out, func(a, b Site) int { return cmp.Compare(b.InUse, a.InUse) })
	var groups []Group
	for _, g := range byGroup {
		groups = append(groups, *g)
	}
	slices.SortFunc(groups, func(a, b Group) int {
		return cmp.Or(cmp.Compare(b.InUse, a.InUse), strings.Compare(a.Group, b.Group))
	})
	return out[:min(top, len(out))], groups
}

// groupOf is a stack's group: its first frame, innermost first, whose function starts with one of
// prefixes, named by that function's own identity (identityOf), not by the prefix that selected
// it; "other" when no frame matches.
func groupOf(pcs []uintptr, prefixes []string) string {
	fs := runtime.CallersFrames(pcs)
	for {
		f, more := fs.Next()
		for _, p := range prefixes {
			if strings.HasPrefix(f.Function, p) {
				return identityOf(f.Function)
			}
		}
		if !more {
			return "other"
		}
	}
}

// identityOf is a function's group name from its own full name, whatever prefix selected it:
// its package path and receiver type ("pkg/path.Type" for "pkg/path.(*Type).Method"), or its
// function ("pkg/path.Func" for "pkg/path.Func.func1").
func identityOf(function string) string {
	slash := strings.LastIndex(function, "/")
	dot := strings.Index(function[slash+1:], ".")
	if dot < 0 {
		return function
	}
	pkg, rest := function[:slash+1+dot], function[slash+1+dot+1:]
	return pkg + "." + receiverOf(rest)
}

// receiverOf is a function name's type ("(*Type).Method", "Type.Method" -> "Type"), or the
// function's own name without one ("Func", "Func.func1" -> "Func").
func receiverOf(name string) string {
	if t, ok := strings.CutPrefix(name, "(*"); ok {
		t, _, _ = strings.Cut(t, ")")
		return t
	}
	t, _, _ := strings.Cut(name, ".")
	return t
}

// scaleHeapSample estimates a site's in-use objects and bytes from its sampled counts, as
// runtime/pprof does for a heap profile: count and bytes together, each object of the site's
// average sampled size having been sampled with probability 1-exp(-size/rate).
func scaleHeapSample(count, size, rate int64) (int64, int64) {
	if count == 0 || size == 0 {
		return 0, 0
	}
	if rate <= 1 {
		return count, size // every allocation was recorded
	}
	avg := float64(size) / float64(count)
	scale := 1 / (1 - math.Exp(-avg/float64(rate)))
	return int64(float64(count) * scale), int64(float64(size) * scale)
}

// stackKey is a full stack's identity: its program counters.
func stackKey(pcs []uintptr) string {
	b := make([]byte, 0, len(pcs)*8)
	for _, pc := range pcs {
		b = binary.LittleEndian.AppendUint64(b, uint64(pc))
	}
	return string(b)
}

// frames names a stack's frames, innermost first, from its first frame outside the runtime (the
// allocator's own frames, as runtime/pprof hides them in a heap profile); all of them when every
// frame is the runtime's.
func frames(pcs []uintptr) []string {
	var all []runtime.Frame
	fs := runtime.CallersFrames(pcs)
	for {
		f, more := fs.Next()
		all = append(all, f)
		if !more {
			break
		}
	}
	from := 0
	for from < len(all) && strings.HasPrefix(all[from].Function, "runtime.") {
		from++
	}
	if from == len(all) {
		from = 0
	}
	out := make([]string, 0, min(len(all)-from, stackFrames))
	for _, f := range all[from:min(len(all), from+stackFrames)] {
		out = append(out, fmt.Sprintf("%s %s:%d", f.Function, f.File, f.Line))
	}
	return out
}
