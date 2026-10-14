package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// MaxBodyBytes is the request body limit (SPEC §9: 8 KiB).
const MaxBodyBytes = 8 << 10

// Decode errors, mapped to problems by WriteDecodeError.
var (
	ErrUnsupportedMediaType = errors.New("content type must be application/json")
	ErrPayloadTooLarge      = errors.New("request body too large")
)

// InvalidJSONError describes a malformed body in client-safe terms.
type InvalidJSONError struct{ Detail string }

func (e *InvalidJSONError) Error() string { return e.Detail }

// LimitBody caps request bodies at n bytes.
func LimitBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > n {
				WriteProblem(w, r, http.StatusRequestEntityTooLarge, CodePayloadTooLarge, fmt.Sprintf("request body must not exceed %d bytes", n))
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// DecodeJSON decodes a single JSON object into dst, rejecting unknown fields,
// trailing data, the wrong content type, and oversize bodies.
func DecodeJSON(r *http.Request, dst any) error {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return ErrUnsupportedMediaType
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return classifyDecodeErr(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return ErrPayloadTooLarge
		}
		return &InvalidJSONError{Detail: "request body must contain a single JSON object"}
	}
	return nil
}

func classifyDecodeErr(err error) error {
	var (
		tooLarge  *http.MaxBytesError
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
	)
	switch {
	case errors.As(err, &tooLarge):
		return ErrPayloadTooLarge
	case errors.Is(err, io.EOF):
		return &InvalidJSONError{Detail: "request body is empty"}
	case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
		return &InvalidJSONError{Detail: "request body is not valid JSON"}
	case errors.As(err, &typeErr):
		return &InvalidJSONError{Detail: fmt.Sprintf("field %q has the wrong type", typeErr.Field)}
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return &InvalidJSONError{Detail: "unknown field " + strings.TrimPrefix(err.Error(), "json: unknown field ")}
	default:
		return &InvalidJSONError{Detail: "request body is not valid JSON"}
	}
}

// WriteDecodeError maps a DecodeJSON error to the matching problem response.
func WriteDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	var inv *InvalidJSONError
	switch {
	case errors.Is(err, ErrUnsupportedMediaType):
		WriteProblem(w, r, http.StatusUnsupportedMediaType, CodeUnsupportedMedia, err.Error())
	case errors.Is(err, ErrPayloadTooLarge):
		WriteProblem(w, r, http.StatusRequestEntityTooLarge, CodePayloadTooLarge, fmt.Sprintf("request body must not exceed %d bytes", MaxBodyBytes))
	case errors.As(err, &inv):
		WriteProblem(w, r, http.StatusBadRequest, CodeInvalidRequest, inv.Detail)
	default:
		WriteProblem(w, r, http.StatusBadRequest, CodeInvalidRequest, "invalid request body")
	}
}

// ClientIP returns the viewer address. With fromCloudFront it reads
// CloudFront-Viewer-Address ("ip:port"; IPv6 is not bracketed). This is only
// trustworthy because the ALB is reachable solely through CloudFront (ADR-0012).
// Otherwise (local development) it uses the TCP peer address.
func ClientIP(r *http.Request, fromCloudFront bool) (netip.Addr, bool) {
	if fromCloudFront {
		if v := r.Header.Get("CloudFront-Viewer-Address"); v != "" {
			if i := strings.LastIndexByte(v, ':'); i > 0 {
				if a, err := netip.ParseAddr(strings.Trim(v[:i], "[]")); err == nil {
					return a.Unmap(), true
				}
			}
		}
		return netip.Addr{}, false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}
