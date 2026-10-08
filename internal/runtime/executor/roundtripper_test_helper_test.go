package executor

import "net/http"

// roundTripperFunc adapts a function to http.RoundTripper for tests that need to
// intercept upstream transport without standing up a real server.
type roundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper.
func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
