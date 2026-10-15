// Package redirect implements the hot read path: GET/HEAD /{code}.
package redirect

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/events"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/httpx"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/visitor"
)

// Resolver resolves codes (links.Resolver).
type Resolver interface {
	Resolve(ctx context.Context, code string) (links.Resolution, error)
}

// Enqueuer accepts click events without blocking (events.Producer).
type Enqueuer interface {
	Enqueue(e events.ClickEvent) bool
}

// Deps wires a Handler.
type Deps struct {
	Resolver       Resolver
	Clicks         Enqueuer
	Hasher         *visitor.Hasher
	FromCloudFront bool
	Metrics        telemetry.Recorder
	Logger         *slog.Logger
	Now            func() time.Time
	NewEventID     func() string
}

// Handler serves redirects.
type Handler struct{ d Deps }

// New returns a Handler.
func New(d Deps) *Handler {
	if d.NewEventID == nil {
		d.NewEventID = func() string {
			if u, err := uuid.NewV7(); err == nil {
				return u.String()
			}
			return uuid.NewString()
		}
	}
	return &Handler{d: d}
}

// Routes returns the service's HTTP handler with middleware applied.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httpx.Healthz)
	mux.HandleFunc("GET /{code}", h.redirect) // GET patterns also match HEAD
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteProblem(w, r, http.StatusNotFound, httpx.CodeNotFound, "")
	})
	return httpx.Chain(mux,
		httpx.RequestID,
		httpx.AccessLog(h.d.Logger, h.d.Now),
		httpx.Recover(h.d.Logger),
	)
}

func (h *Handler) redirect(w http.ResponseWriter, r *http.Request) {
	start := h.d.Now()
	code := r.PathValue("code")
	httpx.AddLogAttrs(r.Context(), "code", code)
	defer func() {
		h.d.Metrics.Observe(telemetry.RedirectLatencyMs, float64(h.d.Now().Sub(start).Microseconds())/1000)
	}()

	if !links.ValidCode(code) {
		httpx.WriteProblem(w, r, http.StatusNotFound, httpx.CodeNotFound, "")
		return
	}
	res, err := h.d.Resolver.Resolve(r.Context(), code)
	if err != nil {
		h.d.Logger.ErrorContext(r.Context(), "resolve failed", "request_id", httpx.RequestIDFrom(r.Context()), "code", code, "err", err)
		httpx.WriteProblem(w, r, http.StatusServiceUnavailable, httpx.CodeServiceUnavailable, "")
		return
	}
	switch res.Outcome {
	case links.OutcomeNotFound:
		httpx.WriteProblem(w, r, http.StatusNotFound, httpx.CodeNotFound, "")
		return
	case links.OutcomeGone:
		httpx.WriteProblem(w, r, http.StatusGone, httpx.CodeGone, "")
		return
	}

	hdr := w.Header()
	hdr.Set("Location", res.URL)
	hdr.Set("Cache-Control", "private, no-store, max-age=0")
	hdr.Set("X-Robots-Tag", "noindex")
	hdr.Set("Referrer-Policy", "no-referrer-when-downgrade")
	w.WriteHeader(http.StatusFound)

	// The response is written first; enqueueing never blocks it (SPEC §4.1).
	if r.Method == http.MethodGet {
		h.d.Clicks.Enqueue(h.event(r, code, start))
	}
}

func (h *Handler) event(r *http.Request, code string, now time.Time) events.ClickEvent {
	ip, _ := httpx.ClientIP(r, h.d.FromCloudFront)
	ua := r.UserAgent()
	return events.ClickEvent{
		V:            events.Version,
		EventID:      h.d.NewEventID(),
		Code:         code,
		TS:           now.UTC(),
		Visitor:      h.d.Hasher.VisitorID(ip, ua, now),
		ReferrerHost: visitor.ReferrerHost(r.Referer()),
		Country:      visitor.Country(r.Header.Get("CloudFront-Viewer-Country")),
		UserAgent:    visitor.TruncateUA(ua),
		IsBot:        visitor.IsBot(ua),
	}
}
