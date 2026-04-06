package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	TypeJSONUTF8 = "application/json; charset=UTF-8"
	TypeJSON     = "application/json"

	// MaxResponseBodySize is the default maximum number of bytes read from a
	// response body (10 MB). Override per-Params with Params.MaxResponseSize.
	MaxResponseBodySize int64 = 10 << 20
)

// Params holds the configuration and state for an HTTP request/response cycle.
type Params struct {
	ResponseBody string
	Headers      map[string]string
	Cookies      []*http.Cookie
	Method       string
	Password     string
	Request      *http.Request
	Response     *http.Response
	Timeout      time.Duration
	Url          string
	Username     string

	// MaxResponseSize overrides the default response body size limit.
	// Set to 0 to use MaxResponseBodySize. Set to -1 for unlimited.
	MaxResponseSize int64
}

// defaultTransport is a shared transport that enables connection pooling
// across requests.
var defaultTransport = &http.Transport{}

// CustomPayload allows custom payload types that provide their own content type.
type CustomPayload interface {
	io.Reader
	ContentType() string
}

// Request executes an HTTP request. It populates params.Request, params.Response,
// and params.ResponseBody. Only transport-level errors are returned; HTTP status
// codes are not treated as errors — use DecodeResponse to handle those.
func Request(params *Params, payload any, opts ...RequestOption) error {
	for _, opt := range opts {
		params = opt(params)
	}
	var _payload io.Reader
	var contentType string
	var contentLength int
	if payload != nil {
		switch v := payload.(type) {
		case CustomPayload:
			_payload = v
			contentType = v.ContentType()
		case io.Reader:
			_payload = v
		case url.Values:
			encoded := v.Encode()
			_payload = strings.NewReader(encoded)
			contentType = "application/x-www-form-urlencoded"
			contentLength = len(encoded)
		default:
			data, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			_payload = bytes.NewReader(data)
			contentType = TypeJSONUTF8
			contentLength = len(data)
		}
	}

	if params.Timeout == 0 {
		params.Timeout = 10
	}

	client := &http.Client{
		Timeout:   params.Timeout * time.Second,
		Transport: defaultTransport,
	}
	req, err := http.NewRequest(params.Method, params.Url, _payload)
	if err != nil {
		return err
	}
	for header, value := range params.Headers {
		req.Header.Add(header, value)
	}
	for _, c := range params.Cookies {
		req.AddCookie(c)
	}
	if contentType != "" {
		req.Header.Add("Content-Type", contentType)
		req.Header.Add("Content-Length", strconv.Itoa(contentLength))
	}
	if params.Username != "" || params.Password != "" {
		req.SetBasicAuth(params.Username, params.Password)
	}

	res, err := client.Do(req)
	if err != nil {
		return err
	}
	params.Request = req
	params.Response = res
	defer res.Body.Close()

	limit := params.MaxResponseSize
	if limit == 0 {
		limit = MaxResponseBodySize
	}
	var reader io.Reader = res.Body
	if limit > 0 {
		reader = io.LimitReader(res.Body, limit+1)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if limit > 0 && int64(len(data)) > limit {
		return fmt.Errorf("response body exceeds maximum size of %d bytes", limit)
	}
	params.ResponseBody = string(data)
	return nil
}

// StatusCodeInBounds returns true if the HTTP status code is in the 2xx-3xx range.
func StatusCodeInBounds(code int) bool {
	return code >= 200 && code < 400
}

// Get performs a GET request and decodes the response using the default Error type.
func Get(uri string, params map[string][]string, response any) (*http.Response, error) {
	if uri[len(uri)-1] == '?' {
		uri = uri[0 : len(uri)-1]
	}
	if params != nil {
		uri += "?" + url.Values(params).Encode()
	}
	p := &Params{
		Timeout: 100000,
		Method:  "GET",
		Url:     uri,
	}
	if err := Request(p, nil); err != nil {
		return nil, err
	}
	return p.Response, DecodeResponse[Error](p, response)
}

// Post performs a POST request and decodes the response using the default Error type.
func Post(uri string, payload, response any) (*http.Response, error) {
	p := &Params{
		Timeout: 100000,
		Method:  "POST",
		Url:     uri,
	}
	if err := Request(p, payload); err != nil {
		return nil, err
	}
	return p.Response, DecodeResponse[Error](p, response)
}
