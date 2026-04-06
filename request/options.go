package request

// RequestOption is a functional option used to modify request parameters before
// the request is executed.
type RequestOption func(param *Params) *Params

// RequestContentType represents the content type of an HTTP request as a string value.
type RequestContentType string

const (
	JSON      RequestContentType = "application/json"
	MULTIPART RequestContentType = "multipart/form-data"
)

// ContentType sets the "Content-Type" header of an HTTP request to the
// provided content type.
func ContentType(contentType RequestContentType) RequestOption {
	return func(param *Params) *Params {
		if param.Headers == nil {
			param.Headers = make(map[string]string)
		}
		param.Headers["Content-Type"] = string(contentType)
		return param
	}
}
