// Package diag is an attachable memory monitor for a long-running Go program, off unless a
// program turns it on.
//
// A [Monitor] samples the runtime's memory classes and the process's resident size into
// samples.jsonl every period ([WithEvery]). Each time the live heap grows by a step ([WithStep]),
// it writes a heap profile (heap-<n>.pb.gz, for go tool pprof and its -diff_base) and a snapshot
// line ranking the allocation sites holding the live heap ([WithTop]), summed by package and type
// with [WithGroups]. [WithLogger] also writes both to a program's own log. With [WithSocket] it
// serves heap, allocs, goroutine and CPU profiles on pprof.sock, a unix socket only the user can
// open, and [Fetch] reads one from a running process. One monitor owns its directory until Close.
//
// Held and retained are told apart: a sample's heap_live is what the last collection found
// reachable, and heap_free is memory the runtime freed and still holds from the OS, which is not a
// leak.
//
// A program turns it on from a flag shaped like --debug[=DIR], or from [FromEnv] when a wrapper
// sets a variable:
//
//	if dir, ok := diag.FromEnv("APP_PROFILE"); ok {
//		m, err := diag.Start(ctx, dir, diag.WithSocket())
//		if err != nil {
//			return err
//		}
//		defer m.Close()
//	}
//
// What each artifact can hold: samples are numbers; snapshot lines, heap-<n>.pb.gz and the heap
// and allocs profiles hold allocation stacks (function names, source paths, the binary's
// mappings), never heap contents; goroutine and CPU profiles also hold the program's pprof labels,
// whatever it put there, so treat those as sensitive.
//
// Standard library only. The profiles are served from runtime/pprof on a mux of the monitor's
// own: net/http/pprof is not imported, since importing it registers its handlers on
// http.DefaultServeMux, and so on any server a program runs there.
package diag
