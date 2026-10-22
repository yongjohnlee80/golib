package decl

// modelSub is a view's subscription to its model, which ListView, ComboBox and TreeView share: the
// model shown, and how to stop hearing from it. swap drops the last model's subscription before
// taking the next; release drops it when the view goes. After either, the last model's changes
// reach the view no more.
type modelSub[M interface{ Subscribe(func(Change)) func() }] struct {
	model  M
	cancel func()
}

// swap shows m, hearing its changes through follow; a nil m is no model.
func (s *modelSub[M]) swap(m M, follow func(Change)) {
	dropSub(&s.cancel)
	s.model = m
	if any(m) != nil {
		s.cancel = m.Subscribe(follow)
	}
}

// release stops hearing from the model.
func (s *modelSub[M]) release() { dropSub(&s.cancel) }

// dropSub cancels a subscription, once. A free function, not a method swap and release share: a
// view embeds modelSub, and a method calling its sibling through the receiver would bypass a
// view's own (TestPromotionSelfCalls).
func dropSub(cancel *func()) {
	if *cancel != nil {
		(*cancel)()
		*cancel = nil
	}
}
