package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/cache"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/httpx"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
)

// apiTime is the wire format for timestamps (RFC 3339, UTC, milliseconds).
const apiTime = "2006-01-02T15:04:05.000Z"

func fmtTime(t time.Time) string { return t.UTC().Format(apiTime) }

func fmtOptTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := fmtTime(*t)
	return &s
}

// LinkResponse is the full link representation.
type LinkResponse struct {
	Code           string  `json:"code"`
	ShortURL       string  `json:"short_url"`
	URL            string  `json:"url"`
	Status         string  `json:"status"`
	Custom         bool    `json:"custom"`
	ScanStatus     string  `json:"scan_status"`
	DisabledReason string  `json:"disabled_reason,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	ExpiresAt      *string `json:"expires_at,omitempty"`
}

// LinkSummaryResponse is the list-view representation.
type LinkSummaryResponse struct {
	Code      string  `json:"code"`
	ShortURL  string  `json:"short_url"`
	URL       string  `json:"url"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt *string `json:"expires_at,omitempty"`
}

// ListResponse is a page of links.
type ListResponse struct {
	Items      []LinkSummaryResponse `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

func (s *Server) shortURL(code string) string {
	return s.d.BaseURL.String() + "/" + code
}

func (s *Server) toResponse(l links.Link) LinkResponse {
	return LinkResponse{
		Code: l.Code, ShortURL: s.shortURL(l.Code), URL: l.LongURL, Status: string(l.Status), Custom: l.Custom,
		ScanStatus: string(l.ScanStatus), DisabledReason: l.DisabledReason,
		CreatedAt: fmtTime(l.CreatedAt), UpdatedAt: fmtTime(l.UpdatedAt), ExpiresAt: fmtOptTime(l.ExpiresAt),
	}
}

type createRequest struct {
	URL       string  `json:"url"`
	Alias     string  `json:"alias"`
	ExpiresAt *string `json:"expires_at"`
}

func parseTime(field, v string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, &links.ValidationError{Kind: links.ErrInvalidInput, Detail: field + " must be an RFC 3339 timestamp"}
	}
	return t.UTC(), nil
}

// validIdemKey: 1–255 printable ASCII characters.
func validIdemKey(k string) bool {
	if k == "" || len(k) > 255 {
		return false
	}
	for i := range len(k) {
		if k[i] < 0x21 || k[i] > 0x7e {
			return false
		}
	}
	return true
}

func (s *Server) createLink(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpx.WriteDecodeError(w, r, httpx.ErrPayloadTooLarge)
		} else {
			badRequest(w, r, "request body could not be read")
		}
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	idemKey := r.Header.Get("Idempotency-Key")
	_, hasIdem := r.Header["Idempotency-Key"]
	if hasIdem && !validIdemKey(idemKey) {
		badRequest(w, r, "Idempotency-Key must be 1-255 printable ASCII characters")
		return
	}
	sum := sha256.Sum256(body)
	fingerprint := hex.EncodeToString(sum[:])
	if hasIdem {
		if rec, ok := s.lookupIdempotent(r, p.OwnerID, idemKey); ok {
			if rec.Fingerprint != fingerprint {
				badRequest(w, r, "Idempotency-Key was already used with a different request body")
				return
			}
			w.Header().Set("Idempotent-Replayed", "true")
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(rec.Status)
			// Stored compact; re-add the newline the live response ends with
			// so a replay is byte-identical to the original.
			_, _ = w.Write(append(rec.Body, '\n'))
			return
		}
	}

	var req createRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteDecodeError(w, r, err)
		return
	}
	in := links.CreateInput{URL: req.URL, Alias: req.Alias}
	if req.ExpiresAt != nil {
		t, err := parseTime("expires_at", *req.ExpiresAt)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		in.ExpiresAt = &t
	}
	l, err := s.d.Links.Create(r.Context(), p.OwnerID, in)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	resp := s.toResponse(l)
	httpx.AddLogAttrs(r.Context(), "code", l.Code)
	if hasIdem {
		b, _ := json.Marshal(resp)
		s.storeIdempotent(r, p.OwnerID, idemKey, cache.IdemRecord{Fingerprint: fingerprint, Status: http.StatusCreated, Body: b})
	}
	w.Header().Set("Location", "/api/v1/links/"+l.Code)
	httpx.WriteJSON(w, http.StatusCreated, resp)
}

// Idempotency failures fail open (the request is processed normally).
func (s *Server) lookupIdempotent(r *http.Request, owner, key string) (cache.IdemRecord, bool) {
	rec, ok, err := s.d.Idempotency.Get(r.Context(), owner, key)
	if err != nil {
		s.d.Metrics.Count(telemetry.CacheErrors, 1)
		s.d.Logger.WarnContext(r.Context(), "idempotency lookup failed; processing request", "err", err)
		return cache.IdemRecord{}, false
	}
	return rec, ok
}

func (s *Server) storeIdempotent(r *http.Request, owner, key string, rec cache.IdemRecord) {
	if err := s.d.Idempotency.Put(r.Context(), owner, key, rec); err != nil {
		s.d.Metrics.Count(telemetry.CacheErrors, 1)
		s.d.Logger.WarnContext(r.Context(), "idempotency store failed", "err", err)
	}
}

func (s *Server) listLinks(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	q := r.URL.Query()
	limit := 20
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			badRequest(w, r, "limit must be an integer between 1 and 100")
			return
		}
		limit = n
	}
	page, err := s.d.Links.List(r.Context(), p.OwnerID, limit, q.Get("cursor"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	resp := ListResponse{Items: make([]LinkSummaryResponse, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, it := range page.Items {
		resp.Items = append(resp.Items, LinkSummaryResponse{
			Code: it.Code, ShortURL: s.shortURL(it.Code), URL: it.LongURL, Status: string(it.Status),
			CreatedAt: fmtTime(it.CreatedAt), ExpiresAt: fmtOptTime(it.ExpiresAt),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (s *Server) getLink(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	httpx.AddLogAttrs(r.Context(), "code", code)
	l, err := s.d.Links.Get(r.Context(), principalFrom(r.Context()).OwnerID, code)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.toResponse(l))
}

type updateRequest struct {
	URL       *string         `json:"url"`
	ExpiresAt json.RawMessage `json:"expires_at"`
}

func (s *Server) updateLink(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	httpx.AddLogAttrs(r.Context(), "code", code)
	var req updateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteDecodeError(w, r, err)
		return
	}
	in := links.UpdateInput{URL: req.URL}
	if req.ExpiresAt != nil {
		in.ExpiresAt.Set = true
		if raw := strings.TrimSpace(string(req.ExpiresAt)); raw != "null" {
			var v string
			if err := json.Unmarshal(req.ExpiresAt, &v); err != nil {
				badRequest(w, r, "expires_at must be an RFC 3339 timestamp or null")
				return
			}
			t, err := parseTime("expires_at", v)
			if err != nil {
				s.writeError(w, r, err)
				return
			}
			in.ExpiresAt.Value = &t
		}
	}
	if in.URL == nil && !in.ExpiresAt.Set {
		badRequest(w, r, "at least one of url or expires_at is required")
		return
	}
	l, err := s.d.Links.Update(r.Context(), principalFrom(r.Context()).OwnerID, code, in)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.toResponse(l))
}

func (s *Server) deleteLink(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	httpx.AddLogAttrs(r.Context(), "code", code)
	if err := s.d.Links.Delete(r.Context(), principalFrom(r.Context()).OwnerID, code); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

type disableRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) disableLink(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	httpx.AddLogAttrs(r.Context(), "code", code)
	var req disableRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteDecodeError(w, r, err)
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" || len(req.Reason) > 256 {
		badRequest(w, r, "reason is required (at most 256 characters)")
		return
	}
	l, err := s.d.Links.Disable(r.Context(), code, req.Reason)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.d.Logger.InfoContext(r.Context(), "link disabled by admin", "code", code, "key_id", principalFrom(r.Context()).KeyID)
	httpx.WriteJSON(w, http.StatusOK, s.toResponse(l))
}

func (s *Server) enableLink(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	httpx.AddLogAttrs(r.Context(), "code", code)
	l, err := s.d.Links.Enable(r.Context(), code)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.d.Logger.InfoContext(r.Context(), "link enabled by admin", "code", code, "key_id", principalFrom(r.Context()).KeyID)
	httpx.WriteJSON(w, http.StatusOK, s.toResponse(l))
}
