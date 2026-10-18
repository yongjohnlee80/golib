// Package diag is an attachable memory monitor for a long-running Go program, off unless a
// program turns it on.
//
// A [Monitor] samples the runtime's memory classes and the process's resident size into
// samples.jsonl every period. Each time the live heap grows by a step, it writes a heap profile
// (heap-<n>.pb.gz, for go tool pprof and its -diff_base) and a snapshot line ranking the
// allocation sites holding the live heap. With Options.Socket it serves the runtime's profiles on
// pprof.sock, a unix socket only the user can open, and [Fetch] reads one from a running process.
//
// Held and retained are told apart: a sample's heap_live is what survived the last collection,
// and heap_free is memory the runtime freed and still holds from the OS, which is not a leak.
//
// A program turns it on from a flag shaped like --debug[=DIR], or from [FromEnv] when a wrapper
// sets a variable:
//
//	if o, ok := diag.FromEnv("APP_PROFILE"); ok {
//		m, err := diag.Start(ctx, o)
//		if err != nil {
//			return err
//		}
//		defer m.Close()
//	}
//
// The directory holds no user data: numbers, symbolized stacks and the binary's paths.
//
// Standard library only. The profiles are served from runtime/pprof on a mux of the monitor's
// own: net/http/pprof is not imported, since importing it registers its handlers on
// http.DefaultServeMux, and so on any server a program runs there.
package diag
