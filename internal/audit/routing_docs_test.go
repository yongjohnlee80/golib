package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The architectural documents drifted silently for three layers: the events
// tutorial still told readers that every key goes straight to HandleEvent and
// bubbles when that returns false, which stopped being true the moment resolvers
// landed. Nothing failed, because nothing checked. A reader following it wrote
// widgets against a routing model the runtime no longer had.
//
// This is deliberately a VOCABULARY check rather than a prose review. It cannot
// tell whether an explanation is good; it can tell whether a surface still
// mentions the stage it is required to describe, which is the failure that
// actually happened.
//
// BOUNDED PER-NODE EVENT ROUTING SEQUENCE:
//
//   Transport Lanes (Target Selection)
//   ├── Key / UserEvent    ──► Target = Confined focused node (within active trapping scope ceiling)
//   ├── CaptureRaw         ──► Target = Active capture node (direct dispatch; no bubbling)
//   ├── CaptureGesture     ──► Routes directly to active gesture recognizer
//   └── Pointer Event      ──► Target = Hit-test node; commits press ordinal before delivery
//
//   Bounded Walk: for n := target; n != nil; n = n.parent (up to confinement ceiling)
//   ┌────────────────────────────────────────────────────────────────────────────────────────┐
//   │ At each visited node n: routeToNode(n, ev)                                             │
//   │                                                                                        │
//   │ 1. Pointer Policy Gate:                                                                │
//   │    If pointer-derived and effectivePointerPolicy(n) == PointerDisabled, skip n         │
//   │                                                                                        │
//   │ 2. Action Resolution:                                                                  │
//   │    First match over consumer resolvers, then defaults: resolveFor(n, ev)               │
//   │                                                                                        │
//   │ 3. Semantic Action Dispatch:                                                           │
//   │    a. ActionHandler: If node implements ActionHandler, call HandleAction(inv)          │
//   │    b. Activatable Fallback: If unhandled AND action is concrete ActivateAction        │
//   │       (or *ActivateAction) AND node implements Activatable, call Activate(origin)      │
//   │       and publish ControlActivatedEvent.                                               │
//   │                                                                                        │
//   │ 4. Raw Delivery (Semantic comes first):                                                │
//   │    If no action resolved or action unhandled, deliver raw HandleEvent(local) to n      │
//   │                                                                                        │
//   │ 5. Walk Continuation:                                                                  │
//   │    If consumed (true): STOP walk.                                                      │
//   │    If unconsumed (false): bubble to n.parent (unless n == confinement ceiling)         │
//   └────────────────────────────────────────────────────────────────────────────────────────┘
//
//   Post-Walk Fallbacks (When Bounded Walk Completes Unconsumed):
//   ├── KeyEvent           ──► Global key bindings / App-level keymap fallback (App.globalKey)
//   ├── UserEvent          ──► Dropped when unconsumed by the bounded walk (no post-walk fallback)
//   └── Primary Press      ──► Eligible unconsumed primary press falls through to Gesture
//                              Recognizer (only if target is Activatable, availability is
//                              active, and pointer policy is not disabled)
//
// DOCUMENTATION SURFACES MONITORED:
//   - tui/doc.go
//   - tui/README.md
//   - tui/tutorial/04-events-focus-keys.md
//   - tui/widget/doc.go
//   - tui/widget/README.md

// docRequirement is one documentation surface and the terms it must carry.
type docRequirement struct {
	path string
	why  string
	must []string
}

var routingDocs = []docRequirement{
	{
		path: "tui/doc.go",
		why:  "the package overview is where the transport lanes are drawn, so it is where the interpretation stage after them belongs",
		must: []string{"HandleAction", "Activatable", "resolver", "gesture",
			"CaptureRaw", "CaptureGesture", "not a third transport lane"},
	},
	{
		path: "tui/README.md",
		why:  "the README carries the same architecture diagram as doc.go and must not contradict it",
		must: []string{"HandleAction", "Activatable", "resolver", "gesture",
			"CaptureRaw", "CaptureGesture", "not a third transport lane"},
	},
	{
		path: "tui/tutorial/04-events-focus-keys.md",
		why:  "this is the document that told readers raw HandleEvent came first",
		must: []string{"HandleAction", "Activatable", "resolver", "Pointer policy",
			"gesture recogniser", "CaptureRaw", "CaptureGesture"},
	},
	{
		path: "tui/widget/doc.go",
		why:  "the widget inventory must list Button and the capability it implements",
		must: []string{"Button", "tui.Activatable", "ActivationAvailability", "ControlActivatedEvent"},
	},
	{
		path: "tui/widget/README.md",
		why:  "the widget README carries the same inventory as doc.go",
		must: []string{"Button", "tui.Activatable", "ActivationAvailability", "ControlActivatedEvent"},
	},
}

// TestRoutingDocsDescribeTheCurrentModel fails when a required surface stops
// mentioning a stage of the routing model it is responsible for describing.
func TestRoutingDocsDescribeTheCurrentModel(t *testing.T) {
	root := repoRoot(t)
	for _, req := range routingDocs {
		full := filepath.Join(root, req.path)
		b, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("%s: cannot read a required documentation surface: %v", req.path, err)
			continue
		}
		// Case-insensitive: these are prose surfaces, and the documents
		// legitimately emphasise with capitals ("NOT a third transport lane").
		// A case-sensitive check would fail on a document that says the right
		// thing, which is worse than not checking.
		text := strings.ToLower(string(b))
		for _, term := range req.must {
			if !strings.Contains(text, strings.ToLower(term)) {
				t.Errorf("%s does not mention %q.\n  Why this surface must: %s\n"+
					"  Routing is resolver, then HandleAction, then raw HandleEvent, "+
					"bounded by the active scope, with an unconsumed primary press "+
					"falling through to the gesture recogniser.",
					req.path, term, req.why)
			}
		}
	}
}

// TestRoutingDocsDoNotClaimRawHandlerFirst catches the specific sentence that
// was wrong, in the specific place it was wrong, so the correction cannot be
// reverted by someone restoring the older and simpler-sounding explanation.
func TestRoutingDocsDoNotClaimRawHandlerFirst(t *testing.T) {
	root := repoRoot(t)
	banned := []string{
		"Every key event goes to the **focused** node first. If its `HandleEvent`",
	}
	for _, req := range routingDocs {
		b, err := os.ReadFile(filepath.Join(root, req.path))
		if err != nil {
			continue // absence is reported by the test above
		}
		for _, phrase := range banned {
			if strings.Contains(string(b), phrase) {
				t.Errorf("%s has reverted to the pre-resolver routing claim %q; the raw "+
					"handler runs after resolution, not before it", req.path, phrase)
			}
		}
	}
}
