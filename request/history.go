package request

import (
	"bytes"
	"io"
	"time"
)

// maxHistoryBodySize is the maximum size of a request body stored in history.
// Larger bodies (e.g. file uploads) are truncated to avoid excessive memory use.
const maxHistoryBodySize = 64 << 10 // 64 KB

// HistoryEntry represents a single HTTP request and response pair with metadata
// such as method, URL, headers, and timestamps.
type HistoryEntry struct {
	method    string
	url       string
	headers   map[string]string
	timestamp time.Time
	req       HistoryRequest
	res       HistoryResponse
}

// HistoryRequest represents an HTTP request with its body content and size.
type HistoryRequest struct {
	size      int64
	body      []byte
	truncated bool
}

// HistoryResponse represents the outcome of an HTTP request, including the
// status code and response body as a byte slice.
type HistoryResponse struct {
	status int
	body   []byte
}

// Histories stores a limited history of HTTP requests as HistoryEntry objects.
// It maintains a maximum number of entries specified by limit, automatically
// removing the oldest entries when full.
type Histories struct {
	limit   uint64
	entries []*HistoryEntry
}

// NewHistories initializes a new Histories instance with a specified size or
// a default size of 3 if size is 0.
func NewHistories(size uint64) *Histories {
	if size == 0 {
		size = 3
	}
	return &Histories{
		entries: make([]*HistoryEntry, 0, size),
		limit:   size,
	}
}

// getRequestBody retrieves the request body as a byte slice from the provided
// Params object, or returns nil if unavailable.
func (h *Histories) getRequestBody(p *Params) ([]byte, bool) {
	if p == nil || p.Request == nil || p.Request.Body == nil {
		return nil, false
	}

	var data []byte
	var err error

	if p.Request.GetBody != nil {
		rc, e := p.Request.GetBody()
		if e != nil {
			return nil, false
		}
		defer rc.Close()
		data, err = io.ReadAll(io.LimitReader(rc, maxHistoryBodySize+1))
	} else {
		data, err = io.ReadAll(io.LimitReader(p.Request.Body, maxHistoryBodySize+1))
		if err == nil {
			// Restore body so it can still be read downstream.
			p.Request.Body = io.NopCloser(bytes.NewReader(data))
		}
	}
	if err != nil {
		return nil, false
	}

	if int64(len(data)) > maxHistoryBodySize {
		return data[:maxHistoryBodySize], true
	}
	return data, false
}

// copyHeaders returns a shallow copy of the headers map so that the history
// entry is isolated from later mutations by the caller.
func copyHeaders(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// Add captures a new HistoryEntry to the history, removing the oldest entry
// if the limit is exceeded.
func (h *Histories) Add(p *Params) {
	body, truncated := h.getRequestBody(p)
	entry := &HistoryEntry{
		method:    p.Method,
		url:       p.Url,
		headers:   copyHeaders(p.Headers),
		timestamp: time.Now(),
		req: HistoryRequest{
			size:      p.Request.ContentLength,
			body:      body,
			truncated: truncated,
		},
		res: HistoryResponse{
			status: p.Response.StatusCode,
			body:   []byte(p.ResponseBody),
		},
	}
	h.entries = append(h.entries, entry)
	if len(h.entries) > int(h.limit) {
		h.entries = h.entries[1:]
	}
}

// GetHistory retrieves the HistoryEntry at the specified reverse position
// prevPos from the history entries. Returns nil if prevPos is out of range.
func (h *Histories) GetHistory(prevPos int) *HistoryEntry {
	pos := len(h.entries) - prevPos
	if pos < 0 {
		pos = 0
	}
	if pos >= len(h.entries) {
		pos = len(h.entries) - 1
	}
	return h.entries[pos]
}
