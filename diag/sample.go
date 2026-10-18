package diag

import (
	"runtime/metrics"
	"time"
)

// Sample is one line of samples.jsonl, and what Read returns.
type Sample struct {
	Kind       string    `json:"kind"` // "sample"
	At         time.Time `json:"at"`
	HeapLive   uint64    `json:"heap_live"`     // what survived the last collection: held
	HeapObjs   uint64    `json:"heap_objects"`  // the heap's objects, garbage not yet swept included
	HeapFree   uint64    `json:"heap_free"`     // free, still mapped: retained, not a leak
	HeapUnused uint64    `json:"heap_unused"`   // allocated spans' unused space: fragmentation
	HeapRel    uint64    `json:"heap_released"` // freed and given back to the OS
	GoTotal    uint64    `json:"go_total"`      // everything the runtime has mapped
	RSS        uint64    `json:"rss"`           // the process's resident size; 0 where unknown
	Goroutines uint64    `json:"goroutines"`
	GCCycles   uint64    `json:"gc_cycles"` // completed collections
}

// sampleNames are the runtime/metrics a sample reads, in the order Read stores them.
var sampleNames = []string{
	"/gc/heap/live:bytes",
	"/memory/classes/heap/objects:bytes",
	"/memory/classes/heap/free:bytes",
	"/memory/classes/heap/unused:bytes",
	"/memory/classes/heap/released:bytes",
	"/memory/classes/total:bytes",
	"/sched/goroutines:goroutines",
	"/gc/cycles/total:gc-cycles",
}

// Read reads the runtime's memory and the process's resident size now: the one memory reader, for
// a monitor's samples and for a program's own display of them.
func Read() Sample {
	ms := make([]metrics.Sample, len(sampleNames))
	for i, n := range sampleNames {
		ms[i].Name = n
	}
	metrics.Read(ms)
	v := func(i int) uint64 {
		if ms[i].Value.Kind() != metrics.KindUint64 {
			return 0 // a name this Go does not support
		}
		return ms[i].Value.Uint64()
	}
	return Sample{Kind: "sample", At: time.Now(),
		HeapLive: v(0), HeapObjs: v(1), HeapFree: v(2), HeapUnused: v(3), HeapRel: v(4), GoTotal: v(5),
		Goroutines: v(6), GCCycles: v(7), RSS: residentSize()}
}
