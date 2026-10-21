package web

import (
	"time"
)

// Limits are the resource bounds of Every one has a concrete
// default and is configurable.
//
// They exist because every resource here is allocated on behalf of a remote
// party: the message buffer, the event queue, the CPU that decodes. Without
// bounds, how much the server spends is the client's decision.
type Limits struct {
	// MaxMessage is the largest accepted WebSocket message. On breach the
	// connection closes with 1009 Message Too Big.
	MaxMessage int64

	// EventsPerSecond is the sustained input rate.
	EventsPerSecond float64

	// Burst is a token-bucket CREDIT, not a promise that this many events are
	// absorbed. The event queue holds [Limits.QueueDepth]; anything beyond it
	// applies backpressure and may reach the overload close.
	Burst float64

	// QueueDepth is the un-coalesced event queue capacity.
	QueueDepth int

	// OverloadGrace is how long the queue may stay continuously full before the
	// connection is closed with 1008 Policy Violation. A brief burst is
	// tolerated; a sustained flood is not.
	OverloadGrace time.Duration

	// MaxPending caps connections that have been accepted but not yet
	// authenticated.
	//
	// MaxSessions bounds only what exists AFTER a successful hello, so without
	// this a responsive non-browser that forges Host and Origin can hold
	// arbitrarily many sockets and goroutines while consuming no session slot at
	// all. This is the bound on the unauthenticated waiting room.
	MaxPending int

	// HelloTimeout bounds how long a connection may sit before its first
	// message.
	//
	// Without it a client that connects and says nothing is held forever: the
	// read simply blocks. A pre-auth connection has to prove it is going
	// somewhere, quickly.
	HelloTimeout time.Duration
}

// Defaults from 's table.
const (
	DefaultMaxMessage      = 64 << 10
	DefaultEventsPerSecond = 500
	DefaultBurst           = 2000
	DefaultQueueDepth      = 1024
	DefaultOverloadGrace   = 2 * time.Second

	// DefaultMaxPending is generous relative to DefaultMaxSessions: a burst of
	// legitimate reconnects after a network blip should not be refused, while an
	// unbounded waiting room should not exist.
	DefaultMaxPending = 64

	// DefaultHelloTimeout is short because a real client sends its hello in the
	// same tick it opens the socket.
	DefaultHelloTimeout = 10 * time.Second
)

// DefaultLimits returns 's defaults.
func DefaultLimits() Limits {
	return Limits{
		MaxMessage:      DefaultMaxMessage,
		EventsPerSecond: DefaultEventsPerSecond,
		Burst:           DefaultBurst,
		QueueDepth:      DefaultQueueDepth,
		OverloadGrace:   DefaultOverloadGrace,
		MaxPending:      DefaultMaxPending,
		HelloTimeout:    DefaultHelloTimeout,
	}
}

// normalize fills zero fields with defaults and refuses nothing: a zero Limits
// is the documented way to ask for the defaults, and a caller who sets one field
// should not have to restate the rest.
func (l Limits) normalize() Limits {
	d := DefaultLimits()
	if l.MaxMessage <= 0 {
		l.MaxMessage = d.MaxMessage
	}
	if l.EventsPerSecond <= 0 {
		l.EventsPerSecond = d.EventsPerSecond
	}
	if l.Burst <= 0 {
		l.Burst = d.Burst
	}
	if l.QueueDepth <= 0 {
		l.QueueDepth = d.QueueDepth
	}
	if l.OverloadGrace <= 0 {
		l.OverloadGrace = d.OverloadGrace
	}
	if l.MaxPending <= 0 {
		l.MaxPending = d.MaxPending
	}
	if l.HelloTimeout <= 0 {
		l.HelloTimeout = d.HelloTimeout
	}
	return l
}
