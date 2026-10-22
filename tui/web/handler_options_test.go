package web

import (
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/logger"
)

// A Handler's options set what they name and leave the defaults where given nothing: an empty
// Title, a zero grace and a nil logger keep the default; a WithLimits value reaches ReadLimit,
// zero fields defaulted.
func TestHandlerOptionsKeepTheirDefaults(t *testing.T) {
	h := handlerFor(t, Title(""), ShutdownGrace(0), HandlerLogger(nil))
	if h.title != DefaultTitle || h.grace != DefaultShutdownGrace {
		t.Errorf("empty options changed the title %q or grace %s", h.title, h.grace)
	}
	if _, ok := h.log.(logger.Nop); !ok {
		t.Errorf("a nil logger replaced the default: %T", h.log)
	}

	h = handlerFor(t, Title("Ops"), ShutdownGrace(time.Second), WithLimits(Limits{MaxMessage: 4096}))
	if h.title != "Ops" || h.grace != time.Second {
		t.Errorf("title %q grace %s, want Ops and 1s", h.title, h.grace)
	}
	if got := h.ReadLimit(); got != 4096 {
		t.Errorf("ReadLimit = %d, want the limits' 4096", got)
	}
	if h.limits.EventsPerSecond != DefaultEventsPerSecond {
		t.Errorf("WithLimits cleared an unset field: EventsPerSecond %v", h.limits.EventsPerSecond)
	}
}
