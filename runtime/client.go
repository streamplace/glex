package glex

import (
	"context"
	"net/http"
)

// HTTP method constants used by generated XRPC endpoint functions.
// Query endpoints use GET; Procedure endpoints use POST.
const (
	Query     = http.MethodGet
	Procedure = http.MethodPost
)

// LexClient is the interface that generated XRPC endpoint functions call
// against. The concrete implementation is provided by the consumer (e.g., a
// wrapper around xrpc.Client or a custom HTTP client). Generated code only
// depends on this interface, keeping the runtime free of HTTP implementation
// details.
//
// Parameters:
//   - ctx: request context.
//   - method: one of Query or Procedure.
//   - inputEncoding: content type for the request body (e.g.,
//     "application/json"); empty string for query (GET) requests.
//   - endpoint: the XRPC endpoint NSID path (e.g.,
//     "app.bsky.feed.getTimeline"); the base URL is managed by the client.
//   - params: query parameters for GET requests; nil/empty for POST.
//   - bodyData: request body struct for POST requests; nil for GET.
//   - out: pointer to the response struct to decode into; nil to discard the
//     response body.
type LexClient interface {
	LexDo(ctx context.Context, method string, inputEncoding string, endpoint string,
		params map[string]any, bodyData any, out any) error
}
