# streamcache

Retains a bounded region of a forward-only byte stream and hands out **owning
views** into it.

It knows nothing about tokens or syntax. *"Read forward, keep what someone still
needs, let go of the rest"* describes a lexer, a protocol framer and a log
tailer equally well, which is why it is its own package rather than a detail
inside a parser.

```go
c := streamcache.New(r)                  // io.Reader in; the Cache never closes it
if _, err := c.Ensure(off, 64); err != nil { … }

v, err := c.Acquire(off, off+16)         // owns the bytes until Close
if err != nil { … }
defer v.Close()

text, _ := v.String()                    // or v.Reader() / v.AppendTo(dst)
c.Release(off)                           // let go of everything before off
```

`streamcache.NewBytes(b)` borrows a caller's slice as one immutable segment —
**no copy** — so the resident case pays nothing for the machinery the streaming
case needs, and both take the same code path.

## Why a cache at all

An `io.Reader` is single-pass. Without retention, three things a byte-consuming
layer routinely owes its callers are impossible: backtracking wider than one
peek, diagnostics that quote the line an error points into, and re-reading a
span under a different assumption. Each otherwise becomes a limit that leaks
outward — *"you may not look back more than N"* — discovered by whoever needs
N+1.

## Lifetimes, not immutability

Every access goes through `Acquire`, which returns a `View` that **owns** the
bytes until closed. There is deliberately no way to obtain bytes without also
obtaining the lifetime that keeps them valid.

An earlier design argued that append-only storage made readers safe because
written bytes never change. That was wrong twice, and both were reproduced
rather than reasoned about:

- Immutable payload does not synchronise the lookup that **finds** a segment,
  nor its release, nor the lifetime of a view already handed out.
- Bytes in a held segment are not immutable at all while the segment is still
  **partial** — the writer keeps filling it. A one-byte reader makes that the
  common case, and it was a data race.

## Retention never blocks the writer

A held segment is never written to and never dropped. When the writer needs
space and the last segment is held, it **allocates** rather than waiting.

That is a correctness requirement, not tuning. A consumer holding a view on the
first token of a statement while scanning forward for its terminator would
otherwise deadlock: the writer needs a segment, all are held, the writer blocks,
and the consumer never releases because it is waiting for the writer.

## Memory

Peak is **O(segment + retained views)**. A consumer that acquires nothing keeps
one segment plus whatever it is mid-read on, whatever the size of the stream.

Retention is the **caller's** choice; this package has no policy of its own,
because it cannot know what a caller still needs. A `Release` that finds a
segment held records the request and applies it when the last view lets go, so
*released* does not quietly mean *released unless somebody happened to be
holding it*.

A held **partial** segment keeps its whole buffer, including the unwritten
tail, so a view on one byte of a 32 KiB segment retains 32 KiB. Smaller
segments trade allocations for finer-grained reclamation.

## Cost

Both access and reclamation are linear in the work actually done, and both have
benchmarks that assert the *shape* rather than a figure — each was quadratic
once, and each was found by measurement rather than by reading:

| operation | cost |
|---|---|
| `Acquire`, `AppendTo` | O(segments the span covers) |
| `Close` reclamation | O(segments actually dropped) |

`go test -bench 'Acquire|Append|Close'` — doubling the input should roughly
double the time. Quadrupling means a per-item search has come back.
