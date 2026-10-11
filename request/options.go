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
