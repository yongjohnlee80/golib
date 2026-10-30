package request

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const (
	TypeJSONUTF8 = "application/json; charset=UTF-8"
	TypeJSON     = "application/json"

	// MaxResponseBodySize is the default maximum number of bytes read from a
	// response body (10 MB). Override per-Params with Params.MaxResponseSize.
	MaxResponseBodySize int64 = 10 << 20

	// DefaultTimeout is the request timeout applied when Params.Timeout is zero.
	DefaultTimeout = 10 * time.Second
)

// Params encapsulates both the input configuration and the execution state for an
// HTTP request/response cycle. Note that Params is mutated by [Request] and [Do] to
// store the resulting response state, and is therefore not safe for concurrent reuse.
type Params struct {
	// ResponseBody contains the response payload returned by the server, populated on success
	// when the body does not exceed the configured size limit. If the body exceeds the limit,
	// Do returns [ErrResponseTooLarge] without assigning ResponseBody.
	ResponseBody string
	// Headers holds request headers transmitted with the HTTP request.
	Headers map[string]string
	// Cookies contains HTTP cookies appended to the request.
	Cookies []*http.Cookie
	// Method is the HTTP verb (e.g. "GET", "POST", "PUT", "DELETE").
	Method string
	// Password is the optional password for HTTP Basic Authentication.
	Password string
	// Request is the underlying *http.Request created and executed by Do.
	Request *http.Request
	// Response is the *http.Response received from the server.
	Response *http.Response

	// Timeout bounds the entire request/response cycle including body reading.
	// Zero uses [DefaultTimeout]; negative duration disables client-side timeouts.
	Timeout time.Duration

	// Url is the destination HTTP/HTTPS endpoint URL.
	Url string
	// Username is the optional user identity for HTTP Basic Authentication.
	Username string

	// MaxResponseSize overrides the maximum allowed response body size in bytes.
	// 0 uses [MaxResponseBodySize] (10 MB); -1 allows unlimited reading.
	MaxResponseSize int64

	// DenyPrivate refuses to connect to a loopback, private, link-local, multicast or unspecified
	// address, checked on the address each connection actually dials (a redirect's included), so
	// a URL from someone else cannot reach this host's own network. No proxy is used with it: a
	// proxy would dial the target where the check cannot see it. See [DenyPrivateAddresses].
	DenyPrivate bool
}

// ErrResponseTooLarge reports a response body exceeding the configured
// MaxResponseSize / MaxResponseBodySize limit. Compare with errors.Is.
var ErrResponseTooLarge = errors.New("request: response body exceeds maximum size")

// defaultTransport is the shared, pooling transport: http.DefaultTransport's settings, so a dial,
// a TLS handshake and an idle connection are bounded, and HTTP(S)_PROXY from the environment is
// honoured.
var defaultTransport = http.DefaultTransport.(*http.Transport).Clone()

// guardedTransport is defaultTransport's, without a proxy, refusing a non-public address
// (Params.DenyPrivate).
var guardedTransport = func() *http.Transport {
	t := defaultTransport.Clone()
	t.Proxy = nil
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: refusePrivate}
	t.DialContext = d.DialContext
	return t
}()

// ErrPrivateAddress is a DenyPrivate request's refusal to connect to a non-public address.
var ErrPrivateAddress = errors.New("request: refused to connect to a non-public address")

// refusePrivate is the dial check of DenyPrivate: it sees the resolved address being connected to.
func refusePrivate(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrPrivateAddress, address)
	}
	if a := ap.Addr(); nonPublic(a) {
		return fmt.Errorf("%w: %s", ErrPrivateAddress, a.Unmap())
	}
	return nil
}

// deniedPrefixes are the non-public ranges netip's predicates do not name: carrier-grade NAT (a
// cloud's metadata service among them), "this network", the IETF and benchmarking blocks, the
// reserved 240/4 and broadcast, IPv4-compatible IPv6 and Teredo. NAT64 and 6to4 addresses carry an
// IPv4 one, checked in its own right (embeddedIPv4).
var deniedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("2001::/32"),
}

// nonPublic reports whether a is an address no request from a stranger's URL should reach: this
// host's or its network's (loopback, private, link-local, a denied range), a multicast or
// unspecified one, or one that carries such an IPv4 address.
func nonPublic(a netip.Addr) bool {
	a = a.Unmap()
	if a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() || a.IsUnspecified() {
		return true
	}
	for _, p := range deniedPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	if v4, ok := embeddedIPv4(a); ok {
		return nonPublic(v4)
	}
	return false
}

// The IPv6 prefixes that carry an IPv4 address: NAT64's well-known and local-use prefixes (RFC
// 6052, RFC 8215), dialled through a NAT64 gateway to that IPv4 address, and 6to4 (RFC 3056).
var (
	nat64      = netip.MustParsePrefix("64:ff9b::/96")
	nat64Local = netip.MustParsePrefix("64:ff9b:1::/48")
	sixToFour  = netip.MustParsePrefix("2002::/16")
)

