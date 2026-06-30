package bayse

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("bayse: resource not found")

type transientError struct{ err error }

func (e *transientError) Error() string { return "bayse: transient: " + e.err.Error() }
func (e *transientError) Unwrap() error { return e.err }

func transient(err error) error { return &transientError{err: err} }

type apiError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("bayse: api error %d: %s (%s)", e.StatusCode, e.Message, e.Code)
}

type errorBody struct {
	Error      string `json:"error"`
	Message    string `json:"message"`
	StatusCode int    `json:"statusCode"`
}
