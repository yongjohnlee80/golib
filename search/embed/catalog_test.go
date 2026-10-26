package embed

import "testing"

const gib = int64(1) << 30

// card is a card resident at 1 GiB at 2048 tokens and 2 GiB at 8192.
var card = Card{Provider: "ollama", Name: "m", Resident: []Sample{{2048, gib}, {8192, 2 * gib}}}

// Memory is linear between samples, exact at them, and unsaid outside them or with too few.
func TestMemoryIsSaidOnlyWithinItsSamples(t *testing.T) {
	for _, c := range []struct {
		context int
		want    int64
		ok      bool
	}{
		{2048, gib, true}, {8192, 2 * gib, true}, {5120, gib + gib/2, true},
		{1024, 0, false}, {16384, 0, false},
	} {
		if got, ok := card.Memory(c.context); got != c.want || ok != c.ok {
			t.Errorf("Memory(%d) = %d, %v; want %d, %v", c.context, got, ok, c.want, c.ok)
		}
	}
	if _, ok := (Card{Resident: []Sample{{8192, gib}}}).Memory(8192); ok {
		t.Error("one sample: a memory said")
	}
}

// Fits gives the GPU only with GPUMargin to spare, falls back to the RAM, and says unknown, never
// none, when it can't tell.
func TestFitsAtEachBoundary(t *testing.T) {
	need := 2 * gib                              // at 8192
	gpu := need + int64(float64(need)*GPUMargin) // the least VRAM that fits
	for _, c := range []struct {
		name string
		b    Budget
		ctx  int
		want Fit
	}{
		{"VRAM with the margin", Budget{VRAM: gpu}, 8192, FitGPU},
		{"VRAM a byte short of the margin, RAM enough", Budget{VRAM: gpu - 1, RAM: need}, 8192, FitCPU},
		{"no VRAM known, RAM enough", Budget{RAM: need}, 8192, FitCPU},
		{"no VRAM known, RAM a byte short", Budget{RAM: need - 1}, 8192, FitNone},
		{"VRAM too small, RAM unknown", Budget{VRAM: gpu - 1}, 8192, FitUnknown},
		{"nothing known", Budget{}, 8192, FitUnknown},
		{"a context outside the samples", Budget{VRAM: 100 * gib, RAM: 100 * gib}, 32768, FitUnknown},
	} {
		if got := card.Fits(c.b, c.ctx); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// ParseFingerprint reads back what Fingerprint writes, and refuses any other shape.
func TestParseFingerprintReadsFingerprints(t *testing.T) {
	m := Model{Provider: "ollama", Name: "qwen3-embedding:0.6b", Digest: "sha256:abc", Dims: 1024}
	if got, ok := ParseFingerprint(m.Fingerprint()); !ok || got != m {
		t.Fatalf("round trip: %+v %v", got, ok)
	}
	if got, ok := ParseFingerprint("openai|text-embedding-3-small||1536"); !ok || got.Digest != "" || got.Dims != 1536 {
		t.Fatalf("no digest: %+v %v", got, ok)
	}
	for _, bad := range []string{"", "a|b|c", "a|b|c|d|e", "|b|c|1", "a||c|1", "a|b|c|x", "a|b|c|-1"} {
		if _, ok := ParseFingerprint(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

// Every shipped card says where its numbers came from, and its numbers hold together.
func TestEveryCardIsMeasured(t *testing.T) {
	for _, c := range Catalog() {
		if c.Source.Digest == "" || c.Source.Ollama == "" || c.Source.Device == "" || c.Source.Measured == "" {
			t.Errorf("%s: an unsourced card: %+v", c.Name, c.Source)
		}
		if c.Dims <= 0 || c.Context <= 0 || c.Download <= 0 || len(c.Resident) < 2 {
			t.Errorf("%s: a card missing a fact: %+v", c.Name, c)
		}
		for i := 1; i < len(c.Resident); i++ {
			if c.Resident[i].Context <= c.Resident[i-1].Context || c.Resident[i].Bytes < c.Resident[i-1].Bytes {
				t.Errorf("%s: samples out of order: %v", c.Name, c.Resident)
			}
		}
	}
	if n := len(Catalog()); n != 5 {
		t.Errorf("%d cards; the ADR's five", n)
	}
}
