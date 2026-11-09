package widget

import "github.com/yongjohnlee80/golib/tui/style"

// ResizableStyle carries the two looks a resize grip has. It follows
// ButtonStyle, ModalStyle and MenuStyle exactly — immutable, nil-safe,
// token-valued — so a consumer who has learned one has learned them all.
//
// Only the grip is styled: the wrapper itself paints nothing, and the child
// keeps whatever look it already had. A wrapper that restyled its child would be
// reaching into a component that does not know it has been wrapped.
type ResizableStyle struct {
	handle style.Style // the grip at rest
	active style.Style // the grip during a drag
}

// NewResizableStyle builds a style from the resting look, deriving the active
// one by reversing it.
func NewResizableStyle(handle style.Style) *ResizableStyle {
	return &ResizableStyle{handle: handle, active: handle.Reverse(true)}
}

// DefaultResizableStyle is the look a wrapper has when its author has said
// nothing. Token-valued, so it follows the App's theme.
func DefaultResizableStyle() *ResizableStyle {
	h := style.New().Background(style.TokenSurface).Foreground(style.TokenBorder)
	return &ResizableStyle{handle: h, active: h.Reverse(true)}
}

// Handle returns the grip's resting look. Nil-safe, like every accessor here.
func (s *ResizableStyle) Handle() style.Style {
	if s == nil {
		return DefaultResizableStyle().handle
	}
	return s.handle
}

// Active returns the grip's look while a drag is running.
func (s *ResizableStyle) Active() style.Style {
	if s == nil {
		return DefaultResizableStyle().active
	}
	return s.active
}

// WithHandle returns a copy with the resting look replaced. A copy, never a
// mutation: these values are shared between wrappers by design.
func (s *ResizableStyle) WithHandle(v style.Style) *ResizableStyle {
	c := s.cloneResizable()
	c.handle = v
	return c
}

// WithActive returns a copy with the dragging look replaced.
func (s *ResizableStyle) WithActive(v style.Style) *ResizableStyle {
	c := s.cloneResizable()
	c.active = v
	return c
}

// cloneResizable copies the receiver, or the defaults when it is nil, so a
// With* call on a nil style yields a complete value rather than empty looks.
func (s *ResizableStyle) cloneResizable() *ResizableStyle {
	if s == nil {
		return DefaultResizableStyle()
	}
	c := *s
	return &c
}
