package request

// RequestOption configures request parameters prior to dispatch in [Request] or [Do].
// Options modify the provided [Params] instance directly.
type RequestOption func(param *Params) *Params

// RequestContentType defines standardized Content-Type header values.
type RequestContentType string

const (
	// JSON specifies "application/json".
	JSON RequestContentType = "application/json"
	// MULTIPART specifies "multipart/form-data".
	MULTIPART RequestContentType = "multipart/form-data"
)

// DenyPrivateAddresses sets Params.DenyPrivate: the request refuses to connect to a loopback,
// private, link-local, multicast or unspecified address, redirects included. For a URL that came
// from someone else (a webhook target, a link to fetch).
func DenyPrivateAddresses() RequestOption {
	return func(param *Params) *Params {
		param.DenyPrivate = true
		return param
	}
}

// ContentType configures the HTTP "Content-Type" request header. If the
// Params.Headers map is not yet initialized, ContentType allocates it.
func ContentType(contentType RequestContentType) RequestOption {
	return func(param *Params) *Params {
		if param.Headers == nil {
			param.Headers = make(map[string]string)
		}
		param.Headers["Content-Type"] = string(contentType)
		return param
	}
}
