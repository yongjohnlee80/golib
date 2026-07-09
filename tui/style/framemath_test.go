package style

import "testing"

// TestFrameMathBoxModel covers the package-side slice of ADR-0006 §5
// acceptance criterion 10: for styles with known padding/border/margin the
// frame-math getters agree with the box model (content → padding → border →
// margin, §2.3). The TestBackend render cross-check against ADR-0007 Box
// lands with the tui render layer; here the getters are pinned against
// hand-computed frame sizes.
func TestFrameMathBoxModel(t *testing.T) {
	tests := []struct {
		name             string
		st               Style
		hPad, vPad       int
		hBorder, vBorder int
		hFrame, vFrame   int
	}{
		{
			name: "empty",
			st:   New(),
		},
		{
			name: "padding only",
			st:   New().Padding(1, 2), // v=1 h=2
			hPad: 4, vPad: 2, hFrame: 4, vFrame: 2,
		},
		{
			name: "full frame",
			st:   New().Padding(1, 2).Margin(3).Border(BorderNormal),
			hPad: 4, vPad: 2,
			hBorder: 2, vBorder: 2,
			hFrame: 3 + 3 + 2 + 4, // margin l+r + border + padding
			vFrame: 3 + 3 + 2 + 2,
		},
		{
			name: "asymmetric sides",
			st:   New().Padding(1, 2, 3, 4).Margin(5, 6, 7, 8).Border(BorderDouble),
			hPad: 2 + 4, vPad: 1 + 3,
			hBorder: 2, vBorder: 2,
			hFrame: 6 + 8 + 2 + 6,
			vFrame: 5 + 7 + 2 + 4,
		},
		{
			name:    "partial edges", // 2-arg shorthand: top/bottom on, left/right off
			st:      New().Border(BorderNormal, true, false),
			vBorder: 2,
			vFrame:  2,
		},
		{
			name:    "hidden border still occupies its frame", // alignment stability
			st:      New().Border(BorderHidden),
			hBorder: 2, vBorder: 2, hFrame: 2, vFrame: 2,
		},
		{
			name:    "empty glyph contributes no size",
			st:      New().Border(BorderStyle{Left: "│", Right: "│"}), // no top/bottom glyphs
			hBorder: 2, hFrame: 2,
		},
		{
			name:   "margin without border or padding",
			st:     New().Margin(2, 1),
			hFrame: 2, vFrame: 4,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.st.GetHorizontalPadding(); got != tc.hPad {
				t.Errorf("GetHorizontalPadding() = %d, want %d", got, tc.hPad)
			}
			if got := tc.st.GetVerticalPadding(); got != tc.vPad {
				t.Errorf("GetVerticalPadding() = %d, want %d", got, tc.vPad)
			}
			if got := tc.st.GetHorizontalBorderSize(); got != tc.hBorder {
				t.Errorf("GetHorizontalBorderSize() = %d, want %d", got, tc.hBorder)
			}
			if got := tc.st.GetVerticalBorderSize(); got != tc.vBorder {
				t.Errorf("GetVerticalBorderSize() = %d, want %d", got, tc.vBorder)
			}
			if got := tc.st.GetHorizontalFrameSize(); got != tc.hFrame {
				t.Errorf("GetHorizontalFrameSize() = %d, want %d", got, tc.hFrame)
			}
			if got := tc.st.GetVerticalFrameSize(); got != tc.vFrame {
				t.Errorf("GetVerticalFrameSize() = %d, want %d", got, tc.vFrame)
			}
		})
	}
}

// TestFrameMathDecomposition: the frame getters are internally consistent —
// frame = margins + border + padding, side by side — so outer↔content rect
// conversion (ADR-0004/0007) can rely on either form.
func TestFrameMathDecomposition(t *testing.T) {
	st := New().Padding(1, 2, 3, 4).Margin(4, 3, 2, 1).Border(BorderThick, true, true, false, true)

	mTop, mRight, mBottom, mLeft := st.GetMargin()
	if got, want := st.GetHorizontalFrameSize(), mLeft+mRight+st.GetHorizontalBorderSize()+st.GetHorizontalPadding(); got != want {
		t.Errorf("horizontal frame %d != margins+border+padding %d", got, want)
	}
	if got, want := st.GetVerticalFrameSize(), mTop+mBottom+st.GetVerticalBorderSize()+st.GetVerticalPadding(); got != want {
		t.Errorf("vertical frame %d != margins+border+padding %d", got, want)
	}
}
