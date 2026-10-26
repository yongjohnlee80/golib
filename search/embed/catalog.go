package embed

import (
	"sort"
	"strconv"
	"strings"
)

// Card is what is known of an embedding model before any server has it: facts measured once, at
// one tag and digest, never a promise about another.
type Card struct {
	Provider, Name string   // "ollama", "qwen3-embedding:0.6b"
	Dims, Context  int      // the vector size; the model's maximum context, in tokens
	Download       int64    // bytes: the sum of the registry manifest's layers
	Resident       []Sample // the resident size measured at two contexts or more, by context
	Multilingual   bool
	Source         Source
}

// Sample is one measurement: the model's resident bytes (Ollama's /api/ps size) when loaded at
// Context tokens.
type Sample struct {
	Context int
	Bytes   int64
}

// Source is where a card's numbers came from: the tag's digest, the Ollama that served it, the
// device its samples were taken on ("cpu", or a GPU's name), and when.
type Source struct{ Digest, Ollama, Device, Measured string }

// Memory is the card's resident size at context tokens, linear between the two nearest samples
// (the KV cache grows with the context); ok is false outside the measured range, where it is not
// said, and for a card with fewer than two samples.
func (c Card) Memory(context int) (bytes int64, ok bool) {
	s := c.Resident
	if len(s) < 2 || context < s[0].Context || context > s[len(s)-1].Context {
		return 0, false
	}
	i := sort.Search(len(s), func(i int) bool { return s[i].Context >= context })
	if s[i].Context == context {
		return s[i].Bytes, true
	}
	a, b := s[i-1], s[i]
	return a.Bytes + (b.Bytes-a.Bytes)*int64(context-a.Context)/int64(b.Context-a.Context), true
}

// Budget is the memory a model may take: a GPU's VRAM, or RAM for the CPU (and unified memory).
// 0 is unknown.
type Budget struct{ VRAM, RAM int64 }

// Fit is where a card runs within a budget.
type Fit string

const (
	FitGPU     Fit = "gpu"     // within the VRAM, after GPUMargin
	FitCPU     Fit = "cpu"     // not the GPU (none, too small, or unknown), within the RAM
	FitNone    Fit = "none"    // too large for both, as far as the budget is known
	FitUnknown Fit = "unknown" // no budget is known, or Memory has no answer at the context
)

// GPUMargin is the share of a card's measured memory added before it is said to fit a GPU. The
// cards are measured on a CPU; on a GPU, CUDA's buffers and compute graph take more than the
// weights and KV cache alone, so a model at the edge is not promised a GPU it won't fit.
const GPUMargin = 0.25

// Fits reports where c runs at context within b. An unknown VRAM never fits the GPU; it falls back
// to the RAM, and with no RAM known either the answer is FitUnknown, never FitNone.
func (c Card) Fits(b Budget, context int) Fit {
	m, ok := c.Memory(context)
	if !ok || (b.VRAM <= 0 && b.RAM <= 0) {
		return FitUnknown
	}
	if b.VRAM > 0 && m+int64(float64(m)*GPUMargin) <= b.VRAM {
		return FitGPU
	}
	if b.RAM <= 0 {
		return FitUnknown
	}
	if m <= b.RAM {
		return FitCPU
	}
	return FitNone
}

// ParseFingerprint is a fingerprint's model (provider|name|digest|dims, Model.Fingerprint's
// form); ok is false for any other.
func ParseFingerprint(fp string) (Model, bool) {
	parts := strings.Split(fp, "|")
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" {
		return Model{}, false
	}
	dims, err := strconv.Atoi(parts[3])
	if err != nil || dims < 0 {
		return Model{}, false
	}
	return Model{Provider: parts[0], Name: parts[1], Digest: parts[2], Dims: dims}, true
}

// Catalog is the cards golib ships, in no order: the facts, not a recommendation.
func Catalog() []Card {
	out := make([]Card, len(catalog))
	for i, c := range catalog {
		c.Resident = append([]Sample(nil), c.Resident...)
		out[i] = c
	}
	return out
}

// catalog is filled from measurements only (catalog_cards.go); see each card's Source.
var catalog = cards
