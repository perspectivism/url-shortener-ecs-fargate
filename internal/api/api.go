// Package api implements the /api/v1 HTTP handlers (SPEC §9).
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/auth"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/cache"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/httpx"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/ratelimit"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
)

// LinkService is the link use-case layer (links.Service).
type LinkService interface {
	Create(ctx context.Context, owner string, in links.CreateInput) (links.Link, error)
	Get(ctx context.Context, owner, code string) (links.Link, error)
	List(ctx context.Context, owner string, limit int, cursor string) (links.Page, error)
	Update(ctx context.Context, owner, code string, in links.UpdateInput) (links.Link, error)
	Delete(ctx context.Context, owner, code string) error
	Disable(ctx context.Context, code, reason string) (links.Link, error)
	Enable(ctx context.Context, code string) (links.Link, error)
}

// Authenticator validates Authorization headers.
type Authenticator interface {
	Authenticate(ctx context.Context, header string) (auth.Principal, error)
}

// Limiter applies rate limits.
type Limiter interface {
	Allow(ctx context.Context, owner, route string, limit int, window time.Duration) ratelimit.Decision
}

// IdempotencyStore persists create responses.
type IdempotencyStore interface {
	Get(ctx context.Context, owner, key string) (cache.IdemRecord, bool, error)
	Put(ctx context.Context, owner, key string, rec cache.IdemRecord) error
}

// Deps wires a Server.
type Deps struct {
	Links       LinkService
	Stats       StatsService
	Auth        Authenticator
	Limiter     Limiter
	Idempotency IdempotencyStore
	Metrics     telemetry.Recorder
	Logger      *slog.Logger
	Now         func() time.Time
	BaseURL     *url.URL
	WriteLimit  int
	ReadLimit   int
}

// Server holds the API handlers.
type Server struct{ d Deps }

// New returns a Server.
func New(d Deps) *Server { return &Server{d: d} }

// Rate-limit buckets.
const (
	bucketWrite = "write"
	bucketRead  = "read"
)

// Handler returns the API's HTTP handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httpx.Healthz)
	s.route(mux, "POST /api/v1/links", auth.ScopeLinksWrite, bucketWrite, s.createLink)
	s.route(mux, "GET /api/v1/links", auth.ScopeLinksRead, bucketRead, s.listLinks)
	s.route(mux, "GET /api/v1/links/{code}", auth.ScopeLinksRead, bucketRead, s.getLink)
	s.route(mux, "PATCH /api/v1/links/{code}", auth.ScopeLinksWrite, bucketWrite, s.updateLink)
	s.route(mux, "DELETE /api/v1/links/{code}", auth.ScopeLinksWrite, bucketWrite, s.deleteLink)
	s.route(mux, "GET /api/v1/links/{code}/stats", auth.ScopeLinksRead, bucketRead, s.linkStats)
	s.route(mux, "POST /api/v1/admin/links/{code}/disable", auth.ScopeAdmin, bucketWrite, s.disableLink)
	s.route(mux, "POST /api/v1/admin/links/{code}/enable", auth.ScopeAdmin, bucketWrite, s.enableLink)

	return httpx.Chain(problemFallback(mux),
		httpx.RequestID,
		httpx.AccessLog(s.d.Logger, s.d.Now),
		httpx.Recover(s.d.Logger),
		httpx.LimitBody(httpx.MaxBodyBytes),
	)
}

type principalKey struct{}

func principalFrom(ctx context.Context) auth.Principal {
	p, _ := ctx.Value(principalKey{}).(auth.Principal)
	return p
}

