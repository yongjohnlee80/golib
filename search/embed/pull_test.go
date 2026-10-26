package embed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// pullServer answers /api/pull with lines, one written and flushed at a time; hold, when set, is
// waited on after the lines (a stream still open), and closed is closed when the request ends.
func pullServer(t *testing.T, status int, lines []string, hold <-chan struct{}) (url string, closed <-chan struct{}) {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		if r.URL.Path != "/api/pull" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		for _, l := range lines {
			fmt.Fprintln(w, l)
			w.(http.Flusher).Flush()
		}
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, done
}

// A pull's progress is summed over its layers as they are announced, and it returns at success.
func TestPullSumsItsLayersAndEndsAtSuccess(t *testing.T) {
	url, _ := pullServer(t, http.StatusOK, []string{
		`{"status":"pulling manifest"}`,
		`{"status":"pulling a","digest":"sha256:a","total":100,"completed":10}`,
		`{"status":"pulling a","digest":"sha256:a","total":100,"completed":100}`,
		`{"status":"pulling b","digest":"sha256:b","total":50,"completed":20}`,
		`{"status":"verifying sha256 digest"}`,
		`{"status":"success"}`,
	}, nil)
	var seen []PullProgress
	if err := Pull(context.Background(), url, "", "m", nil, func(p PullProgress) { seen = append(seen, p) }); err != nil {
		t.Fatal(err)
	}
	want := []PullProgress{{"pulling manifest", 0, 0}, {"pulling a", 10, 100}, {"pulling a", 100, 100},
		{"pulling b", 120, 150}, {"verifying sha256 digest", 120, 150}, {"success", 120, 150}}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Fatalf("progress %v\nwant %v", seen, want)
	}
}

// An error line, an error status, and a stream that ends before success are each the pull's error.
func TestPullFailsOnAnErrorLineAStatusOrAnEarlyEnd(t *testing.T) {
	url, _ := pullServer(t, http.StatusOK, []string{`{"status":"pulling manifest"}`, `{"error":"pull model manifest: file does not exist"}`}, nil)
	if err := Pull(context.Background(), url, "", "nope", nil, nil); err == nil || !strings.Contains(err.Error(), "file does not exist") {
		t.Fatalf("an error line: %v", err)
	}
	url, _ = pullServer(t, http.StatusUnauthorized, []string{`{"error":"no"}`}, nil)
	if err := Pull(context.Background(), url, "k", "m", nil, nil); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("a 401: %v", err)
	}
	url, _ = pullServer(t, http.StatusOK, []string{`{"status":"pulling a","digest":"sha256:a","total":100,"completed":10}`}, nil)
	if err := Pull(context.Background(), url, "", "m", nil, nil); !errors.Is(err, ErrPullIncomplete) {
		t.Fatalf("EOF without success: %v", err)
	}
}

// Cancelling the context closes the request: the server sees it end, and Pull says so.
func TestPullCancelledClosesTheRequest(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)
	url, closed := pullServer(t, http.StatusOK, []string{`{"status":"pulling a","digest":"sha256:a","total":100,"completed":10}`}, hold)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- Pull(ctx, url, "", "m", nil, func(p PullProgress) {
			if p.Completed == 10 {
				cancel() // mid-stream, as a dialog's Cancel
			}
		})
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("a cancelled pull: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Pull did not return after its context was cancelled")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the server's request did not end: the stream was left open")
	}
}
