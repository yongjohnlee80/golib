package ingestor

import (
	"errors"
	"strings"
)

var (
	// ErrMissingIngestor indicates that no ingestor has been configured.
	ErrMissingIngestor = errors.New("missing ingestor, cannot process data")
)

// BatchErrors collects multiple errors from batch write operations.
// It implements the error interface so it can be returned directly.
type BatchErrors struct {
	Errors []error
}

func (be *BatchErrors) Error() string {
	msgs := make([]string, len(be.Errors))
	for i, err := range be.Errors {
		msgs[i] = err.Error()
	}
	return strings.Join(msgs, "; ")
}

// Unwrap returns the underlying errors for use with errors.Is/As.
func (be *BatchErrors) Unwrap() []error {
	return be.Errors
}
