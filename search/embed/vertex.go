package embed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yongjohnlee80/golib/search/internal/gcp"
)

// TokenFunc answers an OAuth access token for Google's APIs: the caller's, so golib takes no Google
// dependency (a server wraps golang.org/x/oauth2/google's default credentials, say). refresh is
// true after the API refused the last token (HTTP 401): the answer must then not be a cached
// token. Its error is passed on wrapped, as it gave it; a token is never in an error.
type TokenFunc func(ctx context.Context, refresh bool) (string, error)

// VertexConfig is a Vertex AI embedding model's place.
type VertexConfig struct {
	// Endpoint is the API's base URL; "" is Location's host (VertexHost).
	Endpoint string
	// Project is the Google Cloud project the calls are made in and billed to. Required.
	Project string
	// Location is where the model is served: "global", "us" or "eu" (gemini-embedding-2 is
	// served at no single region).
	Location string
	// Model is the model's name: VertexModel, the one model this client speaks.
	Model string
	// Dimensions is the vectors' size, VertexMinDims to VertexMaxDims; 0 is the model's own,
	// VertexMaxDims. Google normalizes a vector it reduces.
	Dimensions int
}

// The Vertex AI embedding model this client speaks, its sizes, and the version of the prompt
// formats it embeds texts in (a Model's Digest: a change of format is a change of model).
const (
	VertexModel   = "gemini-embedding-2"
	VertexMinDims = 128
	VertexMaxDims = 3072
	VertexFormat  = "doc-v1"

	// VertexMaxTokens is the most of a text the model reads; the API truncates a longer one,
	// which Embed refuses.
	VertexMaxTokens = 8192
)

// The prompt formats Google gives for gemini-embedding-2's retrieval: a query as a search's, a
// document with its title, here none (a text is embedded once whatever documents share it).
const (
	vertexQuery    = "task: search result | query: "
	vertexDocument = "title: none | text: "
)

// ErrAPIDisabled is Google refusing a call because the API is not enabled in the project (a 403
// whose details say SERVICE_DISABLED). It is an ErrUnauthorized too.
var ErrAPIDisabled = fmt.Errorf("%w: the API is not enabled in the project", ErrUnauthorized)

// VertexHost is the base URL a location's calls go to: Google's global host for "global", the
// multi-region's for "us" and "eu"; "" for any other location.
func VertexHost(location string) string {
	switch location {
	case "global":
		return "https://aiplatform.googleapis.com"
	case "us", "eu":
		return "https://aiplatform." + location + ".rep.googleapis.com"
	}
	return ""
}

// Vertex embeds with gemini-embedding-2 on Vertex AI, through its :embedContent method: one text a
// call, since the method takes one content and several parts make one joint vector. A query (Query,
// EmbedQuery) and a document (Embed) are each sent in Google's prompt format for its side.
type Vertex struct {
	meter
	url, project string
	dims         int // sent as outputDimensionality; 0 sends none
	tokens       TokenFunc
	client       *http.Client
	model        Model
}

