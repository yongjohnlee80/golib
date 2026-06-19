package server

// RouteContext holds the data extracted while matching a route — currently the
// captured path parameters.
type RouteContext struct {
	params map[string]string
}

// Param returns the captured value for name, or "" if absent.
func (rc *RouteContext) Param(name string) string {
	if rc == nil {
		return ""
	}
	return rc.params[name]
}

// Params returns a defensive copy of all captured parameters; mutating the result
// does not affect routing state.
func (rc *RouteContext) Params() map[string]string {
	if rc == nil || len(rc.params) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(rc.params))
	for k, v := range rc.params {
		out[k] = v
	}
	return out
}
