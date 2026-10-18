# diag

An attachable memory monitor for a long-running Go program, off unless the program turns it on.
Standard library only; importing it registers nothing.

```go
dir, ok := diag.FromEnv("APP_PROFILE") // or a --profile[=DIR] flag
if ok {
	m, err := diag.Start(ctx, dir, diag.WithSocket())
	if err != nil {
		return err
	}
	defer m.Close()
}
```

A monitor owns its directory until `Close`: a second `Start` there is refused. It writes:

| File | What | Contains |
| --- | --- | --- |
| `samples.jsonl` `sample` lines | every period (`WithEvery`, default 5s): live heap, heap free, unused and released, Go total, resident size, goroutines, GC cycles | numbers |
| `samples.jsonl` `snapshot` lines | each time the live heap grows by `WithStep` (default 64 MiB), and at Close: the top allocation sites (`WithTop`, default 15) | allocation stacks |
| `heap-<n>.pb.gz` | the heap profile at each snapshot, for `go tool pprof` and `-diff_base` | allocation stacks |
| `pprof.sock` (`WithSocket`) | a 0600 unix socket serving `heap`, `allocs`, `goroutine` and `profile?seconds=N` (CPU) | heap and allocs: stacks; goroutine and CPU: stacks and the program's pprof labels |

Reading it:

- **Held vs retained:** `heap_live` is what the last collection found reachable. `heap_free` and
  `heap_unused` are the runtime's own, not a leak; `heap_released` is back with the OS.
- **A site** is where in-use memory was allocated, not what retains it: one per full allocation
  stack, its estimate scaled as runtime/pprof scales a heap profile. The sites are up to two GC
  cycles older than the sample's `heap_live`.
- **Attaching:** `diag.Fetch(ctx, socket, "heap", w)` writes a profile `go tool pprof` reads.
  Goroutine and CPU profiles carry the program's labels: treat them as sensitive.

`diag.Read()` is the same reading on demand, for a program's own display.

The monitor never forces a collection and never changes `runtime.MemProfileRate`.
