package embed

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestModelName(t *testing.T) {
	if got := ModelName(Model{Provider: "ollama", Name: "bge-m3", Digest: "sha256:x", Dims: 1024}.Fingerprint()); got != "bge-m3" {
		t.Errorf("ModelName = %q", got)
	}
	if got := ModelName("bare"); got != "bare" {
		t.Errorf("a name with no fingerprint: %q", got)
	}
}

// TestOllamaUnloadAndShares: Unload is an embed with no input; two clients share a model when the
// server and the model are the same.
func TestOllamaUnloadAndShares(t *testing.T) {
	s := &server{dims: 4}
	ts := httptest.NewServer(s.handler())
	defer ts.Close()
	ctx := context.Background()
	a, err := NewOllama(ctx, ts.URL, "", "embedder", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewOllama(ctx, ts.URL+"/", "", "embedder", nil)
	other, _ := NewOllama(ctx, ts.URL, "", "other", nil)
	oa, err := NewOpenAI(ctx, ts.URL, "", "embedder", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Name() != "ollama" || oa.Name() != "openai" {
		t.Errorf("names: %q, %q", a.Name(), oa.Name())
	}
	if !a.Shares(b) || a.Shares(other) || a.Shares(oa) {
		t.Errorf("Shares: same %v, another model %v, another kind %v", a.Shares(b), a.Shares(other), a.Shares(oa))
	}
	before := len(s.inputs)
	if err := a.Unload(ctx); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.inputs) != before+1 || len(s.inputs[before]) != 0 {
		t.Errorf("Unload sent %v", s.inputs[before:])
	}
}
