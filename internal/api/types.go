// Package api defines the wire types shared between the CLI client and the
// server. It imports stdlib only.
package api

import "time"

// ProtocolVersion is the shared protocol revision. The server reports it in
// HealthResponse and the client requires an exact match — it is the single
// source of truth; never hardcode the revision elsewhere.
const ProtocolVersion = 1

// BriefRequest is the POST /brief request body. Empty in Phase 1; the final
// shape (nested news/stocks overrides + top-level model) is defined in
// docs/PRODUCT.md "Client/Server API". The Phase 1 server ignores all fields.
type BriefRequest struct{}

// BriefResponse is the POST /brief response body.
type BriefResponse struct {
	GeneratedAt time.Time `json:"generated_at"`
	Message     string    `json:"message"`
}

// HealthResponse is the GET /healthz response body. Token is the server's
// instance token ("") for an unmanaged manual server; the CLI requires it to
// echo back the state file's token as part of identity verification.
type HealthResponse struct {
	Token           string `json:"token"`
	ProtocolVersion int    `json:"protocol_version"`
}

// ErrorResponse is the shared error shape used by every non-2xx response.
type ErrorResponse struct {
	Error string `json:"error"`
}
