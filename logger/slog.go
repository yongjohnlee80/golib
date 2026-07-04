package logger

import (
	"context"
	"fmt"
	"log/slog"
)

// Fields is a structured payload: log it (or embed it in an [Entry] via the
// level helpers) and structure-aware sinks like the slog bridge emit each key
// as an attribute instead of one flattened string. The optional "msg" key (a
// string) becomes the record message. Text sinks render it as a plain map.
type Fields map[string]any

// severity → slog level. Notice and Critical sit between/beyond the four
// stdlib levels per slog's documented level-spacing convention.
func slogLevel(s Severity) slog.Level {
	switch s {
	case SeverityDebug:
		return slog.LevelDebug
	case SeverityInfo:
		return slog.LevelInfo
	case SeverityNotice:
		return slog.LevelInfo + 2
	case SeverityWarning:
		return slog.LevelWarn
	case SeverityError:
		return slog.LevelError
	case SeverityCritical:
		return slog.LevelError + 4
	default:
		return slog.LevelInfo
	}
}

// slog level → severity (inverse of slogLevel, by threshold).
func severityOf(l slog.Level) Severity {
	switch {
	case l < slog.LevelInfo:
		return SeverityDebug
	case l < slog.LevelInfo+2:
		return SeverityInfo
	case l < slog.LevelWarn:
		return SeverityNotice
	case l < slog.LevelError:
		return SeverityWarning
	case l < slog.LevelError+4:
		return SeverityError
	default:
		return SeverityCritical
	}
}

// FromSlog returns a [Logger] backed by sl, so an application already built on
// log/slog can supply golib packages a logger with one call. [Fields] payloads
// become attributes; [Entry] payloads become an "err" attribute plus the
// payload's attributes; anything else is rendered as the record message.
func FromSlog(sl *slog.Logger) Logger {
	return slogAdapter{sl: sl}
}

type slogAdapter struct{ sl *slog.Logger }

func (a slogAdapter) Log(severity Severity, payload any) {
	level := slogLevel(severity)
	msg, attrs := destructure(payload)
	a.sl.LogAttrs(context.Background(), level, msg, attrs...)
}

// destructure splits a payload into a record message and attributes.
func destructure(payload any) (string, []slog.Attr) {
	switch p := payload.(type) {
	case Entry:
		msg, attrs := destructure(p.Payload)
		if msg == "" {
			msg = p.Err.Error()
		}
		return msg, append([]slog.Attr{slog.Any("err", p.Err)}, attrs...)
	case Fields:
		msg := ""
		attrs := make([]slog.Attr, 0, len(p))
		for k, v := range p {
			if k == "msg" {
				if s, ok := v.(string); ok {
					msg = s
					continue
				}
			}
			attrs = append(attrs, slog.Any(k, v))
		}
		return msg, attrs
	case nil:
		return "", nil
	default:
		return fmt.Sprintf("%+v", p), nil
	}
}

// NewSlogHandler returns a slog.Handler that forwards every record to l — the
// reverse bridge: plug golib's [Logger] under an existing *slog.Logger
// (slog.New(logger.NewSlogHandler(l))). Record attributes (plus any WithAttrs
// / WithGroup accumulation) arrive as a [Fields] payload with the record
// message under "msg".
func NewSlogHandler(l Logger) slog.Handler {
	return &slogHandler{l: l}
}

type slogHandler struct {
	l      Logger
	prefix string      // dotted group path from WithGroup
	attrs  []slog.Attr // accumulated WithAttrs, already prefixed
}

// Enabled reports true for every level: filtering belongs to the destination
// Logger (MinLevel/BlockList), which cannot be consulted per-level here.
func (h *slogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *slogHandler) Handle(_ context.Context, r slog.Record) error {
	fields := Fields{"msg": r.Message}
	for _, a := range h.attrs {
		fields[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		fields[h.prefix+a.Key] = a.Value.Any()
		return true
	})
	h.l.Log(severityOf(r.Level), fields)
	return nil
}

func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := &slogHandler{l: h.l, prefix: h.prefix, attrs: append([]slog.Attr{}, h.attrs...)}
	for _, a := range attrs {
		next.attrs = append(next.attrs, slog.Attr{Key: h.prefix + a.Key, Value: a.Value})
	}
	return next
}

func (h *slogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &slogHandler{l: h.l, prefix: h.prefix + name + ".", attrs: h.attrs}
}