// route registers h behind authentication (401), scope (403) and rate limiting (429).
func (s *Server) route(mux *http.ServeMux, pattern, scope, bucket string, h http.HandlerFunc) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		p, err := s.d.Auth.Authenticate(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			if !errors.Is(err, auth.ErrUnauthorized) {
				s.d.Logger.ErrorContext(r.Context(), "authentication backend error", "request_id", httpx.RequestIDFrom(r.Context()), "err", err)
				httpx.WriteProblem(w, r, http.StatusServiceUnavailable, httpx.CodeServiceUnavailable, "")
				return
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
			httpx.WriteProblem(w, r, http.StatusUnauthorized, httpx.CodeUnauthorized, "a valid API key is required")
			return
		}
		httpx.AddLogAttrs(r.Context(), "key_id", p.KeyID)
		if !p.Has(scope) {
			httpx.WriteProblem(w, r, http.StatusForbidden, httpx.CodeForbidden, "API key lacks scope "+scope)
			return
		}
		limit := s.d.ReadLimit
		if bucket == bucketWrite {
			limit = s.d.WriteLimit
		}
		if d := s.d.Limiter.Allow(r.Context(), p.OwnerID, bucket, limit, time.Minute); !d.Allowed {
			secs := int((d.RetryAfter + time.Second - 1) / time.Second)
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			httpx.WriteProblem(w, r, http.StatusTooManyRequests, httpx.CodeRateLimited, "rate limit exceeded")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

// problemFallback turns the mux's plain-text 404/405 into problem+json.
func problemFallback(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		probe := &headerProbe{header: http.Header{}}
		mux.ServeHTTP(probe, r)
		if probe.status == http.StatusMethodNotAllowed {
			w.Header().Set("Allow", probe.header.Get("Allow"))
			httpx.WriteProblem(w, r, http.StatusMethodNotAllowed, httpx.CodeMethodNotAllowed, "")
			return
		}
		httpx.WriteProblem(w, r, http.StatusNotFound, httpx.CodeNotFound, "")
	})
}

// headerProbe captures what the mux's built-in 404/405 handlers would write.
type headerProbe struct {
	header http.Header
	status int
}

func (p *headerProbe) Header() http.Header         { return p.header }
func (p *headerProbe) Write(b []byte) (int, error) { return len(b), nil }
func (p *headerProbe) WriteHeader(code int)        { p.status = code }

// writeError maps domain errors to problem responses — the single place where
// that mapping lives. Internal error text never reaches clients.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *links.ValidationError
	detail := ""
	if errors.As(err, &ve) {
		detail = ve.Detail
	}
	switch {
	case errors.Is(err, links.ErrNotFound):
		httpx.WriteProblem(w, r, http.StatusNotFound, httpx.CodeNotFound, "")
	case errors.Is(err, links.ErrGone):
		httpx.WriteProblem(w, r, http.StatusGone, httpx.CodeGone, detail)
	case errors.Is(err, links.ErrAliasTaken):
		httpx.WriteProblem(w, r, http.StatusConflict, httpx.CodeAliasTaken, detail)
	case errors.Is(err, links.ErrUnsafeURL):
		httpx.WriteProblem(w, r, http.StatusUnprocessableEntity, httpx.CodeUnsafeURL, detail)
	case errors.Is(err, links.ErrInvalidURL):
		httpx.WriteProblem(w, r, http.StatusUnprocessableEntity, httpx.CodeInvalidURL, detail)
	case errors.Is(err, links.ErrInvalidInput):
		httpx.WriteProblem(w, r, http.StatusBadRequest, httpx.CodeInvalidRequest, detail)
	case errors.Is(err, links.ErrInvalidCursor):
		httpx.WriteProblem(w, r, http.StatusBadRequest, httpx.CodeInvalidRequest, "cursor is invalid")
	case errors.Is(err, context.DeadlineExceeded):
		s.d.Logger.ErrorContext(r.Context(), "dependency timeout", "request_id", httpx.RequestIDFrom(r.Context()), "err", err)
		httpx.WriteProblem(w, r, http.StatusServiceUnavailable, httpx.CodeServiceUnavailable, "")
	default:
		s.d.Logger.ErrorContext(r.Context(), "internal error", "request_id", httpx.RequestIDFrom(r.Context()), "err", err)
		httpx.WriteProblem(w, r, http.StatusInternalServerError, httpx.CodeInternal, "")
	}
}

func badRequest(w http.ResponseWriter, r *http.Request, detail string) {
	httpx.WriteProblem(w, r, http.StatusBadRequest, httpx.CodeInvalidRequest, detail)
}
