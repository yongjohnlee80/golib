// Package streamcache retains a bounded region of a forward-only byte stream
// and hands out owning views into it.
//
// It knows nothing about tokens or syntax. "Read forward, keep what someone
// still needs, let go of the rest" describes a lexer, a protocol framer and a
// log tailer equally well, which is why it is its own package rather than a
// detail inside a parser.
//
// # Usage
//
//	c := streamcache.New(r)
//	if _, err := c.Ensure(off, 64); err != nil { … }
//	v, err := c.Acquire(off, off+16)   // owns the bytes until Close
//	if err != nil { … }
//	defer v.Close()
//	text, _ := v.String()
//	c.Release(off)                     // let go of everything before off
//
// # Lifetimes, not immutability
//
// Every access goes through [Cache.Acquire], which returns a [View] that owns
// the bytes until it is closed. There is deliberately no way to obtain bytes
// without also obtaining the lifetime that keeps them valid.
//
// An earlier design argued that append-only storage made readers safe because
// written bytes never change. That is wrong twice, and both were reproduced
// rather than reasoned about:
//
//   - Immutable payload does not synchronise the lookup that FINDS a segment,
//     nor its release, nor the lifetime of a view already handed out.
//   - Bytes in a held segment are not immutable at all if the segment is still
//     PARTIAL — the writer keeps filling it. A one-byte reader makes that the
//     common case, and it is a data race. [Cache] therefore never writes into a
//     segment a view holds.
//
// # Memory
//
// Peak is O(segment + retained views). A consumer that acquires nothing keeps
// one segment plus whatever it is mid-read on, whatever the size of the stream.
// Retention is the caller's choice; this package has no policy of its own,
// because it cannot know what a caller still needs.
//
// [Cache.Release] sets a WATERMARK, and the watermark alone decides what is
// still acquirable. The offset may be beyond what has been read, meaning "skip
// forward": those bytes are dropped as they arrive. It is a statement about the
// stream, not about how much of it happens to have been read when it is made — not which buffers happen to have been freed. Otherwise the
// answer would depend on whether an unrelated view is holding an unrelated
// segment in front of the span, which is not something a caller can reason
// about. Freeing follows on its own schedule: unheld segments below the
// watermark go at once, held ones are freed by their last Close, and old views
// keep reading their own bytes throughout. One view of the first byte of a
// stream does not pin the rest of it.
//
// # Concurrency
//
// A Cache may be read by several goroutines while another advances it. A held
// segment is never written to and never dropped; when the writer needs space
// and the last segment is held, it ALLOCATES rather than waiting.
//
// That is a correctness requirement, not a tuning choice: a consumer holding a
// view on the first token of a statement while scanning forward for its
// terminator would otherwise deadlock — the writer needs a segment, all are
// held, the writer blocks, and the consumer never releases because it is
// waiting for the writer.
package streamcache
