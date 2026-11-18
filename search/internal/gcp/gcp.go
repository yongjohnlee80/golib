// Package gcp is what search/embed's and search/rank's Vertex AI clients share: the headers of a
// Google API call, and the reading of a 403 that says an API is not enabled. It takes no Google
// dependency: the caller supplies the tokens.
package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
)

// Tokens answers an OAuth access token for Google's APIs. refresh is true after the API refused the
// last one (HTTP 401): the answer must then not be a cached token.
type Tokens func(ctx context.Context, refresh bool) (string, error)

// Header is the headers of one call: the token tokens answers, and the project the call is billed
// to (X-Goog-User-Project, which Google's APIs need with user credentials and ignore with a
// service account of the same project). A failure to get a token is returned as tokens gave it,
// wrapped with %w; the token itself is never in an error.
func Header(ctx context.Context, tokens Tokens, refresh bool, project string) (http.Header, error) {
	tok, err := tokens(ctx, refresh)
	if err != nil {
		return nil, fmt.Errorf("getting a token: %w", err)
	}
	if tok == "" {
		return nil, fmt.Errorf("getting a token: the token source answered none")
	}
	return http.Header{"Authorization": {"Bearer " + tok}, "X-Goog-User-Project": {project}}, nil
}

// name is what a service or consumer name in an error's details may be: Google's identifiers
// ("aiplatform.googleapis.com", "projects/123456"), never free text.
var name = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)

// Disabled reads a 403's body: the API that is not enabled and the project it isn't enabled in,
// when the body's details carry a google.rpc.ErrorInfo of reason SERVICE_DISABLED in the domain
// googleapis.com. ok is false for any other body, and for names that are not Google identifiers.
func Disabled(body []byte) (service, consumer string, ok bool) {
	var e struct {
		Error struct {
			Details []struct {
				Type     string            `json:"@type"`
				Reason   string            `json:"reason"`
				Domain   string            `json:"domain"`
				Metadata map[string]string `json:"metadata"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil {
		return "", "", false
	}
	for _, d := range e.Error.Details {
		if d.Type != "type.googleapis.com/google.rpc.ErrorInfo" || d.Reason != "SERVICE_DISABLED" || d.Domain != "googleapis.com" {
			continue
		}
		service, consumer = d.Metadata["service"], d.Metadata["consumer"]
		if !name.MatchString(service) {
			return "", "", false
		}
		if !name.MatchString(consumer) {
			consumer = ""
		}
		return service, consumer, true
	}
	return "", "", false
}