// embeddedIPv4 is the IPv4 address a NAT64 or 6to4 address carries.
func embeddedIPv4(a netip.Addr) (netip.Addr, bool) {
	b := a.As16()
	switch {
	case nat64.Contains(a):
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case nat64Local.Contains(a):
		// a /48 prefix: the IPv4 address is octets 6 and 7, then 9 and 10, around the reserved u octet
		return netip.AddrFrom4([4]byte{b[6], b[7], b[9], b[10]}), true
	case sixToFour.Contains(a):
		return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), true
	}
	return netip.Addr{}, false
}

// keptAcrossOrigins are the headers a redirect to another origin keeps. Every other header the
// caller set (Authorization, a Cookie, an X-API-Key, any custom credential) is dropped: the stdlib
// drops only Authorization, Cookie and WWW-Authenticate, and only for another domain, so a custom
// key followed a redirect anywhere.
var keptAcrossOrigins = map[string]bool{"Accept": true, "Accept-Language": true, "Content-Type": true, "User-Agent": true}

// checkRedirect follows at most 10 redirects, as the stdlib does, and strips the caller's headers
// but keptAcrossOrigins from a hop to another origin (scheme, host or port) than the request's own.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("request: stopped after 10 redirects")
	}
	if from := via[0].URL; !strings.EqualFold(req.URL.Scheme, from.Scheme) || !strings.EqualFold(req.URL.Host, from.Host) {
		for k := range req.Header {
			if !keptAcrossOrigins[http.CanonicalHeaderKey(k)] {
				req.Header.Del(k)
			}
		}
	}
	return nil
}

// CustomPayload allows payload objects to declare their own Content-Type header
// alongside the payload data stream (for example, [FormWriter]).
type CustomPayload interface {
	io.Reader
	// ContentType returns the MIME media type string (e.g. "multipart/form-data; boundary=...").
	ContentType() string
}

// Request executes an HTTP request. It populates params.Request, params.Response,
// and params.ResponseBody. Only transport-level errors are returned; HTTP status
// codes are not treated as errors — use DecodeResponse to handle those.
//
// Request has no cancellation support; use Do to pass a context.Context.
func Request(params *Params, payload any, opts ...RequestOption) error {
	return Do(context.Background(), params, payload, opts...)
}

// Do executes an HTTP request bound to ctx. Cancelling ctx aborts the request
// and the response-body read. It is otherwise identical to Request.
func Do(ctx context.Context, params *Params, payload any, opts ...RequestOption) error {
	for _, opt := range opts {
		if opt != nil {
			params = opt(params)
		}
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

	timeout := params.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if timeout < 0 {
		timeout = 0 // http.Client treats zero as no timeout
	}

	transport := defaultTransport
	if params.DenyPrivate {
		transport = guardedTransport
	}
	client := &http.Client{
		Timeout:       timeout,
		Transport:     transport,
		CheckRedirect: checkRedirect,
	}
	req, err := http.NewRequestWithContext(ctx, params.Method, params.Url, _payload)
	if err != nil {
		return err
	}
	for header, value := range params.Headers {
		req.Header.Set(header, value)
	}
	for _, c := range params.Cookies {
		req.AddCookie(c)
	}
	// A Content-Type set explicitly via params.Headers wins over the one
	// derived from the payload type.
	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}
	if contentLength > 0 {
		req.ContentLength = int64(contentLength)
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
		return fmt.Errorf("%w (limit %d bytes)", ErrResponseTooLarge, limit)
	}
	params.ResponseBody = string(data)
	return nil
}

// StatusCodeInBounds returns true if the HTTP status code is in the 2xx-3xx range.
func StatusCodeInBounds(code int) bool {
	return code >= 200 && code < 400
}

// Get performs a GET request and decodes the response using the default Error
// type. The request uses DefaultTimeout; build Params directly when a
// different timeout is needed.
func Get(uri string, params map[string][]string, response any) (*http.Response, error) {
	uri = strings.TrimSuffix(uri, "?")
	if params != nil {
		uri += "?" + url.Values(params).Encode()
	}
	p := &Params{
		Method: "GET",
		Url:    uri,
	}
	if err := Request(p, nil); err != nil {
		return nil, err
	}
	return p.Response, DecodeResponse[Error](p, response)
}

// Post performs a POST request and decodes the response using the default
// Error type. The request uses DefaultTimeout; build Params directly when a
// different timeout is needed.
func Post(uri string, payload, response any) (*http.Response, error) {
	p := &Params{
		Method: "POST",
		Url:    uri,
	}
	if err := Request(p, payload); err != nil {
		return nil, err
	}
	return p.Response, DecodeResponse[Error](p, response)
}