// NewVertex returns the client of cfg's model, asking tokens for each call's token. It embeds one
// probe text, so a project, location, model or credentials the API refuses fail here rather than
// at the first document. A model other than VertexModel is ErrNoModel: other Vertex models take
// other requests.
func NewVertex(ctx context.Context, cfg VertexConfig, tokens TokenFunc, client *http.Client) (*Vertex, error) {
	if client == nil {
		client = http.DefaultClient
	}
	switch {
	case cfg.Model != VertexModel:
		return nil, fmt.Errorf("%w: vertex: %q (this client speaks %s only)", ErrNoModel, cfg.Model, VertexModel)
	case cfg.Project == "":
		return nil, errors.New("embed: vertex: no project")
	case VertexHost(cfg.Location) == "":
		return nil, fmt.Errorf("embed: vertex: location %q: %s is served at global, us or eu", cfg.Location, VertexModel)
	case cfg.Dimensions != 0 && (cfg.Dimensions < VertexMinDims || cfg.Dimensions > VertexMaxDims):
		return nil, fmt.Errorf("embed: vertex: %d dimensions: %s answers %d to %d", cfg.Dimensions, VertexModel, VertexMinDims, VertexMaxDims)
	case tokens == nil:
		return nil, errors.New("embed: vertex: no token function")
	}
	base := cfg.Endpoint
	if base == "" {
		base = VertexHost(cfg.Location)
	}
	dims := cfg.Dimensions
	if dims == 0 {
		dims = VertexMaxDims
	}
	v := &Vertex{
		url: strings.TrimRight(base, "/") + "/v1/projects/" + url.PathEscape(cfg.Project) + "/locations/" + cfg.Location +
			"/publishers/google/models/" + cfg.Model + ":embedContent",
		project: cfg.Project, dims: cfg.Dimensions, tokens: tokens, client: client,
		model: Model{Provider: "vertex", Name: cfg.Model, Digest: VertexFormat, Dims: dims},
	}
	if _, err := probe(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *Vertex) Name() string { return "vertex" }
func (v *Vertex) Model() Model { return v.model }

// Embed embeds documents' texts, one call each, in order; the first failure fails them all.
// A text the API had to truncate is ErrRejected, never a vector of part of it.
func (v *Vertex) Embed(ctx context.Context, texts []string) (vecs [][]float32, err error) {
	start := time.Now()
	tokens := 0
	defer func() { v.report(len(texts), tokens, err, start) }()
	vecs = make([][]float32, len(texts))
	for i, t := range texts {
		vec, n, err := v.one(ctx, vertexDocument+t)
		tokens += n
		if err != nil {
			return nil, err
		}
		vecs[i] = vec
	}
	return checked(vecs, len(texts), v.model.Dims)
}

// EmbedQuery embeds a search's query, in Google's format for a retrieval query.
func (v *Vertex) EmbedQuery(ctx context.Context, q string) (vec []float32, err error) {
	start := time.Now()
	tokens := 0
	defer func() { v.report(1, tokens, err, start) }()
	vec, tokens, err = v.one(ctx, vertexQuery+q)
	if err != nil {
		return nil, err
	}
	if _, err := checked([][]float32{vec}, 1, v.model.Dims); err != nil {
		return nil, err
	}
	return vec, nil
}

// one is one :embedContent call: the vector of text, and the input tokens Google counted.
func (v *Vertex) one(ctx context.Context, text string) ([]float32, int, error) {
	req := map[string]any{"content": map[string]any{"parts": []map[string]any{{"text": text}}}}
	if v.dims != 0 {
		req["embedContentConfig"] = map[string]any{"outputDimensionality": v.dims}
	}
	var out struct {
		Embedding struct {
			Values []float32 `json:"values"`
		} `json:"embedding"`
		Usage struct {
			Prompt int `json:"promptTokenCount"`
		} `json:"usageMetadata"`
		Truncated bool `json:"truncated"`
	}
	resp, err := v.post(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	if err := decode(http.MethodPost, v.url, resp, &out); err != nil {
		return nil, 0, err
	}
	if out.Truncated {
		return nil, out.Usage.Prompt, fmt.Errorf("%w: vertex: the text is over the model's %d tokens, and was truncated", ErrRejected, VertexMaxTokens)
	}
	return out.Embedding.Values, out.Usage.Prompt, nil
}

// post sends one request with a token, and asks a fresh token and sends it once more when the API
// refuses the first (HTTP 401: a token that expired between the asking and the call). It answers
// the response of a 200; any other status is an error, typed as send's are, a 404 as ErrNoModel
// and a disabled API as ErrAPIDisabled.
func (v *Vertex) post(ctx context.Context, in any) (*http.Response, error) {
	body, err := marshal(in)
	if err != nil {
		return nil, err
	}
	var resp *http.Response
	for _, refresh := range [...]bool{false, true} {
		header, err := gcp.Header(ctx, gcp.Tokens(v.tokens), refresh, v.project)
		if err != nil {
			return nil, fmt.Errorf("embed: vertex: %w", err)
		}
		if resp, err = do(ctx, v.client, http.MethodPost, v.url, header, body); err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusUnauthorized || refresh {
			break
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxStatusBody))
		resp.Body.Close()
	}
	return status(http.MethodPost, v.url, resp, vertexStatus)
}

// vertexStatus types the errors Vertex AI's answers say more of than their status.
func vertexStatus(code int, body []byte, msg string) error {
	switch code {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNoModel, msg)
	case http.StatusForbidden:
		if service, consumer, ok := gcp.Disabled(body); ok {
			if consumer != "" {
				return fmt.Errorf("%w: %s in %s", ErrAPIDisabled, service, consumer)
			}
			return fmt.Errorf("%w: %s", ErrAPIDisabled, service)
		}
	}
	return nil
}

// Querier is a provider that embeds a search's query differently from a document: in a prompt
// format of the model's, say. Query uses it when a provider has it.
type Querier interface {
	EmbedQuery(ctx context.Context, q string) ([]float32, error)
}

// Query embeds a search's query with p: through its EmbedQuery when p is a Querier, otherwise as
// Embed embeds a document. Every caller embedding a query uses it, so a model whose queries need
// their own format gets it.
func Query(ctx context.Context, p Provider, q string) ([]float32, error) {
	if qp, ok := p.(Querier); ok {
		return qp.EmbedQuery(ctx, q)
	}
	vecs, err := p.Embed(ctx, []string{q})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("%w: %d vectors for one query", ErrDims, len(vecs))
	}
	return vecs[0], nil
}

var (
	_ Provider = (*Vertex)(nil)
	_ Querier  = (*Vertex)(nil)
	_ Metered  = (*Vertex)(nil)
)
