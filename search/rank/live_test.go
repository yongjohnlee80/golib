package rank

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestALiveTEIReranker scores against a real TEI re-ranker where TEST_TEI_RERANK_URL names one: it
// reports a re-ranker model, and ranks the passage that answers the query above one that does not.
func TestALiveTEIReranker(t *testing.T) {
	url := os.Getenv("TEST_TEI_RERANK_URL")
	if url == "" {
		t.Skip("TEST_TEI_RERANK_URL not set; skipping the live TEI re-ranker cell")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	r, err := NewTEI(ctx, url, os.Getenv("TEST_TEI_RERANK_KEY"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Model().Name == "" || r.Model().MaxBatch <= 0 {
		t.Fatalf("model %+v", r.Model())
	}
	scores, err := r.Rank(ctx, "where is the restart handoff written",
		[]string{"a recipe for banana bread", "the requester writes the restart handoff before it stops the daemon"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) != 2 || scores[1] <= scores[0] {
		t.Fatalf("scores %v: the answering passage is not ranked first", scores)
	}
}
