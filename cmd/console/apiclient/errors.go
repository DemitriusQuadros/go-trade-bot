package apiclient

import "fmt"

// ErrUnreachable wraps any network-level failure (connection refused, DNS,
// timeout) distinctly from a well-formed non-2xx HTTP response, so callers
// (pages) can distinguish "cmd/api is down" (show disconnected state, keep
// last-known-good data on screen) from "cmd/api returned 404/500" (show an
// inline error for that one panel, other panels keep working).
type ErrUnreachable struct {
	Cause error
}

func (e ErrUnreachable) Error() string {
	return fmt.Sprintf("api unreachable: %v", e.Cause)
}

func (e ErrUnreachable) Unwrap() error {
	return e.Cause
}

// ErrAPI wraps a non-2xx response with status code and body.
type ErrAPI struct {
	StatusCode int
	Body       string
}

func (e ErrAPI) Error() string {
	return fmt.Sprintf("api error %d: %s", e.StatusCode, e.Body)
}
