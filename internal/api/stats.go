package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/shortener/url-shortener-ecs-fargate/internal/analytics"
	"github.com/shortener/url-shortener-ecs-fargate/internal/httpx"
)

// StatsService builds link statistics (analytics.Service).
type StatsService interface {
	Report(ctx context.Context, code string, from, to time.Time) (analytics.Report, error)
}

// linkStats serves GET /api/v1/links/{code}/stats?from=YYYY-MM-DD&to=YYYY-MM-DD
// (default: the last 7 days including today, UTC; at most 90 days).
func (s *Server) linkStats(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	httpx.AddLogAttrs(r.Context(), "code", code)
	// Ownership first: other owners' links are 404 (never reveal existence).
	if _, err := s.d.Links.Get(r.Context(), principalFrom(r.Context()).OwnerID, code); err != nil {
		s.writeError(w, r, err)
		return
	}
	today := s.d.Now().UTC().Truncate(24 * time.Hour)
	to, from := today, today.AddDate(0, 0, -6)
	q := r.URL.Query()
	var err error
	if v := q.Get("to"); v != "" {
		if to, err = time.Parse(time.DateOnly, v); err != nil {
			badRequest(w, r, "to must be a date (YYYY-MM-DD)")
			return
		}
		if q.Get("from") == "" {
			from = to.AddDate(0, 0, -6)
		}
	}
	if v := q.Get("from"); v != "" {
		if from, err = time.Parse(time.DateOnly, v); err != nil {
			badRequest(w, r, "from must be a date (YYYY-MM-DD)")
			return
		}
	}
	rep, err := s.d.Stats.Report(r.Context(), code, from, to)
	if errors.Is(err, analytics.ErrInvalidRange) {
		badRequest(w, r, "from must not be after to and the range is at most 90 days")
		return
	}
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rep)
}
