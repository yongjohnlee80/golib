package rank

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yongjohnlee80/golib/search/internal/gcp"
)

// TokenFunc answers an OAuth access token for Google's APIs: the caller's, so golib takes no Google
// dependency (a server wraps golang.org/x/oauth2/google's default credentials, say). refresh is
// true after the API refused the last token (HTTP 401): the answer must then not be a cached
// token. Its error is passed on wrapped, as it gave it; a token is never in an error.
type TokenFunc func(ctx context.Context, refresh bool) (string, error)

// VertexConfig is a Vertex AI Ranking API model's place. The API is served at the global location
// only, so there is no location to choose.
type VertexConfig struct {
	// Endpoint is the API's base URL; "" is VertexHost.
	Endpoint string
	// Project is the Google Cloud project the calls are made in and billed to. Required.
	Project string
	// Model is the ranking model, pinned: semantic-ranker-default-005, say. Required: the API's
	// own default (an @latest alias) would move under a running server.
	Model string
}

// VertexHost is the Ranking API's base URL.
const VertexHost = "https://discoveryengine.googleapis.com"

// DefaultVertexBatch is the most records a Vertex client sends in one request, unless told
// otherwise: one billed count is a query of up to 100 records (the API takes 1,000).
const DefaultVertexBatch = 100

// ErrAPIDisabled is Google refusing a call because the API is not enabled in the project (a 403
// whose details say SERVICE_DISABLED). It is an ErrUnauthorized too.
var ErrAPIDisabled = fmt.Errorf("%w: the API is not enabled in the project", ErrUnauthorized)

// Vertex ranks with the Vertex AI Ranking API (Discovery Engine's rankingConfigs.rank). Each text
// is a record's content, its id its place in the request; an empty text is no record (the API
// refuses one with neither title nor content) and scores 0, the bottom of the API's 0 to 1.
type Vertex struct {
	meter
	url, project string
	tokens       TokenFunc
	client       *http.Client
	timeout      time.Duration
	model        Model
}

// VertexOption configures a Vertex client.
type VertexOption func(*Vertex)

// WithVertexTimeout bounds each of the client's requests, its token's asking included
// (DefaultTimeout otherwise).
func WithVertexTimeout(d time.Duration) VertexOption {
	return func(v *Vertex) {
		if d > 0 {
			v.timeout = d
		}
	}
}

// WithVertexBatch sends at most n records in one request (DefaultVertexBatch otherwise).
func WithVertexBatch(n int) VertexOption {
	return func(v *Vertex) {
		if n > 0 {
			v.model.MaxBatch = n
		}
	}
}

// NewVertex returns the client of cfg's model, asking tokens for each call's token. It sends
// nothing: a probe is the caller's (Rank with two texts), and each is a billed count.
func NewVertex(cfg VertexConfig, tokens TokenFunc, client *http.Client, opts ...VertexOption) (*Vertex, error) {
	switch {
	case cfg.Project == "":
		return nil, errors.New("rank: vertex: no project")
	case cfg.Model == "":
		return nil, fmt.Errorf("%w: vertex: no model (pin one, semantic-ranker-default-005 say)", ErrNoModel)
	case tokens == nil:
		return nil, errors.New("rank: vertex: no token function")
	}
	if client == nil {
		client = http.DefaultClient
	}
	base := cfg.Endpoint
	if base == "" {
		base = VertexHost
	}
	v := &Vertex{
		url: strings.TrimRight(base, "/") + "/v1/projects/" + url.PathEscape(cfg.Project) +
			"/locations/global/rankingConfigs/default_ranking_config:rank",
		project: cfg.Project, tokens: tokens, client: client, timeout: DefaultTimeout,
		model: Model{Provider: "vertex", Name: cfg.Model, MaxBatch: DefaultVertexBatch},
	}
	for _, o := range opts {
		if o != nil {
			o(v)
		}
	}
	return v, nil
}

func (v *Vertex) Model() Model { return v.model }

func (v *Vertex) Rank(ctx context.Context, query string, texts []string) (scores []float64, err error) {
	start := time.Now()
	defer func() { v.report(len(texts), 0, err, start) }()
	a := auth{retries: true, header: func(ctx context.Context, refresh bool) (http.Header, error) {
		return gcp.Header(ctx, gcp.Tokens(v.tokens), refresh, v.project)
	}}
	scores, _, err = batched(ctx, texts, v.model.MaxBatch, func(ctx context.Context, batch []string) ([]indexed, int, error) {
		got := make([]indexed, 0, len(batch))
		records := make([]map[string]string, 0, len(batch))
		for i, t := range batch {
			if t == "" {
				zero := 0.0
				got = append(got, indexed{Index: &i, Score: &zero})
				continue
			}
			records = append(records, map[string]string{"id": strconv.Itoa(i), "content": t})
		}
		if len(records) == 0 {
			return got, 0, nil
		}
		var out struct {
			Records []struct {
				ID    string   `json:"id"`
				Score *float64 `json:"score"`
			} `json:"records"`
		}
		// no topN: every record is answered; no details: the answer is ids and scores
		req := map[string]any{"model": v.model.Name, "query": query, "records": records, "ignoreRecordDetailsInResponse": true}
		if err := exchange(ctx, v.client, v.timeout, http.MethodPost, v.url, a, vertexStatus, req, &out); err != nil {
			return nil, 0, err
		}
		for _, r := range out.Records {
			var at *int
			if i, err := strconv.Atoi(r.ID); err == nil && i >= 0 && i < len(batch) && batch[i] != "" {
				at = &i
			}
			got = append(got, indexed{Index: at, Score: r.Score})
		}
		return got, 0, nil
	})
	return scores, err
}

// vertexStatus types a disabled API's 403 from its details; the API's own text is never passed on,
// only the service and project names it gives, which are Google's identifiers.
func vertexStatus(code int, body []byte) error {
	if code != http.StatusForbidden {
		return nil
	}
	service, consumer, ok := gcp.Disabled(body)
	switch {
	case !ok:
		return nil
	case consumer != "":
		return fmt.Errorf("%w: %s in %s", ErrAPIDisabled, service, consumer)
	}
	return fmt.Errorf("%w: %s", ErrAPIDisabled, service)
}

var (
	_ Ranker  = (*Vertex)(nil)
	_ Metered = (*Vertex)(nil)
)
