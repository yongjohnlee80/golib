package main

import (
	"image"
	"testing"
)

// The Vulkan renderer draws what the CPU draws: the same scene, the right way up, in the right
// colours, read back from its own device. (Exporting it is the window test's, on a device that
// exports dma-bufs.)
func TestTheVulkanRendererDrawsWhatTheCPUDraws(t *testing.T) {
	p, err := openVulkan([16]byte{}, false)
	if err != nil {
		t.Skipf("no Vulkan device here: %v", err)
	}
	defer p.close()
	got, err := p.readback(0.3, image.Pt(testW, testH))
	if err != nil {
		t.Fatal(err)
	}
	want := cpuPicture(t, 0.3)
	savePNG(t, "vulkan-readback", got)
	diff, first := compare(want, got)
	t.Logf("%s: %.2f%% of pixels differ by more than the tolerance; first at %v", p.device, diff*100, first)
	if diff > 0.03 {
		t.Fatalf("the Vulkan picture differs from the CPU's in %.1f%% of its pixels (first at %v); want under 3%%", diff*100, first)
	}
}
