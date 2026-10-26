package embed

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// PullProgress is where an Ollama pull is, summed over the model's layers as its stream announces
// them.
type PullProgress struct {
	// Status is the stream's latest: "pulling manifest", "pulling <digest>", "verifying sha256
	// digest", "writing manifest", "success".
	Status string
	// Completed and Total are bytes over every layer seen so far. Total grows as layers are
	// announced, so a fraction of them can fall back.
	Completed, Total int64
}

// ErrPullIncomplete is a pull whose stream ended without saying success: the server went away, or
// the connection was cut.
var ErrPullIncomplete = errors.New("embed: the pull ended before the server said success")

// maxPullLine bounds one line of the pull stream; a line longer than it is an error.
const maxPullLine = 64 << 10

// Pull has the Ollama server at base pull model name, calling progress (if not nil) for each line
// of its stream, and returns when the stream says success, or with the stream's error line, an
// error status, or ErrPullIncomplete. Cancelling ctx closes the stream: the server stops the
// download unless another client is pulling the same model, and a later pull resumes it from
// where it stopped (Ollama 0.40.11, measured). client must have no overall timeout, since a pull
// runs for minutes; nil is http.DefaultClient.
func Pull(ctx context.Context, base, key, name string, client *http.Client, progress func(PullProgress)) error {
	if client == nil {
		client = http.DefaultClient
	}
	url := strings.TrimRight(base, "/") + "/api/pull"
	resp, err := send(ctx, client, http.MethodPost, url, bearer(key), map[string]any{"model": name, "stream": true})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	totals, done := map[string]int64{}, map[string]int64{}
	var p PullProgress
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 4096), maxPullLine)
	for sc.Scan() {
		var line struct {
			Status    string `json:"status"`
			Digest    string `json:"digest"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
			Error     string `json:"error"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			return fmt.Errorf("embed: POST %s: reading the stream: %w", url, err)
		}
		if line.Error != "" {
			return fmt.Errorf("embed: pulling %s: %s", name, line.Error)
		}
		if line.Digest != "" {
			totals[line.Digest], done[line.Digest] = line.Total, line.Completed
			p.Total, p.Completed = 0, 0
			for d, t := range totals {
				p.Total += t
				p.Completed += done[d]
			}
		}
		p.Status = line.Status
		if progress != nil {
			progress(p)
		}
		if line.Status == "success" {
			return nil
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("embed: pulling %s: %w", name, ctx.Err())
		}
		return fmt.Errorf("%w: %s: %w", ErrPullIncomplete, name, err)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("embed: pulling %s: %w", name, ctx.Err())
	}
	return fmt.Errorf("%w: %s", ErrPullIncomplete, name)
}
