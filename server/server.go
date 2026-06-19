package server

import "context"

// Server is the lifecycle contract a transport server implements (for example
// golib/server/http). The core defines the shape; transports own the
// implementation (listener creation, serving, graceful shutdown).
type Server interface {
	// Run serves until ctx is cancelled, then shuts down gracefully. It returns
	// nil on a clean shutdown.
	Run(ctx context.Context) error

	// Shutdown gracefully drains in-flight work, bounded by ctx.
	Shutdown(ctx context.Context) error

	// Addr returns the resolved listen address (valid once the server has bound,
	// so it reports the real port after binding ":0").
	Addr() string
}
