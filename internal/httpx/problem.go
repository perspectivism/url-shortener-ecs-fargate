// Package httpx holds HTTP plumbing shared by the services: RFC 9457 problem
// responses, JSON helpers, middleware (request IDs, recovery, access logs, body
// limits), client IP extraction, and server lifecycle.
package httpx

import (
	"encoding/json"
	"net/http"
)

// ProblemContentType is the RFC 9457 media type.
const ProblemContentType = "application/problem+json"

// Problem codes (SPEC §9.3).
const (
	CodeInvalidRequest     = "invalid_request"
	CodeUnauthorized       = "unauthorized"
	CodeForbidden          = "forbidden"
	CodeNotFound           = "not_found"
	CodeMethodNotAllowed   = "method_not_allowed"
	CodeAliasTaken         = "alias_taken"
	CodeGone               = "gone"
	CodePayloadTooLarge    = "payload_too_large"
	CodeUnsupportedMedia   = "unsupported_media_type"
	CodeUnsafeURL          = "unsafe_url"
	CodeInvalidURL         = "invalid_url"
	CodeRateLimited        = "rate_limited"
	CodeInternal           = "internal"
	CodeServiceUnavailable = "unavailable"
)

// Problem is an RFC 9457 problem details object with a machine-readable code.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Code      string `json:"code"`
	RequestID string `json:"request_id,omitempty"`
}

// WriteProblem writes a problem+json response. detail must be safe to show to
// clients (never internal error text).
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	p := Problem{
		Type:      "urn:shortener:problem:" + code,
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    detail,
		Code:      code,
		RequestID: RequestIDFrom(r.Context()),
	}
	h := w.Header()
	h.Set("Content-Type", ProblemContentType)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// WriteJSON writes v as an application/json response.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
