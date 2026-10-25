package tui

// handlerPhase is which phase of the loop runs, and on whose behalf: the layout or render pass, the
// node whose handler, Init or Layout executes, the open action's provenance, and the Update depth.
// The runtime's guards read it to refuse a call made in the wrong phase or another node's name.
type handlerPhase struct {
	inLayout bool
	inRender bool

	// handlerNode is the node whose HandleEvent OR HandleAction is executing,
	// or 0 outside input delivery. Both phases mark it, because capture may be
	// taken from either and is granted to a specific node.
	//
	// It is an IDENTITY rather than a flag: with a bare "some handler is
	// running" boolean, node A's handler could call a retained Context
	// belonging to node B and take the pointer in B's name.
	handlerNode NodeID

	// actionHandlerNode is the node whose HandleAction is executing, or 0
	// outside an action delivery. SEPARATE from handlerNode on purpose:
	// handlerNode deliberately covers HandleEvent as well, because pointer
	// capture may be taken from either phase, and reusing it to gate action
	// forwarding let the RAW lane forward with provenance it does not own.
	//
	// Raw delivery clears it for its duration, so a raw handler running inside
	// an action delivery cannot borrow the invocation still open above it.
	actionHandlerNode NodeID

	// handlerOrigin and handlerSource are the provenance of the action
	// invocation currently being delivered, kept so Context.ForwardAction can
	// pass it to a child without the handler supplying it. Provenance a
	// consumer can state is provenance a consumer can forge, so the only way to
	// carry it is to read it back from the runtime that recorded it.
	//
	// Meaningful only while handlerNode names an action delivery; both are
	// saved and restored around every delivery so a nested one cannot leave the
	// outer invocation described by the wrong input.
	handlerOrigin ActionOrigin
	handlerSource Event

	// initNode is the node whose Init is running, or 0 outside mounting. Like
	// handlerNode it is an identity, because the setter it gates publishes a
	// specific component's own default bindings.
	initNode NodeID

	// updateDepth counts nested App.Update callbacks currently executing. The
	// phases in which a capture may be ENDED are stated positively — a handler
	// or an Update — because the forbidden list was incomplete twice over:
	// Init and any callback reached outside an Update both slipped through a
	// Layout/Render-only check.
	updateDepth int

	// inCommit marks the commit phase (commit.go): geometry final, before render. It may end a
	// capture, as a handler or an Update may: a geometry-derived side effect that retargets or
	// re-anchors a window ends the drag that holds the pointer. Taking a capture stays a handler's.
	inCommit bool
}

// delivery is the marks a handler's delivery sets: whose handler runs, whose action delivery it is
// (0 for a raw one), and the action's provenance.
type delivery struct {
	node, action NodeID
	origin       ActionOrigin
	source       Event
}

// enterDelivery marks d's delivery and returns the marks it replaced, for leaveDelivery to put back:
// a nested delivery then cannot leave the outer one described by the wrong node or input.
func (p *handlerPhase) enterDelivery(d delivery) (saved delivery) { return swapDelivery(p, d) }

// leaveDelivery puts back the marks enterDelivery replaced.
func (p *handlerPhase) leaveDelivery(saved delivery) { swapDelivery(p, saved) }

// swapDelivery sets p's delivery marks to d and returns the ones it replaced. A function, not a
// method, so the two methods above share it without calling a sibling on their receiver.
func swapDelivery(p *handlerPhase, d delivery) (old delivery) {
	old = delivery{p.handlerNode, p.actionHandlerNode, p.handlerOrigin, p.handlerSource}
	p.handlerNode, p.actionHandlerNode, p.handlerOrigin, p.handlerSource = d.node, d.action, d.origin, d.source
	return old
}
