# states

Internal immutable state interning with root leases, monotonic IDs and budgeted
collection. Sources retain current structural/child dependencies; cache and frame
owners retain their incoming/outgoing state IDs. Replacement retains new roots
before releasing old ones. Unreachable tuple keys/payloads are removed during
ordinary editing, not only when a document closes.

See [languages](../../languages/README.md) and [LICENSE](../../../LICENSE).
